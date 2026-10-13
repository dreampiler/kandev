package review

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/kandev/kandev/internal/common/logger"
	"github.com/kandev/kandev/internal/task/models"
	taskservice "github.com/kandev/kandev/internal/task/service"
)

// PromptContext carries the task-level facts the review prompt interpolates.
type PromptContext struct {
	TaskTitle       string
	TaskDescription string
	BranchName      string
	BaseBranch      string
}

// PromptResult is one reviewer reply plus its accounting.
type PromptResult struct {
	Response       string
	Model          string
	PromptTokens   int
	ResponseTokens int
	DurationMs     int
}

// Inference runs one reviewer prompt. Implementations wrap the session-bound
// agentctl inference path and the sessionless host-utility path; the runner does
// not care which, so both triggers share one execution substrate.
type Inference interface {
	Run(ctx context.Context, identity ReviewerIdentity, sessionID, prompt string) (*PromptResult, error)
}

// PromptBuilder renders the reviewer prompt for one batch of files. Backed by the
// `code-review` utility agent's stored (and user-editable) prompt template.
type PromptBuilder interface {
	Build(ctx context.Context, batch []ChangedFile, promptCtx PromptContext) (string, error)
}

// TaskContextLookup supplies the task-level prompt facts.
type TaskContextLookup interface {
	ReviewPromptContext(ctx context.Context, taskID, sessionID string) (PromptContext, error)
}

// SessionLookup resolves which session's workspace a run should read. A task can
// have several sessions; they share one workspace, so any live session will do.
type SessionLookup interface {
	ReviewSessionID(ctx context.Context, taskID string) (string, error)
}

// Store is the persistence surface the runner drives. Satisfied by
// *taskservice.ReviewService.
type Store interface {
	CreateRun(ctx context.Context, req taskservice.CreateRunRequest) (*models.TaskReviewRun, error)
	ActiveRun(ctx context.Context, taskID string) (*models.TaskReviewRun, error)
	MarkRunRunning(ctx context.Context, runID string) (*models.TaskReviewRun, error)
	CompleteRun(ctx context.Context, req taskservice.CompleteRunRequest) (*models.TaskReviewRun, error)
	FailRun(ctx context.Context, runID, code, message string, durationMs int) (*models.TaskReviewRun, error)
	CancelRun(ctx context.Context, runID string) (*models.TaskReviewRun, error)
	PublishFindings(ctx context.Context, req taskservice.PublishFindingsRequest) (*models.TaskReviewRun, []*models.TaskReviewFinding, error)
	// FindRunByEntryID returns the run already created for a step-transition
	// ledger entry, or nil when none exists yet. Backs the durable dedup for
	// AC-OFFICE-STEP-ENTRY-001.10: a redelivery of the same entry (after
	// completion, or after a restart) must rejoin the existing run instead of
	// launching a second one.
	FindRunByEntryID(ctx context.Context, entryID string) (*models.TaskReviewRun, error)
}

// RunRequest describes a review pass to start.
type RunRequest struct {
	WorktreePath   string `json:"worktree_path,omitempty"`
	BaseRef        string `json:"base_ref,omitempty"`
	HeadRef        string `json:"head_ref,omitempty"`
	TaskID         string
	SessionID      string
	RepositoryID   string
	AgentProfileID string
	Trigger        models.ReviewRunTrigger
	WorkflowStepID string
	// EntryID is the step-transition ledger row identifier of the step entry
	// that requested this run, when triggered by the run_code_review
	// step-entry action. Empty for manual/MCP-triggered runs.
	EntryID string
	// Origin is the concrete execution that wrote the changes, when known. The
	// workflow-step path sets it so the reviewer is never the origin's own model
	// or profile. Nil leaves the explicit precedence path unchanged when
	// OriginRequired is false.
	Origin *OriginIdentity
	// OriginRequired marks a caller (the workflow-step path) that must exclude a
	// known origin. With it set, a nil Origin fails closed with ErrAgentUnavailable
	// instead of reviewing with an unproven writer model.
	OriginRequired bool
	// retryOfRunID links an automatic timeout fallback to the run it replaces.
	// Set only by the runner itself; never a persisted column.
	retryOfRunID string
}

// Runner orchestrates review passes.
//
// Goroutine ownership: Start registers the runner's background context and
// Stop cancels it and waits for in-flight passes to drain, per the ownership
// rules in apps/backend/AGENTS.md. A pass detached by Launch is therefore always
// joined at shutdown rather than leaked.
type Runner struct {
	store       Store
	resolver    *Resolver
	changes     ChangeSource
	inference   Inference
	prompts     PromptBuilder
	taskContext TaskContextLookup
	sessions    SessionLookup
	logger      *logger.Logger

	// budgetBytes overrides PromptBudgetBytes; tests set it to force batching.
	budgetBytes int

	// writePolicy bounds terminal-write retries; tests shrink it.
	writePolicy terminalWritePolicy

	// passTimeout bounds a single pass; defaults to runTimeout and is only
	// overridden by tests.
	passTimeout time.Duration

	mu       sync.Mutex
	inFlight map[string]*inFlightRun
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	started  bool
}

// inFlightRun is the live state of one review pass. The cancel handle is what
// makes a user-requested cancel actually stop inference: without it, cancelling
// only marks the DB row and the goroutine keeps going and overwrites it.
type inFlightRun struct {
	runID  string
	cancel context.CancelFunc
	done   chan struct{}
}

// RunnerDeps groups the runner's collaborators.
type RunnerDeps struct {
	Store       Store
	Resolver    *Resolver
	Changes     ChangeSource
	Inference   Inference
	Prompts     PromptBuilder
	TaskContext TaskContextLookup
	Sessions    SessionLookup
	Logger      *logger.Logger
	BudgetBytes int
}

// NewRunner builds a Runner.
func NewRunner(deps RunnerDeps) *Runner {
	return &Runner{
		store:       deps.Store,
		resolver:    deps.Resolver,
		changes:     deps.Changes,
		inference:   deps.Inference,
		prompts:     deps.Prompts,
		taskContext: deps.TaskContext,
		sessions:    deps.Sessions,
		logger:      deps.Logger.WithFields(zap.String("component", "review-runner")),
		budgetBytes: deps.BudgetBytes,
		writePolicy: defaultTerminalWritePolicy,
		passTimeout: runTimeout,
		inFlight:    make(map[string]*inFlightRun),
	}
}

// writeTerminal records a run's terminal outcome. A single write can lose to a
// busy database writer, so it is retried a bounded number of times; every
// attempt uses a context detached from the pass so neither the pass deadline nor
// a caller disconnect can cancel the write that clears pending/running. Delays
// observe the runner's shutdown context so Stop is not delayed by a backoff.
func (r *Runner) writeTerminal(ctx context.Context, write func(context.Context) error) error {
	base := context.WithoutCancel(ctx)
	var lastErr error
	for attempt := 0; attempt < r.writePolicy.attempts; attempt++ {
		if attempt > 0 && !r.waitBeforeRetry() {
			break
		}
		attemptCtx, cancel := context.WithTimeout(base, r.writePolicy.timeout)
		lastErr = write(attemptCtx)
		cancel()
		if lastErr == nil {
			return nil
		}
	}
	return lastErr
}

// waitBeforeRetry waits between terminal-write attempts and reports false when
// the runner is shutting down.
func (r *Runner) waitBeforeRetry() bool {
	if r.writePolicy.backoff <= 0 {
		return true
	}
	timer := time.NewTimer(r.writePolicy.backoff)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-r.backgroundContext().Done():
		return false
	}
}

// abandoned reports whether a non-terminal run has outlived any live pass: it is
// still pending/running after the progress-less time limit. A run with no
// recorded age is never classified as abandoned, so an unknown age cannot close
// a run the runner may still own.
func (r *Runner) abandoned(run *models.TaskReviewRun) bool {
	if run == nil || run.Status.IsTerminal() || run.CreatedAt.IsZero() {
		return false
	}
	return time.Since(run.CreatedAt) > staleRunLimit
}

// finalizeAbandoned records an abandoned run as failed so it stops counting as
// an active pass. It retries like any terminal write and reports the outcome.
func (r *Runner) finalizeAbandoned(ctx context.Context, run *models.TaskReviewRun) error {
	err := r.writeTerminal(ctx, func(wctx context.Context) error {
		_, werr := r.store.FailRun(wctx, run.ID, CodeAbandoned,
			"review run made no progress within the progress-less time limit", 0)
		return werr
	})
	if err != nil {
		r.logger.Error("finalize abandoned review run",
			zap.String("task_id", run.TaskID), zap.String("run_id", run.ID), zap.Error(err))
		return err
	}
	r.logger.Warn("finalized abandoned review run",
		zap.String("task_id", run.TaskID), zap.String("run_id", run.ID))
	return nil
}

// Start registers the runner's background context.
func (r *Runner) Start(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return
	}
	r.ctx, r.cancel = context.WithCancel(ctx)
	r.started = true
}

// Stop cancels in-flight passes and waits for them to drain. Idempotent.
func (r *Runner) Stop() {
	r.mu.Lock()
	cancel := r.cancel
	r.started = false
	r.cancel = nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	r.wg.Wait()
}

// runTimeout bounds a single review pass so a wedged provider cannot pin the
// in-flight slot for a task forever.
const runTimeout = 10 * time.Minute

// staleRunLimit is the progress-less time limit for a non-terminal run. A live
// pass cannot outlive runTimeout, so a row still pending/running past this limit
// has no goroutine behind it and is abandoned. The margin keeps a pass the
// runner still owns from being misclassified while it records its outcome.
const staleRunLimit = 2 * runTimeout

// terminalWritePolicy bounds how a pass records its terminal outcome. Each
// attempt writes on a context detached from the pass, so the pass deadline or a
// caller disconnect cannot cancel the write that clears pending/running.
type terminalWritePolicy struct {
	attempts int
	timeout  time.Duration
	backoff  time.Duration
}

var defaultTerminalWritePolicy = terminalWritePolicy{
	attempts: 3,
	timeout:  10 * time.Second,
	backoff:  time.Second,
}

// Launch starts a review pass in the background and returns the pending run, so
// the caller's request does not block on inference.
//
// A task that already has a pending or running pass returns that run untouched —
// re-clicking Review must not fan out into duplicate provider calls.
func (r *Runner) Launch(ctx context.Context, req RunRequest) (*models.TaskReviewRun, error) {
	run, _, err := r.launch(ctx, req)
	return run, err
}

// resolveIdentity picks the reviewer. A workflow-step caller that requires an
// origin fails closed when its writer identity is unknown, so an unreadable
// origin never becomes a same-model review.
func (r *Runner) resolveIdentity(ctx context.Context, req RunRequest) (ReviewerIdentity, error) {
	if req.OriginRequired {
		return r.resolver.ResolveRequiredOrigin(ctx, req.AgentProfileID, req.Origin)
	}
	return r.resolver.ResolveExcluding(ctx, req.AgentProfileID, req.Origin)
}

// launch does the work of Launch and additionally returns a channel closed when
// the pass finishes, so a synchronous caller can wait for its own run instead of
// every run in the process.
func (r *Runner) launch(ctx context.Context, req RunRequest) (*models.TaskReviewRun, <-chan struct{}, error) {
	if req.TaskID == "" {
		return nil, nil, taskservice.ErrTaskIDRequired
	}

	// Claim the task's slot before touching the DB. Claiming after CreateRun
	// would let two concurrent launches both insert a run and leave the loser's
	// row pending forever with no goroutine behind it.
	if !r.claim(req.TaskID) {
		existing, err := r.store.ActiveRun(ctx, req.TaskID)
		if err != nil {
			return nil, nil, err
		}
		if existing == nil {
			// Claimed by a launch that has not created its row yet; report the
			// in-flight pass rather than starting a competing one.
			return nil, nil, fmt.Errorf("%w: a review is already starting for this task", ErrExecutionFailed)
		}
		return existing, nil, nil
	}
	// Every early return below must free the slot; only the launched goroutine
	// keeps it.
	launched := false
	defer func() {
		if !launched {
			r.release(req.TaskID)
		}
	}()

	// A pass that survived a restart has a DB row but no in-memory claim, so the
	// claim above succeeds. Rejoin it instead of starting a second one.
	existing, err := r.store.ActiveRun(ctx, req.TaskID)
	if err != nil {
		// Fail closed: an unreadable active-run set may already hold a row, and
		// creating a second run would break the single-active-run invariant.
		return nil, nil, err
	}
	if existing != nil {
		if !r.abandoned(existing) {
			return existing, nil, nil
		}
		// The row has outlived any live pass, so it is abandoned: record it and
		// let the requested pass start instead of blocking on it forever. If the
		// abandonment cannot be recorded, keep the single-active-run invariant
		// and report the existing run rather than start a duplicate.
		if err := r.finalizeAbandoned(ctx, existing); err != nil {
			return existing, nil, nil
		}
	}

	// A redelivered step entry must rejoin the run it already created — even a
	// completed one — rather than start a duplicate. The ActiveRun check above
	// only catches a still-pending/running pass, so a redelivery after
	// completion needs its own check keyed on entry identity.
	if req.EntryID != "" {
		existing, err := r.store.FindRunByEntryID(ctx, req.EntryID)
		if err != nil {
			return nil, nil, err
		}
		if existing != nil {
			return existing, nil, nil
		}
	}

	sessionID, err := r.resolveSessionID(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	req.SessionID = sessionID

	// Resolve the reviewer and read the diff before creating a run row: both
	// "no capable agent" and "nothing to review" are conditions the user should
	// see immediately, and neither deserves a failed run in the history.
	identity, err := r.resolveIdentity(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	files, err := r.collectRequestChanges(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("%w: the task has no changed files to review", ErrNoChanges)
	}

	run, err := r.store.CreateRun(ctx, taskservice.CreateRunRequest{
		TaskID:         req.TaskID,
		SessionID:      sessionID,
		Trigger:        req.Trigger,
		WorkflowStepID: req.WorkflowStepID,
		AgentID:        identity.AgentID,
		Model:          identity.Model,
		EntryID:        req.EntryID,
	})
	if err != nil {
		return nil, nil, err
	}

	runCtx, cancel := context.WithTimeout(r.backgroundContext(), r.passTimeout)
	done := r.attach(req.TaskID, run.ID, cancel)
	launched = true

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer r.release(req.TaskID)
		defer cancel()
		if err := r.execute(runCtx, req, run.ID, identity, files); err != nil {
			r.logger.Warn("review run failed",
				zap.String("task_id", req.TaskID), zap.String("run_id", run.ID), zap.Error(err))
		}
	}()
	return run, done, nil
}

// Cancel stops a review pass and records the cancellation.
//
// Cancelling the live context is the half that matters: marking only the DB row
// would let the goroutine finish inference and overwrite the cancelled status
// with a completed one, publishing findings the user already declined.
func (r *Runner) Cancel(ctx context.Context, runID string) (*models.TaskReviewRun, error) {
	r.cancelInFlight(runID)
	return r.store.CancelRun(ctx, runID)
}

// backgroundContext returns the runner's own context so a detached pass outlives
// the originating request but still stops on shutdown.
func (r *Runner) backgroundContext() context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctx != nil {
		return r.ctx
	}
	return context.Background()
}

// claim takes the task's single review slot before any DB row exists.
//
// Claiming first is what closes the duplicate-row race: if the slot were taken
// only after CreateRun, two concurrent launches could both create a run and the
// loser would return a pending row with no goroutine behind it, leaving a review
// that never completes.
func (r *Runner) claim(taskID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, busy := r.inFlight[taskID]; busy {
		return false
	}
	r.inFlight[taskID] = &inFlightRun{done: make(chan struct{})}
	return true
}

// attach records the run id and cancel handle on an already-claimed slot and
// returns its done channel.
func (r *Runner) attach(taskID, runID string, cancel context.CancelFunc) <-chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.inFlight[taskID]
	if entry == nil {
		return nil
	}
	entry.runID = runID
	entry.cancel = cancel
	return entry.done
}

// release frees the task's slot and wakes anyone waiting on the pass.
func (r *Runner) release(taskID string) {
	r.mu.Lock()
	entry := r.inFlight[taskID]
	delete(r.inFlight, taskID)
	r.mu.Unlock()
	if entry != nil {
		close(entry.done)
	}
}

// cancelInFlight cancels the live pass for runID, if this process owns it.
// Returns true when a goroutine was actually signalled.
func (r *Runner) cancelInFlight(runID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, entry := range r.inFlight {
		if entry.runID == runID && entry.cancel != nil {
			entry.cancel()
			return true
		}
	}
	return false
}

// resolveSessionID prefers the caller's session and otherwise asks the lookup.
func (r *Runner) resolveSessionID(ctx context.Context, req RunRequest) (string, error) {
	if req.SessionID != "" {
		return req.SessionID, nil
	}
	if r.sessions == nil {
		return "", fmt.Errorf("%w: no session is available for this task", ErrWorkspaceUnavailable)
	}
	sessionID, err := r.sessions.ReviewSessionID(ctx, req.TaskID)
	if err != nil {
		return "", fmt.Errorf("%w: resolve session: %v", ErrWorkspaceUnavailable, err)
	}
	if sessionID == "" {
		return "", fmt.Errorf("%w: the task has no session to read changes from", ErrWorkspaceUnavailable)
	}
	return sessionID, nil
}

// Run performs a review pass synchronously. Used by tests and by callers that
// want to await the result; Launch is the normal entry point.
func (r *Runner) Run(ctx context.Context, req RunRequest) (*models.TaskReviewRun, error) {
	run, done, err := r.launch(ctx, req)
	if err != nil {
		return nil, err
	}
	// Wait only for this task's pass. Waiting on the shared WaitGroup would
	// block on every other task's review too.
	if done != nil {
		<-done
	}
	return run, nil
}

// execute is the body of one pass. Every failure path records the outcome on the
// run row before returning, so the UI always has a reason to show.
func (r *Runner) execute(ctx context.Context, req RunRequest, runID string, identity ReviewerIdentity, files []ChangedFile) error {
	started := time.Now()
	if _, err := r.store.MarkRunRunning(ctx, runID); err != nil {
		return err
	}

	promptCtx, err := r.promptContext(ctx, req)
	if err != nil {
		return r.fail(ctx, runID, linkRetry(req, err), started)
	}

	plan := PlanBatches(files, r.budgetBytes)
	accumulated, identity, err := r.reviewWithLaunchFallback(ctx, plan, identity, req, promptCtx)
	if err != nil {
		// Record the failure first. A retry on another candidate is only safe
		// once this run is terminal, or a failed finalization would leave two
		// active rows for the task.
		if r.recordFailure(ctx, runID, linkRetry(req, err), started) {
			r.maybeRetryAfterTimeout(ctx, err, req, identity, runID, files)
		}
		return err
	}

	// A cancel that landed while inference was running must win: publishing here
	// would store findings the user already declined, and CompleteRun below would
	// be a no-op against the cancelled row, leaving orphaned findings.
	if err := ctx.Err(); err != nil {
		r.logger.Debug("review run cancelled before publishing",
			zap.String("run_id", runID), zap.Error(err))
		return nil
	}

	index := FileByKey(files)
	inputs := anchorFindings(accumulated.findings, index)
	summary := buildRunSummary(accumulated, plan, len(inputs))
	if req.retryOfRunID != "" {
		summary = fmt.Sprintf("Automatic retry of run %s after a review timeout. %s", req.retryOfRunID, summary)
	}

	if len(inputs) > 0 {
		if _, _, pubErr := r.store.PublishFindings(ctx, taskservice.PublishFindingsRequest{
			TaskID:    req.TaskID,
			RunID:     runID,
			SessionID: req.SessionID,
			Trigger:   req.Trigger,
			Summary:   summary,
			Findings:  inputs,
		}); pubErr != nil {
			// PublishFindings runs on the pass context, so a deadline here is the
			// pass overrunning its budget: record it and take the same one-shot
			// candidate fallback as an inference-phase timeout.
			if r.recordFailure(ctx, runID, linkRetry(req, pubErr), started) {
				r.maybeRetryAfterTimeout(ctx, pubErr, req, identity, runID, files)
			}
			return pubErr
		}
	}

	completeErr := r.writeTerminal(ctx, func(wctx context.Context) error {
		_, werr := r.store.CompleteRun(wctx, taskservice.CompleteRunRequest{
			AgentID:         identity.AgentID,
			Model:           identity.Model,
			RunID:           runID,
			Summary:         summary,
			FindingCount:    len(inputs),
			FileCount:       plan.FileCount(),
			RepositoryCount: RepositoryCount(files),
			PromptTokens:    accumulated.promptTokens,
			ResponseTokens:  accumulated.responseTokens,
			DurationMs:      int(time.Since(started).Milliseconds()),
		})
		return werr
	})
	if completeErr != nil {
		// The completion write failed on every attempt. Record a failure so the
		// run does not stay running with no reason.
		return r.fail(ctx, runID, linkRetry(req, fmt.Errorf("%w: record completion: %w", ErrExecutionFailed, completeErr)), started)
	}
	return nil
}

// linkRetry prefixes a failure with the run this pass retries, so an automatic
// timeout fallback is visibly tied to the timed-out run.
func linkRetry(req RunRequest, err error) error {
	if req.retryOfRunID == "" || err == nil {
		return err
	}
	return fmt.Errorf("retry of run %s: %w", req.retryOfRunID, err)
}

// maybeRetryAfterTimeout runs one automatic fallback when a pass ended on a
// deadline: the dynamic route's next candidate runs the same review once. It
// never runs for any other failure, never repeats a model that already timed
// out, and never retries a retry.
//
// Called from the pass goroutine, so the task's in-flight slot stays held and
// the cancel handle is repointed at the retry run.
func (r *Runner) maybeRetryAfterTimeout(ctx context.Context, cause error, req RunRequest, timedOut ReviewerIdentity, origRunID string, files []ChangedFile) {
	if req.retryOfRunID != "" || !isRunTimeout(ctx, cause) {
		return
	}
	base := r.backgroundContext()
	setupCtx, cancelSetup := context.WithTimeout(base, retrySetupTimeout)
	defer cancelSetup()
	// Exclude the reviewer that timed out so the retry is a different candidate,
	// in the route's existing order.
	next, err := r.resolver.nextDynamicReviewer(setupCtx, timedOut, req.Origin, map[string]bool{timedOut.ProfileID: true})
	if err != nil {
		r.logger.Info("no reviewer candidate for a timed-out review",
			zap.String("task_id", req.TaskID), zap.String("run_id", origRunID), zap.Error(err))
		return
	}
	retryReq := req
	retryReq.retryOfRunID = origRunID
	// The retry is not a new ledger entry: reusing the entry id would collide
	// with the unique index and break step-entry redelivery dedup.
	retryReq.EntryID = ""
	retryRun, err := r.store.CreateRun(setupCtx, taskservice.CreateRunRequest{
		TaskID:         req.TaskID,
		SessionID:      req.SessionID,
		Trigger:        req.Trigger,
		WorkflowStepID: req.WorkflowStepID,
		AgentID:        next.AgentID,
		Model:          next.Model,
	})
	if err != nil {
		r.logger.Error("create review retry run", zap.String("run_id", origRunID), zap.Error(err))
		return
	}
	retryCtx, cancelRetry := context.WithTimeout(base, r.passTimeout)
	r.attach(req.TaskID, retryRun.ID, cancelRetry)
	defer cancelRetry()
	if err := r.execute(retryCtx, retryReq, retryRun.ID, next, files); err != nil {
		r.logger.Warn("review retry failed",
			zap.String("task_id", req.TaskID), zap.String("run_id", retryRun.ID), zap.Error(err))
	}
}

// isRunTimeout reports whether a pass ended because its own deadline expired,
// the only failure that triggers the automatic candidate fallback.
func isRunTimeout(ctx context.Context, err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || (ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded))
}

// retrySetupTimeout bounds the resolve-and-create work before a fallback pass.
const retrySetupTimeout = 30 * time.Second

func (r *Runner) promptContext(ctx context.Context, req RunRequest) (PromptContext, error) {
	if r.taskContext == nil {
		return PromptContext{}, nil
	}
	promptCtx, err := r.taskContext.ReviewPromptContext(ctx, req.TaskID, req.SessionID)
	if err != nil {
		// Task metadata is nice-to-have context, not a reason to fail a review.
		r.logger.Warn("review prompt context unavailable",
			zap.String("task_id", req.TaskID), zap.Error(err))
		return PromptContext{}, nil
	}
	return promptCtx, nil
}

// accumulator collects results across prompt batches.
type accumulator struct {
	findings       []FindingInput
	summaries      []string
	rejected       int
	promptTokens   int
	responseTokens int
}

func (r *Runner) reviewBatches(ctx context.Context, plan BatchPlan, identity ReviewerIdentity, sessionID string, promptCtx PromptContext) (accumulator, error) {
	acc := accumulator{}
	for i, batch := range plan.Batches {
		if err := ctx.Err(); err != nil {
			// Keep context.Canceled / DeadlineExceeded in the chain so fail() can
			// leave a user cancel as cancelled instead of overwriting it failed.
			return acc, fmt.Errorf("%w: review cancelled after batch %d: %w", ErrExecutionFailed, i, err)
		}
		prompt, err := r.prompts.Build(ctx, batch, promptCtx)
		if err != nil {
			return acc, fmt.Errorf("%w: build prompt: %w", ErrExecutionFailed, err)
		}
		result, err := r.inference.Run(ctx, identity, sessionID, prompt)
		if err != nil {
			// Preserve the inference error chain (esp. context cancellation). Using
			// %v here used to drop context.Canceled, so fail() marked cancelled runs
			// as failed when CancelRun lost the race with FailRun.
			return acc, fmt.Errorf("%w: %w", ErrExecutionFailed, err)
		}
		if result == nil {
			return acc, fmt.Errorf("%w: reviewer returned no result", ErrExecutionFailed)
		}
		acc.promptTokens += result.PromptTokens
		acc.responseTokens += result.ResponseTokens

		parsed, err := ParseFindings(result.Response)
		if err != nil {
			// One unreadable batch fails the run: reporting a partial review as
			// complete would read as an all-clear for the files we could not parse.
			return acc, fmt.Errorf("%w: batch %d: %w", ErrUnparseableResponse, i+1, err)
		}
		acc.findings = append(acc.findings, parsed.Findings...)
		acc.rejected += parsed.Rejected
		if parsed.Summary != "" {
			acc.summaries = append(acc.summaries, parsed.Summary)
		}
	}
	return acc, nil
}

func (r *Runner) fail(ctx context.Context, runID string, cause error, started time.Time) error {
	r.recordFailure(ctx, runID, cause, started)
	if cause != nil {
		return cause
	}
	return ctx.Err()
}

// recordFailure persists a run's failure and reports whether the row reached a
// terminal state. A caller that starts further work for the same task — the
// timeout fallback — must wait for this to be true, or a failed finalization
// would leave two active rows.
func (r *Runner) recordFailure(ctx context.Context, runID string, cause error, started time.Time) bool {
	// An explicit cancel already recorded the terminal state, so re-reporting it
	// as a failure would only churn the row (and would overwrite it on any store
	// without a terminal guard). A deadline is different: nobody recorded that.
	// Check both the wrapped cause and the run context: wrappers historically used
	// %v and dropped context.Canceled from the chain.
	if isReviewCanceled(cause) || isReviewCanceled(ctx.Err()) {
		r.logger.Debug("review run unwound after cancellation", zap.String("run_id", runID))
		return true
	}
	code := CodeFor(cause)
	message := cause.Error()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		// The pass's own deadline fired: record it as a timeout with its own
		// reason, not a user cancel. A storage write that timed out on a
		// detached context leaves the pass context alive, so it is not this
		// case and keeps its own execution-failure reason.
		code = CodeTimeout
		message = fmt.Sprintf("review pass exceeded its %s time budget: %v", r.passTimeout, cause)
	}
	// Persist the failure on a detached, retried write: a single attempt can lose
	// to a busy writer and leave the run stuck as "running" forever.
	if err := r.writeTerminal(ctx, func(wctx context.Context) error {
		_, werr := r.store.FailRun(wctx, runID, code, message, int(time.Since(started).Milliseconds()))
		return werr
	}); err != nil {
		r.logger.Error("record review run failure", zap.String("run_id", runID), zap.Error(err))
		return false
	}
	return true
}

// isReviewCanceled reports a user/cancel signal (context.Canceled), not a
// deadline. DeadlineExceeded also satisfies errors.Is(..., Canceled) in some
// chains, so it is excluded explicitly.
func isReviewCanceled(err error) bool {
	return err != nil && errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// anchorFindings attaches the authoritative diff hash and anchor text from the
// change set to each reported finding, and drops findings naming a file the
// reviewer was never shown.
//
// The hash must come from the change set rather than the reviewer: it is what
// staleness is computed against, so it has to describe the diff that was
// actually reviewed.
func anchorFindings(reported []FindingInput, index map[string]ChangedFile) []taskservice.ReviewFindingInput {
	inputs := make([]taskservice.ReviewFindingInput, 0, len(reported))
	for _, f := range reported {
		file, ok := lookupReportedFile(f, index)
		if !ok {
			continue
		}
		inputs = append(inputs, taskservice.ReviewFindingInput{
			RepositoryID:   file.RepositoryID,
			RepositoryName: file.RepositoryName,
			FilePath:       file.Path,
			StartLine:      f.Line,
			EndLine:        f.LineEnd,
			Side:           f.Side,
			Severity:       f.Severity,
			Category:       f.Category,
			Title:          f.Title,
			Body:           f.Body,
			Suggestion:     f.Suggestion,
			AnchorText:     TruncateAnchorText(ExtractAnchorText(file.Diff, f.Line, f.LineEnd)),
			FileDiffHash:   file.DiffHash,
		})
	}
	return inputs
}

// lookupReportedFile matches a reported (repo, file) pair against the change set,
// tolerating a reviewer that omitted or guessed the repository prefix.
func lookupReportedFile(f FindingInput, index map[string]ChangedFile) (ChangedFile, bool) {
	if f.Repo != "" {
		if file, ok := index[f.Repo+fileKeySep+f.File]; ok {
			return file, true
		}
	}
	if file, ok := index[f.File]; ok {
		return file, true
	}
	// Unprefixed path in a multi-repo change set: accept it only when exactly one
	// repository contains that path, so a finding is never attributed to the
	// wrong repository.
	var match ChangedFile
	found := 0
	for _, file := range index {
		if file.Path == f.File {
			match = file
			found++
		}
	}
	if found == 1 {
		return match, true
	}
	return ChangedFile{}, false
}

// buildRunSummary combines the reviewer's prose with the mechanical facts the
// user needs to trust the result: what was skipped and what was rejected.
func buildRunSummary(acc accumulator, plan BatchPlan, stored int) string {
	parts := make([]string, 0, 4)
	if joined := strings.TrimSpace(strings.Join(acc.summaries, "\n\n")); joined != "" {
		parts = append(parts, joined)
	}
	if len(plan.Skipped) > 0 {
		names := make([]string, 0, len(plan.Skipped))
		for _, f := range plan.Skipped {
			names = append(names, f.Key())
		}
		parts = append(parts, fmt.Sprintf("Skipped %d file(s) whose diff was too large to review: %s.",
			len(plan.Skipped), strings.Join(names, ", ")))
	}
	if acc.rejected > 0 {
		parts = append(parts, fmt.Sprintf("Discarded %d malformed finding(s) returned by the reviewer.", acc.rejected))
	}
	if dropped := len(acc.findings) - stored; dropped > 0 {
		parts = append(parts, fmt.Sprintf("Discarded %d finding(s) anchored to files outside the reviewed change set.", dropped))
	}
	return strings.Join(parts, " ")
}
