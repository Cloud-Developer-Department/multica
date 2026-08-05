package workflow

import "testing"

func TestCanTransitionDefaultTable(t *testing.T) {
	cases := []struct {
		from, to string
		want     bool
	}{
		{StatusBacklog, StatusTodo, true},
		{StatusBacklog, StatusDone, false},
		{StatusTodo, StatusInProgress, true},
		{StatusTodo, StatusDone, false},
		{StatusInProgress, StatusInReview, true},
		{StatusInProgress, StatusDone, true},
		{StatusInReview, StatusDone, true},  // via review approval (ArtifactService enforces)
		{StatusInReview, StatusTodo, true},  // via review rejection (ArtifactService enforces)
		{StatusDone, StatusInProgress, false},
		{StatusCancelled, StatusTodo, false},
		{StatusBlocked, StatusTodo, true},
		{StatusBlocked, StatusInProgress, true},
	}
	for _, c := range cases {
		if got := CanTransition(nil, c.from, c.to); got != c.want {
			t.Errorf("CanTransition(nil, %q, %q) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

func TestCanTransitionCustomTable(t *testing.T) {
	trans := map[string][]string{
		StatusTodo: {StatusDone},
	}
	if !CanTransition(trans, StatusTodo, StatusDone) {
		t.Error("custom table: todo -> done should be allowed")
	}
	if CanTransition(trans, StatusTodo, StatusInProgress) {
		t.Error("custom table: todo -> in_progress should be rejected")
	}
	if CanTransition(trans, StatusBacklog, StatusTodo) {
		t.Error("custom table should fall back to default for missing keys")
	}
}

func TestSyncDoneAllowed(t *testing.T) {
	cases := []struct {
		name             string
		reviewRequired   bool
		approvedArtifact bool
		want             bool
	}{
		{"review-free node completes directly", false, false, true},
		{"review-free node with artifact", false, true, true},
		{"review-required node without approved artifact blocked", true, false, false},
		{"review-required node with approved artifact allowed", true, true, true},
	}
	for _, c := range cases {
		if got := SyncDoneAllowed(c.reviewRequired, c.approvedArtifact); got != c.want {
			t.Errorf("%s: SyncDoneAllowed(%v, %v) = %v, want %v", c.name, c.reviewRequired, c.approvedArtifact, got, c.want)
		}
	}
}

func TestStageStatus(t *testing.T) {
	cases := []struct {
		name   string
		status []string
		want   string
	}{
		{"empty", nil, "todo"},
		{"all_done", []string{"done", "done"}, "done"},
		{"done_and_cancelled", []string{"done", "cancelled"}, "done"},
		{"one_blocked", []string{"done", "blocked"}, "blocked"},
		{"one_in_review", []string{"done", "in_review"}, "in_review"},
		{"one_active", []string{"done", "in_progress"}, "in_progress"},
		{"all_backlog", []string{"backlog", "backlog"}, "todo"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := StageStatus(c.status); got != c.want {
				t.Errorf("StageStatus(%v) = %q, want %q", c.status, got, c.want)
			}
		})
	}
}

func TestStageBarrierClosed(t *testing.T) {
	nodes := []NodeBySeq{
		{Seq: 1, Stage: 1, Status: StatusDone},
		{Seq: 2, Stage: 1, Status: StatusDone},
		{Seq: 3, Stage: 2, Status: StatusBacklog},
	}
	if !StageBarrierClosed(nodes, 1) {
		t.Error("stage 1 with all terminal nodes should be closed")
	}
	if StageBarrierClosed(nodes, 2) {
		t.Error("stage 2 with backlog node should be open")
	}
	if StageBarrierClosed(nodes, 3) {
		t.Error("empty stage 3 should not be closed")
	}
}

func TestNextStageToOpen(t *testing.T) {
	nodes := []NodeBySeq{
		{Seq: 1, Stage: 1, Status: StatusDone},
		{Seq: 2, Stage: 2, Status: StatusTodo},
		{Seq: 3, Stage: 3, Status: StatusBacklog},
	}
	if got := NextStageToOpen(nodes, 1); got != 2 {
		t.Errorf("NextStageToOpen(nodes, 1) = %d, want 2", got)
	}
	if got := NextStageToOpen(nodes, 2); got != 3 {
		t.Errorf("NextStageToOpen(nodes, 2) = %d, want 3", got)
	}
	if got := NextStageToOpen(nodes, 3); got != 0 {
		t.Errorf("NextStageToOpen(nodes, 3) = %d, want 0", got)
	}
}

func TestWorkflowInstanceStatus(t *testing.T) {
	cases := []struct {
		name   string
		status []string
		want   string
	}{
		{"no_nodes", nil, WorkflowStatusTodo},
		{"all_done", []string{"done", "done"}, WorkflowStatusDone},
		{"blocked", []string{"done", "blocked"}, WorkflowStatusBlocked},
		{"in_review", []string{"done", "in_review"}, WorkflowStatusInReview},
		{"active", []string{"done", "in_progress"}, WorkflowStatusInProgress},
		{"pending", []string{"backlog", "backlog"}, WorkflowStatusTodo},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := WorkflowInstanceStatus(c.status); got != c.want {
				t.Errorf("WorkflowInstanceStatus(%v) = %q, want %q", c.status, got, c.want)
			}
		})
	}
}
