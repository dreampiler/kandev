package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/kandev/kandev/internal/auth/authn"
)

// deferredLaunchDiscarderSpy records whether the handler reached the discard
// surface and returns a canned outcome.
type deferredLaunchDiscarderSpy struct {
	called    bool
	discarded bool
	err       error
}

func (s *deferredLaunchDiscarderSpy) DiscardDeferredCeilingLaunch(context.Context, string) (bool, error) {
	s.called = true
	return s.discarded, s.err
}

func discardDeferredLaunchRequest(t *testing.T, userID, taskID string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+taskID+"/deferred-launch/discard", nil)
	req = req.WithContext(authn.WithIdentity(
		req.Context(), authn.Identity{UserID: userID, Role: authn.RoleMember}))
	c.Request = req
	c.Params = gin.Params{{Key: "id", Value: taskID}}
	return c, rec
}

// TestHTTPDiscardDeferredLaunchDeniesForeignTask proves the route is scoped:
// a caller who cannot reach the task gets 404 and never reaches the discard
// surface.
func TestHTTPDiscardDeferredLaunchDeniesForeignTask(t *testing.T) {
	repo := &authzDeleteRepo{}
	h := newAuthzTaskHandlers(t, repo)
	spy := &deferredLaunchDiscarderSpy{discarded: true}
	h.deferredLaunchDiscarder = spy

	c, rec := discardDeferredLaunchRequest(t, "user-a", "task-b")
	h.httpDiscardDeferredLaunch(c)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.False(t, spy.called, "a denied release must not reach the discard surface")
}

// TestHTTPDiscardDeferredLaunchReleasesForOwner proves the owner's request
// reaches the discard surface and reports the discarded verdict.
func TestHTTPDiscardDeferredLaunchReleasesForOwner(t *testing.T) {
	repo := &authzDeleteRepo{}
	h := newAuthzTaskHandlers(t, repo)
	spy := &deferredLaunchDiscarderSpy{discarded: true}
	h.deferredLaunchDiscarder = spy

	c, rec := discardDeferredLaunchRequest(t, "user-b", "task-b")
	h.httpDiscardDeferredLaunch(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, spy.called)
	require.JSONEq(t, `{"discarded":true}`, rec.Body.String())
}

// TestHTTPDiscardDeferredLaunchNoRecordIsNoOp proves a task with no deferred
// record answers 200 with discarded=false, so a retried release is idempotent.
func TestHTTPDiscardDeferredLaunchNoRecordIsNoOp(t *testing.T) {
	repo := &authzDeleteRepo{}
	h := newAuthzTaskHandlers(t, repo)
	spy := &deferredLaunchDiscarderSpy{discarded: false}
	h.deferredLaunchDiscarder = spy

	c, rec := discardDeferredLaunchRequest(t, "user-b", "task-b")
	h.httpDiscardDeferredLaunch(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, spy.called)
	require.JSONEq(t, `{"discarded":false}`, rec.Body.String())
}

// TestHTTPDiscardDeferredLaunchUnavailableWithoutOrchestrator proves the route
// fails closed (503) when no discard surface is wired, rather than silently
// reporting success.
func TestHTTPDiscardDeferredLaunchUnavailableWithoutOrchestrator(t *testing.T) {
	repo := &authzDeleteRepo{}
	h := newAuthzTaskHandlers(t, repo)

	c, rec := discardDeferredLaunchRequest(t, "user-b", "task-b")
	h.httpDiscardDeferredLaunch(c)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}
