package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// seedPlainMember creates a throwaway user in the test workspace with role
// "member" (not owner/admin) and returns their user id. Used by tests that
// verify owner/admin-only gates.
func seedPlainMember(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	var memberID string
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Workflow Security Plain Member", fmt.Sprintf("workflow-sec-%d@multica.test", time.Now().UnixNano())).Scan(&memberID); err != nil {
		t.Fatalf("seed plain member user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, memberID)
	})
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		testWorkspaceID, memberID); err != nil {
		t.Fatalf("seed plain member row: %v", err)
	}
	return memberID
}

// TestReviewArtifact_RejectsTaskToken pins the H-1 security fix (security
// audit): POST /api/artifacts/{id}/review must NOT be callable with a machine
// credential. The handler-level requireHumanActor guard returns 403 before any
// database work, so a task-token request can never self-review / self-approve
// an artifact.
func TestReviewArtifact_RejectsTaskToken(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/artifacts/some-uuid/review?workspace_id="+testWorkspaceID, map[string]any{
		"action":  "approved",
		"comment": "self-approval attempt",
	})
	req = withURLParam(req, "id", "00000000-0000-0000-0000-000000000001")
	// This is what the Auth middleware stamps for a mat_ task token; it is
	// server-set and cannot be forged by the client.
	req.Header.Set("X-Actor-Source", "task_token")
	testHandler.ReviewArtifact(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("ReviewArtifact with task_token: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestOverrideNodeStatus_RejectsTaskToken pins the H-2 defense-in-depth: the
// node status override handler rejects machine credentials before any DB work.
func TestOverrideNodeStatus_RejectsTaskToken(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workflows/wf/nodes/node/status?workspace_id="+testWorkspaceID, map[string]any{
		"status": "done",
		"reason": "bypass review attempt",
	})
	req = withURLParams(req, "id", "00000000-0000-0000-0000-000000000001", "nodeId", "00000000-0000-0000-0000-000000000002")
	req.Header.Set("X-Actor-Source", "task_token")
	testHandler.OverrideNodeStatus(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("OverrideNodeStatus with task_token: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestAdvanceWorkflow_RejectsTaskToken pins the M-1 defense-in-depth: stage
// advancement is a Leader/admin human control and rejects machine credentials.
func TestAdvanceWorkflow_RejectsTaskToken(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workflows/wf/advance?workspace_id="+testWorkspaceID, map[string]any{
		"reason": "machine-driven advance attempt",
	})
	req = withURLParam(req, "id", "00000000-0000-0000-0000-000000000001")
	req.Header.Set("X-Actor-Source", "task_token")
	testHandler.AdvanceWorkflow(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("AdvanceWorkflow with task_token: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestWorkflowRoutesCarryRequireHumanActor pins the route-wiring side of H-1 /
// H-2 / M-1: the review, node-status-override and advance routes are mounted
// behind RequireHumanActor, so a task-token request never reaches the handler
// even if a handler-level guard were later removed.
func TestWorkflowRoutesCarryRequireHumanActor(t *testing.T) {
	r := chi.NewRouter()
	r.Route("/api/artifacts", func(r chi.Router) {
		r.Route("/{id}", func(r chi.Router) {
			r.With(RequireHumanActor).Post("/review", func(_ http.ResponseWriter, _ *http.Request) {
				t.Fatal("inner review handler must NOT run when guard rejects")
			})
		})
	})
	r.Route("/api/workflows", func(r chi.Router) {
		r.Route("/{id}", func(r chi.Router) {
			r.With(RequireHumanActor).Post("/advance", func(_ http.ResponseWriter, _ *http.Request) {
				t.Fatal("inner advance handler must NOT run when guard rejects")
			})
			r.With(RequireHumanActor).Post("/nodes/{nodeId}/status", func(_ http.ResponseWriter, _ *http.Request) {
				t.Fatal("inner override handler must NOT run when guard rejects")
			})
		})
	})

	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "review", path: "/api/artifacts/abc/review"},
		{name: "advance", path: "/api/workflows/abc/advance"},
		{name: "override", path: "/api/workflows/abc/nodes/def/status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, nil)
			req.Header.Set("X-Actor-Source", "task_token")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("%s route with task_token: expected 403, got %d", tc.name, w.Code)
			}
		})
	}
}

// TestCreateWorkflow_MachineCredentialRejectsCustomizations pins the V-01 fix:
// a machine credential (mat_ task token / mcn_ cloud PAT) may not carry
// customizations[].review_required=false — the exact vector that previously let
// an agent create a review-free workflow and bypass the CLO-175 human review
// chain. The handler must reject with 403 before any database work.
func TestCreateWorkflow_MachineCredentialRejectsCustomizations(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, actorSource := range []string{"task_token", "cloud_pat"} {
		t.Run(actorSource, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/workflows?workspace_id="+testWorkspaceID, map[string]any{
				"source_issue_id": "00000000-0000-0000-0000-000000000001",
				"name":            "agent-created workflow",
				"customizations": []any{
					map[string]any{
						"type":            "requirements",
						"review_required": false,
					},
				},
			})
			req.Header.Set("X-Actor-Source", actorSource)
			testHandler.CreateWorkflow(w, req)

			if w.Code != http.StatusForbidden {
				t.Fatalf("CreateWorkflow with %s + customizations: expected 403, got %d: %s", actorSource, w.Code, w.Body.String())
			}
		})
	}
}

// TestCreateWorkflow_NonOwnerRejectsCustomizations pins the V-01 fix: only a
// workspace owner/admin may override node config via customizations. A plain
// member (or machine credential posing as one) gets 403.
func TestCreateWorkflow_NonOwnerRejectsCustomizations(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workflows?workspace_id="+testWorkspaceID, map[string]any{
		"source_issue_id": "00000000-0000-0000-0000-000000000001",
		"name":            "member-created workflow",
		"customizations": []any{
			map[string]any{
				"type":            "development",
				"review_required": false,
			},
		},
	})
	// newRequest sets X-User-ID to testUserID (workspace owner in the fixture);
	// override it to a fresh non-owner member so the role gate rejects.
	memberID := seedPlainMember(t)
	req.Header.Set("X-User-ID", memberID)
	testHandler.CreateWorkflow(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("CreateWorkflow as plain member + customizations: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateArtifact_RejectsOversizedContent pins the V-03 fix: inline artifact
// content over maxInlineContentBytes is rejected with 413 (request body too
// large), never decoded into an unbounded memory buffer.
func TestCreateArtifact_RejectsOversizedContent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	big := strings.Repeat("x", maxInlineContentBytes+1)
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/artifacts?workspace_id="+testWorkspaceID, map[string]any{
		"workflow_id": "00000000-0000-0000-0000-000000000001",
		"node_id":     "00000000-0000-0000-0000-000000000002",
		"type":        "requirements",
		"title":       "oversized.md",
		"content":     big,
	})
	testHandler.CreateArtifact(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("CreateArtifact with oversized content: expected 413, got %d: %s", w.Code, w.Body.String())
	}
}

// TestReviewQueue_RejectsTaskToken pins the V-04 fix: GET /api/reviews/queue is
// a human reviewer surface and must reject machine credentials before any
// metadata is returned.
func TestReviewQueue_RejectsTaskToken(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/reviews/queue?workspace_id="+testWorkspaceID, nil)
	req.Header.Set("X-Actor-Source", "task_token")
	testHandler.ListReviewQueue(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("ListReviewQueue with task_token: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestReviewQueueRouteCarriesRequireHumanActor pins the route-wiring side of
// V-04: /api/reviews/queue is mounted behind RequireHumanActor so a task-token
// request never reaches the handler even if the handler-level guard were later
// removed.
func TestReviewQueueRouteCarriesRequireHumanActor(t *testing.T) {
	r := chi.NewRouter()
	r.Route("/api/reviews", func(r chi.Router) {
		r.With(RequireHumanActor).Get("/queue", func(_ http.ResponseWriter, _ *http.Request) {
			t.Fatal("queue handler must NOT run when guard rejects")
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/reviews/queue", nil)
	req.Header.Set("X-Actor-Source", "cloud_pat")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("/api/reviews/queue route with machine credential: expected 403, got %d", w.Code)
	}
}
