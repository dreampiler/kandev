package subproc

import (
	"context"
	"os"
	"os/exec"
	"sync"
)

// gitPathMemo remembers where "git" resolved under the current PATH. Resolving
// it for every command walks PATH x PATHEXT on Windows (hundreds of file
// checks per command on a long PATH), and the workspace pollers build many Git
// commands per second across sessions. The remembered path is re-checked with
// a single stat and resolved again when PATH changes or when the remembered
// file is gone (Git moved by an update or reinstall).
var gitPathMemo struct {
	sync.Mutex
	path    string
	pathEnv string
}

func rememberedGitPath(pathEnv string) string {
	gitPathMemo.Lock()
	defer gitPathMemo.Unlock()
	if gitPathMemo.pathEnv != pathEnv {
		return ""
	}
	return gitPathMemo.path
}

func resolveGitPath(pathEnv string) (string, error) {
	resolved, err := exec.LookPath("git")
	gitPathMemo.Lock()
	defer gitPathMemo.Unlock()
	if err != nil {
		gitPathMemo.path, gitPathMemo.pathEnv = "", ""
		return "", err
	}
	gitPathMemo.path, gitPathMemo.pathEnv = resolved, pathEnv
	return resolved, nil
}

// GitExecutablePath resolves the Git binary for helper processes that need to
// preserve a file-descriptor-based working directory. Keeping the lookup in
// the Git seam lets the repository audit cover both command construction and
// executable discovery.
func GitExecutablePath() (string, error) {
	pathEnv := os.Getenv("PATH")
	if remembered := rememberedGitPath(pathEnv); remembered != "" {
		if _, err := os.Stat(remembered); err == nil {
			return remembered, nil
		}
	}
	return resolveGitPath(pathEnv)
}

// NewGitCommand is the only production Git command-construction seam. Callers
// must pass the returned command to a classified RunGit* helper (or hold a
// classified slot around a streaming Start/Wait lifecycle).
//
// The executable is the remembered Git path, checked with one stat (exec does
// not stat an absolute path that already carries an extension). When Git
// cannot be resolved at all the command falls back to "git" so exec reports
// the same not-found error as before. argv[0] stays "git". Git never invokes a
// shell for Cmd.Args. Callers remain responsible for validating
// user-controlled refs, paths, and option values before they reach this seam.
func NewGitCommand(ctx context.Context, args ...string) *exec.Cmd {
	name := "git"
	if resolved, err := GitExecutablePath(); err == nil {
		name = resolved
	}
	cmd := exec.CommandContext(ctx, name)
	cmd.Args = append([]string{"git"}, args...)
	return cmd
}
