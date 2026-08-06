package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestListIssues_HierarchyDirectChildCount covers the ?hierarchy=true query
// parameter on GET /api/issues: every row gains a workspace-wide
// direct_child_count (one level, ignoring the list's own filters), while the
// field stays absent when the parameter is not requested.
func TestListIssues_HierarchyDirectChildCount(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Board hierarchy %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	nextNumber := func() int {
		var number int
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace
			SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
			WHERE id = $1 RETURNING issue_counter
		`, testWorkspaceID).Scan(&number); err != nil {
			t.Fatalf("next issue number: %v", err)
		}
		return number
	}

	insertIssue := func(title string, parentID *string) string {
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (
				workspace_id, title, status, priority, creator_type, creator_id,
				parent_issue_id, position, number, project_id
			) VALUES ($1, $2, 'todo', 'none', 'member', $3, $4, 0, $5, $6)
			RETURNING id
		`, testWorkspaceID, title, testUserID, parentID, nextNumber(), projectID).Scan(&id); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
		return id
	}

	parent := insertIssue(fmt.Sprintf("hier-parent-%d", suffix), nil)
	childA := insertIssue(fmt.Sprintf("hier-child-a-%d", suffix), &parent)
	insertIssue(fmt.Sprintf("hier-child-b-%d", suffix), &parent)
	insertIssue(fmt.Sprintf("hier-grandchild-%d", suffix), &childA)
	leaf := insertIssue(fmt.Sprintf("hier-leaf-%d", suffix), nil)

	listCounts := func(hierarchy bool) map[string]*int64 {
		t.Helper()
		path := fmt.Sprintf(
			"/api/issues?workspace_id=%s&project_id=%s&limit=100",
			testWorkspaceID, projectID,
		)
		if hierarchy {
			path += "&hierarchy=true"
		}
		w := httptest.NewRecorder()
		testHandler.ListIssues(w, newRequest("GET", path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("ListIssues: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var response struct {
			Issues []IssueResponse `json:"issues"`
		}
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		counts := map[string]*int64{}
		for _, issue := range response.Issues {
			counts[issue.ID] = issue.DirectChildCount
		}
		return counts
	}

	// Without hierarchy=true the field must be absent (backward compatible).
	plain := listCounts(false)
	for _, issueID := range []string{parent, childA, leaf} {
		if count := plain[issueID]; count != nil {
			t.Fatalf("issue %s: direct_child_count must be absent without hierarchy=true, got %d", issueID, *count)
		}
	}

	// With hierarchy=true every row carries a count; only direct children
	// count (grandchildren are not attributed to the grandparent).
	withHierarchy := listCounts(true)
	expected := map[string]int64{
		parent: 2,
		childA: 1,
		leaf:   0,
	}
	for issueID, want := range expected {
		count := withHierarchy[issueID]
		if count == nil {
			t.Fatalf("issue %s: direct_child_count missing with hierarchy=true", issueID)
		}
		if *count != want {
			t.Fatalf("issue %s direct_child_count = %d, want %d", issueID, *count, want)
		}
	}
}
