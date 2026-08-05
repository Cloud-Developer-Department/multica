package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Tests for the /delegate command (LIU-13): explicit command recognition
// (F1), gate-before-create (F2), child-issue creation + dispatch (F3/F4),
// outcomes with subissue refs (F5), provenance/idempotency via
// issue.delegation_comment_id (F6), preview, and structural-edit blocking.

type delegationFixture struct {
	IssueID string
	TaskID  string
}

// createDelegationIssue creates a main issue assigned to an agent leader and a
// running task for that leader ON the issue — the X-Task-ID an agent author
// sends, which CreateComment stamps as source_task_id and the delegated child
// inherits as origin_id (MUL-4305 originator inheritance, O2).
func createDelegationIssue(t *testing.T, leaderID, title string) delegationFixture {
	t.Helper()
	issueID := createCommentTriggerPreviewIssue(t, title, "agent", leaderID)
	taskID := createHandlerTestTaskForAgentOnIssue(t, leaderID, issueID)
	return delegationFixture{IssueID: issueID, TaskID: taskID}
}

// postDelegationCommentAsAgent posts a comment with an agent author identity
// (X-Agent-ID + X-Task-ID) and returns the created comment response.
func postDelegationCommentAsAgent(t *testing.T, fx delegationFixture, agentID, content string) CommentResponse {
	t.Helper()
	w := httptest.NewRecorder()
	r := newRequest(http.MethodPost, "/api/issues/"+fx.IssueID+"/comments", map[string]any{"content": content})
	r = withURLParam(r, "id", fx.IssueID)
	r.Header.Set("X-Agent-ID", agentID)
	r.Header.Set("X-Task-ID", fx.TaskID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode created comment: %v", err)
	}
	return resp
}

// previewDelegationCommentAsAgent runs the trigger-preview as an agent author.
func previewDelegationCommentAsAgent(t *testing.T, fx delegationFixture, agentID, content string) CommentTriggerPreviewResponse {
	t.Helper()
	w := httptest.NewRecorder()
	r := newRequest(http.MethodPost, "/api/issues/"+fx.IssueID+"/comments/trigger-preview", map[string]any{"content": content})
	r = withURLParam(r, "id", fx.IssueID)
	r.Header.Set("X-Agent-ID", agentID)
	r.Header.Set("X-Task-ID", fx.TaskID)
	testHandler.PreviewCommentTriggers(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("PreviewCommentTriggers: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp CommentTriggerPreviewResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode preview response: %v", err)
	}
	return resp
}

// countDelegatedChildren counts the child issues a comment created via
// /delegate (delegation_comment_id = $1).
func countDelegatedChildren(t *testing.T, commentID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM issue WHERE delegation_comment_id = $1
	`, commentID).Scan(&n); err != nil {
		t.Fatalf("count delegated children: %v", err)
	}
	return n
}

// loadDelegatedChild loads the single child issue a comment created via
// /delegate. Fails when the count is not exactly one.
func loadDelegatedChild(t *testing.T, commentID string) db.Issue {
	t.Helper()
	var childID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id FROM issue WHERE delegation_comment_id = $1
	`, commentID).Scan(&childID); err != nil {
		t.Fatalf("load delegated child id: %v", err)
	}
	child, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(childID))
	if err != nil {
		t.Fatalf("load delegated child: %v", err)
	}
	return child
}

func delegationOutcomeFor(resp CommentResponse, targetID string) *CommentTriggerOutcome {
	for i := range resp.TriggerOutcomes {
		if resp.TriggerOutcomes[i].TargetID == targetID {
			return &resp.TriggerOutcomes[i]
		}
	}
	return nil
}

// createHandlerTestAgentOffline creates a workspace-visible agent with NO
// bound runtime (leader offline for the delegation gate, AC-5.3).
func createHandlerTestAgentOffline(t *testing.T, name string) string {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			visibility, permission_mode, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, 'workspace', 'public_to', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, testWorkspaceID, name, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("failed to create offline handler test agent: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id)
		VALUES ($1, 'workspace', $2)
		ON CONFLICT (agent_id, target_type, target_id) DO NOTHING
	`, agentID, testWorkspaceID); err != nil {
		t.Fatalf("failed to seed workspace invocation target: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}

// createHandlerTestAgentOfflineRuntime creates a workspace-visible agent with a
// runtime BOUND but whose status is NOT 'online' (缺陷 #2). The delegation
// offline gate must treat this exactly like a missing runtime — the dispatch
// path's AgentReadiness refuses it — so the child parks in backlog instead of
// a stuck todo with a misreported internal_error.
func createHandlerTestAgentOfflineRuntime(t *testing.T, name string) string {
	t.Helper()
	ctx := context.Background()
	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', $3, 'offline', $4, '{}'::jsonb, $5, now())
		RETURNING id
	`, testWorkspaceID, name+"-rt", "handler_test_runtime_offline", "handler test runtime offline", testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("create offline-status runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 'public_to', 1, $4, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, testWorkspaceID, name, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("failed to create offline-runtime handler test agent: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id)
		VALUES ($1, 'workspace', $2)
		ON CONFLICT (agent_id, target_type, target_id) DO NOTHING
	`, agentID, testWorkspaceID); err != nil {
		t.Fatalf("failed to seed workspace invocation target: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}

func TestIsDelegationComment(t *testing.T) {
	cases := []struct {
		content string
		want    bool
	}{
		{"/delegate 完成任务", true},
		{"  /delegate @squad", true},
		{"/DELEGATE 完成任务", true},
		{"/Delegate @squad", true},
		{"/委派 完成任务", true},
		{"/委派", true},
		{"/delegate", true},
		{"/delegates something", false},
		{"/ delegate", false},
		{"please /delegate @squad", false},
		{"/note /delegate", false},
		{"see /delegate docs", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isDelegationComment(tc.content); got != tc.want {
			t.Errorf("isDelegationComment(%q) = %v, want %v", tc.content, got, tc.want)
		}
	}
	if !delegationTokenNotFirst("please /delegate @squad") {
		t.Errorf("delegationTokenNotFirst(please /delegate @squad) = false, want true")
	}
	if delegationTokenNotFirst("/delegate 完成任务") {
		t.Errorf("delegationTokenNotFirst(/delegate ...) = true, want false")
	}
}

func TestDelegationTaskSummary(t *testing.T) {
	cases := []struct {
		content string
		want    string
	}{
		{"/delegate 完成任务X：请子小队处理", "完成任务X：请子小队处理"},
		{"/委派 [@A](mention://squad/11111111-1111-1111-1111-111111111111) 完成", "完成"},
		{"/delegate [@A](mention://squad/11111111-1111-1111-1111-111111111111)", "Delegated task"},
		{"/delegate", "Delegated task"},
	}
	for _, tc := range cases {
		if got := delegationTaskSummary(tc.content); got != tc.want {
			t.Errorf("delegationTaskSummary(%q) = %q, want %q", tc.content, got, tc.want)
		}
	}
}

// TestCreateComment_DelegateCreatesChildAndDispatches covers AC-1 / AC-2 / F4:
// the child issue is created with the full delegation contract (assignee
// squad, todo, origin agent_create + origin_id = trigger comment
// source_task_id, delegation_comment_id, stage NULL) and the squad leader is
// dispatched on the CHILD, never on the main issue.
func TestCreateComment_DelegateCreatesChildAndDispatches(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	leaderID := createHandlerTestAgent(t, "Delegate Main Leader", nil)
	squadLeaderID := createHandlerTestAgent(t, "Delegate Squad Leader", nil)
	squadID := createCommentTriggerPreviewSquad(t, "Delegate Squad One", squadLeaderID)
	fx := createDelegationIssue(t, leaderID, "delegate creates child")

	content := fmt.Sprintf("/delegate [@SquadOne](mention://squad/%s) 完成任务X：请子小队处理", squadID)
	resp := postDelegationCommentAsAgent(t, fx, leaderID, content)

	outcome := delegationOutcomeFor(resp, squadID)
	if outcome == nil {
		t.Fatalf("trigger_outcomes = %+v, want an outcome for squad %s", resp.TriggerOutcomes, squadID)
	}
	if outcome.Status != DispatchQueued || outcome.ReasonCode != ReasonQueued {
		t.Fatalf("delegation outcome = %+v, want queued/queued", outcome)
	}
	if outcome.Subissue == nil {
		t.Fatalf("delegation outcome has no subissue ref: %+v", outcome)
	}

	child := loadDelegatedChild(t, resp.ID)
	if uuidToString(child.ParentIssueID) != fx.IssueID {
		t.Fatalf("child parent = %s, want %s", uuidToString(child.ParentIssueID), fx.IssueID)
	}
	if child.AssigneeType.String != "squad" || uuidToString(child.AssigneeID) != squadID {
		t.Fatalf("child assignee = %s/%s, want squad/%s", child.AssigneeType.String, uuidToString(child.AssigneeID), squadID)
	}
	if child.Status != "todo" {
		t.Fatalf("child status = %q, want todo", child.Status)
	}
	if child.OriginType.String != "agent_create" {
		t.Fatalf("child origin_type = %q, want agent_create", child.OriginType.String)
	}
	if uuidToString(child.OriginID) != fx.TaskID {
		t.Fatalf("child origin_id = %s, want %s (trigger comment source_task_id)", uuidToString(child.OriginID), fx.TaskID)
	}
	if child.Stage.Valid {
		t.Fatalf("child stage is set (%v), want NULL (AC-4.4)", child.Stage.Int32)
	}
	parent, err := testHandler.Queries.GetIssue(ctx, parseUUID(fx.IssueID))
	if err != nil {
		t.Fatalf("load main issue: %v", err)
	}
	if child.ProjectID.Valid != parent.ProjectID.Valid || uuidToString(child.ProjectID) != uuidToString(parent.ProjectID) {
		t.Fatalf("child project = %v, want inherited from parent (%v)", child.ProjectID, parent.ProjectID)
	}
	if !child.Description.Valid || !strings.Contains(child.Description.String, "完成任务X") ||
		!strings.Contains(child.Description.String, "mention://issue/"+fx.IssueID) {
		t.Fatalf("child description missing command text or backlink: %q", child.Description.String)
	}
	if outcome.Subissue.ID != uuidToString(child.ID) || outcome.Subissue.Title != child.Title {
		t.Fatalf("subissue ref = %+v, want id=%s title=%q", outcome.Subissue, uuidToString(child.ID), child.Title)
	}

	// F4: the leader is dispatched on the CHILD...
	if got := countQueuedCommentTriggerTasks(t, uuidToString(child.ID), squadLeaderID); got != 1 {
		t.Fatalf("child queued leader tasks = %d, want 1", got)
	}
	// ...and NOT on the main issue.
	if got := countQueuedCommentTriggerTasks(t, fx.IssueID, squadLeaderID); got != 0 {
		t.Fatalf("main issue queued squad leader tasks = %d, want 0 (F4)", got)
	}
}

// TestCreateComment_DelegateMultipleSquads covers AC-2.3 / O8: one comment
// delegating two squads creates two distinguishable children, each dispatched
// to its own leader.
func TestCreateComment_DelegateMultipleSquads(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	leaderID := createHandlerTestAgent(t, "Delegate Multi Leader", nil)
	sqALeader := createHandlerTestAgent(t, "Delegate Multi A Leader", nil)
	sqBLeader := createHandlerTestAgent(t, "Delegate Multi B Leader", nil)
	squadA := createCommentTriggerPreviewSquad(t, "Delegate Multi Squad A", sqALeader)
	squadB := createCommentTriggerPreviewSquad(t, "Delegate Multi Squad B", sqBLeader)
	fx := createDelegationIssue(t, leaderID, "delegate multiple squads")

	content := fmt.Sprintf("/delegate [@A](mention://squad/%s) [@B](mention://squad/%s) 拆解任务", squadA, squadB)
	resp := postDelegationCommentAsAgent(t, fx, leaderID, content)

	if got := countDelegatedChildren(t, resp.ID); got != 2 {
		t.Fatalf("delegated children = %d, want 2", got)
	}
	var titleA, titleB string
	if err := testPool.QueryRow(context.Background(), `
		SELECT title FROM issue WHERE delegation_comment_id = $1 AND assignee_id = $2
	`, resp.ID, squadA).Scan(&titleA); err != nil {
		t.Fatalf("load child A: %v", err)
	}
	if err := testPool.QueryRow(context.Background(), `
		SELECT title FROM issue WHERE delegation_comment_id = $1 AND assignee_id = $2
	`, resp.ID, squadB).Scan(&titleB); err != nil {
		t.Fatalf("load child B: %v", err)
	}
	if titleA == titleB {
		t.Fatalf("children titles not distinguishable: both %q (O8)", titleA)
	}
	if !strings.Contains(titleA, "Delegate Multi Squad A") || !strings.Contains(titleB, "Delegate Multi Squad B") {
		t.Fatalf("titles missing squad name: %q / %q", titleA, titleB)
	}
	var childA, childB string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id FROM issue WHERE delegation_comment_id = $1 AND assignee_id = $2
	`, resp.ID, squadA).Scan(&childA); err != nil {
		t.Fatalf("load child A id: %v", err)
	}
	if err := testPool.QueryRow(context.Background(), `
		SELECT id FROM issue WHERE delegation_comment_id = $1 AND assignee_id = $2
	`, resp.ID, squadB).Scan(&childB); err != nil {
		t.Fatalf("load child B id: %v", err)
	}
	if got := countQueuedCommentTriggerTasks(t, childA, sqALeader); got != 1 {
		t.Fatalf("child A queued leader tasks = %d, want 1", got)
	}
	if got := countQueuedCommentTriggerTasks(t, childB, sqBLeader); got != 1 {
		t.Fatalf("child B queued leader tasks = %d, want 1", got)
	}
}

// TestCreateComment_DelegateNoSquadMentionInvalid covers AC-5.1: /delegate
// with no squad mention is blocked invalid_command; the comment is saved and
// no child is created.
func TestCreateComment_DelegateNoSquadMentionInvalid(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	leaderID := createHandlerTestAgent(t, "Delegate NoSquad Leader", nil)
	fx := createDelegationIssue(t, leaderID, "delegate no squad")

	resp := postDelegationCommentAsAgent(t, fx, leaderID, "/delegate 完成一份任务")
	outcome := delegationOutcomeFor(resp, "")
	if outcome == nil || outcome.Status != DispatchBlocked || outcome.ReasonCode != ReasonInvalidCommand {
		t.Fatalf("outcomes = %+v, want blocked invalid_command", resp.TriggerOutcomes)
	}
	if got := countDelegatedChildren(t, resp.ID); got != 0 {
		t.Fatalf("delegated children = %d, want 0", got)
	}
}

// TestCreateComment_DelegateNonFirstTokenInvalid covers AC-5.1: a misplaced
// /delegate token is invalid_command; the mention still triggers normally
// (AC-7.1).
func TestCreateComment_DelegateNonFirstTokenInvalid(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	leaderID := createHandlerTestAgent(t, "Delegate NonFirst Leader", nil)
	squadLeaderID := createHandlerTestAgent(t, "Delegate NonFirst Squad Leader", nil)
	squadID := createCommentTriggerPreviewSquad(t, "Delegate NonFirst Squad", squadLeaderID)
	fx := createDelegationIssue(t, leaderID, "delegate non-first token")

	content := fmt.Sprintf("please /delegate [@Squad](mention://squad/%s) 完成任务", squadID)
	resp := postDelegationCommentAsAgent(t, fx, leaderID, content)

	if got := countDelegatedChildren(t, resp.ID); got != 0 {
		t.Fatalf("delegated children = %d, want 0", got)
	}
	if got := countQueuedCommentTriggerTasks(t, fx.IssueID, squadLeaderID); got != 1 {
		t.Fatalf("main issue queued squad leader tasks = %d, want 1", got)
	}
	hasInvalid := false
	for _, o := range resp.TriggerOutcomes {
		if o.Status == DispatchBlocked && o.ReasonCode == ReasonInvalidCommand {
			hasInvalid = true
		}
	}
	if !hasInvalid {
		t.Fatalf("outcomes = %+v, want a blocked invalid_command", resp.TriggerOutcomes)
	}
}

// TestCreateComment_DelegatePrivateSquadGateNoCreate covers AC-5.2: delegating
// to a squad whose leader the author cannot invoke is blocked
// invocation_not_allowed and creates nothing.
func TestCreateComment_DelegatePrivateSquadGateNoCreate(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	leaderID := createHandlerTestAgent(t, "Delegate PrivateGate Leader", nil)
	privateLeaderID, _, _ := privateAgentTestFixture(t)
	squadID := createCommentTriggerPreviewSquad(t, "Delegate Private Squad", privateLeaderID)
	fx := createDelegationIssue(t, leaderID, "delegate private squad")

	content := fmt.Sprintf("/delegate [@Private](mention://squad/%s) 完成任务", squadID)
	resp := postDelegationCommentAsAgent(t, fx, leaderID, content)

	outcome := delegationOutcomeFor(resp, squadID)
	if outcome == nil || outcome.Status != DispatchBlocked || outcome.ReasonCode != ReasonInvocationNotAllowed {
		t.Fatalf("outcomes = %+v, want blocked invocation_not_allowed for %s", resp.TriggerOutcomes, squadID)
	}
	if got := countDelegatedChildren(t, resp.ID); got != 0 {
		t.Fatalf("delegated children = %d, want 0 (gate failure must not create)", got)
	}
}

// TestCreateComment_DelegateOfflineLeaderParksBacklog covers AC-5.3 / O3.2: an
// offline leader is blocked runtime_offline and the child is created as
// backlog (no auto-enqueue), ready for the backlog→todo retry path.
func TestCreateComment_DelegateOfflineLeaderParksBacklog(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	leaderID := createHandlerTestAgent(t, "Delegate Offline Main Leader", nil)
	offlineLeaderID := createHandlerTestAgentOffline(t, "Delegate Offline Squad Leader")
	squadID := createCommentTriggerPreviewSquad(t, "Delegate Offline Squad", offlineLeaderID)
	fx := createDelegationIssue(t, leaderID, "delegate offline leader")

	content := fmt.Sprintf("/delegate [@Offline](mention://squad/%s) 完成任务", squadID)
	resp := postDelegationCommentAsAgent(t, fx, leaderID, content)

	outcome := delegationOutcomeFor(resp, squadID)
	if outcome == nil || outcome.Status != DispatchBlocked || outcome.ReasonCode != ReasonRuntimeOffline {
		t.Fatalf("outcomes = %+v, want blocked runtime_offline", resp.TriggerOutcomes)
	}
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM issue WHERE delegation_comment_id = $1`, resp.ID).Scan(&status); err != nil {
		t.Fatalf("load backlog child: %v", err)
	}
	if status != "backlog" {
		t.Fatalf("offline child status = %q, want backlog", status)
	}
}

// TestCreateComment_DelegateMissingSquad covers AC-5.3: a squad that does not
// exist is blocked target_unavailable without creating anything.
func TestCreateComment_DelegateMissingSquad(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	leaderID := createHandlerTestAgent(t, "Delegate Missing Leader", nil)
	fx := createDelegationIssue(t, leaderID, "delegate missing squad")
	ghostSquad := "00000000-0000-0000-0000-0000000000ab"

	content := fmt.Sprintf("/delegate [@Ghost](mention://squad/%s) 完成任务", ghostSquad)
	resp := postDelegationCommentAsAgent(t, fx, leaderID, content)

	outcome := delegationOutcomeFor(resp, ghostSquad)
	if outcome == nil || outcome.Status != DispatchBlocked || outcome.ReasonCode != ReasonTargetUnavailable {
		t.Fatalf("outcomes = %+v, want blocked target_unavailable", resp.TriggerOutcomes)
	}
	if got := countDelegatedChildren(t, resp.ID); got != 0 {
		t.Fatalf("delegated children = %d, want 0", got)
	}
}

// TestCreateComment_DelegateArchivedLeader covers AC-5.3: an archived leader is
// blocked target_unavailable without creating anything.
func TestCreateComment_DelegateArchivedLeader(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	leaderID := createHandlerTestAgent(t, "Delegate Archived Main Leader", nil)
	archivedLeaderID := createHandlerTestAgent(t, "Delegate Archived Squad Leader", nil)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET archived_at = now() WHERE id = $1`, archivedLeaderID); err != nil {
		t.Fatalf("archive leader: %v", err)
	}
	squadID := createCommentTriggerPreviewSquad(t, "Delegate Archived Squad", archivedLeaderID)
	fx := createDelegationIssue(t, leaderID, "delegate archived leader")

	content := fmt.Sprintf("/delegate [@Archived](mention://squad/%s) 完成任务", squadID)
	resp := postDelegationCommentAsAgent(t, fx, leaderID, content)

	outcome := delegationOutcomeFor(resp, squadID)
	if outcome == nil || outcome.Status != DispatchBlocked || outcome.ReasonCode != ReasonTargetUnavailable {
		t.Fatalf("outcomes = %+v, want blocked target_unavailable", resp.TriggerOutcomes)
	}
	if got := countDelegatedChildren(t, resp.ID); got != 0 {
		t.Fatalf("delegated children = %d, want 0", got)
	}
}

// TestCreateComment_DelegateSelfSquadNormal covers E1: delegating to the main
// issue's own assignee squad is treated as a normal @squad — no child is
// created and nothing extra is forbidden.
func TestCreateComment_DelegateSelfSquadNormal(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	squadLeaderID := createHandlerTestAgent(t, "Delegate Self Squad Leader", nil)
	squadID := createCommentTriggerPreviewSquad(t, "Delegate Self Squad", squadLeaderID)
	issueID := createCommentTriggerPreviewIssue(t, "delegate self squad", "squad", squadID)
	taskID := createHandlerTestTaskForAgentOnIssue(t, squadLeaderID, issueID)
	fx := delegationFixture{IssueID: issueID, TaskID: taskID}

	content := fmt.Sprintf("/delegate [@Self](mention://squad/%s) 完成任务", squadID)
	resp := postDelegationCommentAsAgent(t, fx, squadLeaderID, content)

	if got := countDelegatedChildren(t, resp.ID); got != 0 {
		t.Fatalf("delegated children = %d, want 0 (self-squad, E1)", got)
	}
	for _, o := range resp.TriggerOutcomes {
		if o.Status == DispatchBlocked && o.ReasonCode == ReasonInvalidCommand {
			t.Fatalf("self-squad /delegate reported invalid_command: %+v", resp.TriggerOutcomes)
		}
	}
}

// TestCreateComment_DelegateAgentMentionStillTriggers covers E6: an @agent
// mention in a /delegate comment keeps its normal trigger on the main issue,
// while the squad mention is delegated.
func TestCreateComment_DelegateAgentMentionStillTriggers(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	leaderID := createHandlerTestAgent(t, "Delegate Mixed Leader", nil)
	squadLeaderID := createHandlerTestAgent(t, "Delegate Mixed Squad Leader", nil)
	otherAgentID := createHandlerTestAgent(t, "Delegate Mixed Other Agent", nil)
	squadID := createCommentTriggerPreviewSquad(t, "Delegate Mixed Squad", squadLeaderID)
	fx := createDelegationIssue(t, leaderID, "delegate mixed mentions")

	content := fmt.Sprintf("/delegate [@Squad](mention://squad/%s) [@Other](mention://agent/%s) 完成任务", squadID, otherAgentID)
	resp := postDelegationCommentAsAgent(t, fx, leaderID, content)

	if got := countDelegatedChildren(t, resp.ID); got != 1 {
		t.Fatalf("delegated children = %d, want 1", got)
	}
	// The @agent mention triggered normally on the main issue (E6).
	if got := countQueuedCommentTriggerTasks(t, fx.IssueID, otherAgentID); got != 1 {
		t.Fatalf("main issue queued other-agent tasks = %d, want 1 (E6)", got)
	}
	// The delegated squad leader is NOT triggered on the main issue (F4).
	if got := countQueuedCommentTriggerTasks(t, fx.IssueID, squadLeaderID); got != 0 {
		t.Fatalf("main issue queued squad leader tasks = %d, want 0 (F4)", got)
	}
}

// TestCreateComment_DelegateDoneIssueBlocked covers E2: /delegate on a
// done/cancelled main issue is blocked target_unavailable and creates nothing.
func TestCreateComment_DelegateDoneIssueBlocked(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	leaderID := createHandlerTestAgent(t, "Delegate Done Leader", nil)
	squadLeaderID := createHandlerTestAgent(t, "Delegate Done Squad Leader", nil)
	squadID := createCommentTriggerPreviewSquad(t, "Delegate Done Squad", squadLeaderID)
	fx := createDelegationIssue(t, leaderID, "delegate done issue")
	if _, err := testPool.Exec(ctx, `UPDATE issue SET status = 'done' WHERE id = $1`, fx.IssueID); err != nil {
		t.Fatalf("mark issue done: %v", err)
	}

	content := fmt.Sprintf("/delegate [@Squad](mention://squad/%s) 完成任务", squadID)
	resp := postDelegationCommentAsAgent(t, fx, leaderID, content)

	outcome := delegationOutcomeFor(resp, "")
	if outcome == nil || outcome.Status != DispatchBlocked || outcome.ReasonCode != ReasonTargetUnavailable {
		t.Fatalf("outcomes = %+v, want blocked target_unavailable (E2)", resp.TriggerOutcomes)
	}
	if got := countDelegatedChildren(t, resp.ID); got != 0 {
		t.Fatalf("delegated children = %d, want 0", got)
	}
}

// TestCreateComment_DelegateNoAssigneeBlocked covers E2a: /delegate on a main
// issue without a leader assignee is blocked target_unavailable.
func TestCreateComment_DelegateNoAssigneeBlocked(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	leaderID := createHandlerTestAgent(t, "Delegate NoAssignee Leader", nil)
	squadLeaderID := createHandlerTestAgent(t, "Delegate NoAssignee Squad Leader", nil)
	squadID := createCommentTriggerPreviewSquad(t, "Delegate NoAssignee Squad", squadLeaderID)
	issueID := createCommentTriggerPreviewIssue(t, "delegate no assignee", "", "")
	taskID := createHandlerTestTaskForAgentOnIssue(t, leaderID, issueID)
	fx := delegationFixture{IssueID: issueID, TaskID: taskID}

	content := fmt.Sprintf("/delegate [@Squad](mention://squad/%s) 完成任务", squadID)
	resp := postDelegationCommentAsAgent(t, fx, leaderID, content)

	outcome := delegationOutcomeFor(resp, "")
	if outcome == nil || outcome.Status != DispatchBlocked || outcome.ReasonCode != ReasonTargetUnavailable {
		t.Fatalf("outcomes = %+v, want blocked target_unavailable (E2a)", resp.TriggerOutcomes)
	}
	if got := countDelegatedChildren(t, resp.ID); got != 0 {
		t.Fatalf("delegated children = %d, want 0", got)
	}
}

// TestCreateComment_DelegateAuthorNotLeaderInvalid covers 1.3: only the main
// issue's leader may delegate; another agent gets invalid_command.
func TestCreateComment_DelegateAuthorNotLeaderInvalid(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	assigneeID := createHandlerTestAgent(t, "Delegate Real Assignee", nil)
	otherAgentID := createHandlerTestAgent(t, "Delegate Not Leader", nil)
	squadLeaderID := createHandlerTestAgent(t, "Delegate NotLeader Squad Leader", nil)
	squadID := createCommentTriggerPreviewSquad(t, "Delegate NotLeader Squad", squadLeaderID)
	issueID := createCommentTriggerPreviewIssue(t, "delegate author not leader", "agent", assigneeID)
	taskID := createHandlerTestTaskForAgentOnIssue(t, otherAgentID, issueID)
	fx := delegationFixture{IssueID: issueID, TaskID: taskID}

	content := fmt.Sprintf("/delegate [@Squad](mention://squad/%s) 完成任务", squadID)
	resp := postDelegationCommentAsAgent(t, fx, otherAgentID, content)

	outcome := delegationOutcomeFor(resp, "")
	if outcome == nil || outcome.Status != DispatchBlocked || outcome.ReasonCode != ReasonInvalidCommand {
		t.Fatalf("outcomes = %+v, want blocked invalid_command", resp.TriggerOutcomes)
	}
	if got := countDelegatedChildren(t, resp.ID); got != 0 {
		t.Fatalf("delegated children = %d, want 0", got)
	}
}

// TestCreateComment_DelegateMemberAuthorInvalid covers 1.3: member authors are
// not open for v1 delegation.
func TestCreateComment_DelegateMemberAuthorInvalid(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	assigneeID := createHandlerTestAgent(t, "Delegate Member Assignee", nil)
	squadLeaderID := createHandlerTestAgent(t, "Delegate Member Squad Leader", nil)
	squadID := createCommentTriggerPreviewSquad(t, "Delegate Member Squad", squadLeaderID)
	issueID := createCommentTriggerPreviewIssue(t, "delegate member author", "agent", assigneeID)

	content := fmt.Sprintf("/delegate [@Squad](mention://squad/%s) 完成任务", squadID)
	w := httptest.NewRecorder()
	r := newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": content})
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode created comment: %v", err)
	}
	outcome := delegationOutcomeFor(resp, "")
	if outcome == nil || outcome.Status != DispatchBlocked || outcome.ReasonCode != ReasonInvalidCommand {
		t.Fatalf("outcomes = %+v, want blocked invalid_command", resp.TriggerOutcomes)
	}
	if got := countDelegatedChildren(t, resp.ID); got != 0 {
		t.Fatalf("delegated children = %d, want 0", got)
	}
}

// TestPreviewCommentTriggers_Delegate covers AC-3.1 / §7.3: the preview lists
// gate-passing squads in `delegations`, never the delegated leader in
// `agents`; gate failures land in `blocked`.
func TestPreviewCommentTriggers_Delegate(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	leaderID := createHandlerTestAgent(t, "Delegate Preview Leader", nil)
	squadLeaderID := createHandlerTestAgent(t, "Delegate Preview Squad Leader", nil)
	privateLeaderID, _, _ := privateAgentTestFixture(t)
	squadID := createCommentTriggerPreviewSquad(t, "Delegate Preview Squad", squadLeaderID)
	privateSquadID := createCommentTriggerPreviewSquad(t, "Delegate Preview Private Squad", privateLeaderID)
	fx := createDelegationIssue(t, leaderID, "delegate preview")

	content := fmt.Sprintf("/delegate [@A](mention://squad/%s) [@Priv](mention://squad/%s) 完成任务", squadID, privateSquadID)
	preview := previewDelegationCommentAsAgent(t, fx, leaderID, content)

	if len(preview.Agents) != 0 {
		t.Fatalf("preview agents = %+v, want none (delegated leaders not triggered on main issue)", preview.Agents)
	}
	if len(preview.Delegations) != 1 || preview.Delegations[0].TargetID != squadID {
		t.Fatalf("preview delegations = %+v, want [squad %s]", preview.Delegations, squadID)
	}
	foundPrivateBlocked := false
	for _, b := range preview.Blocked {
		if b.TargetID == privateSquadID && b.ReasonCode == ReasonInvocationNotAllowed {
			foundPrivateBlocked = true
		}
	}
	if !foundPrivateBlocked {
		t.Fatalf("preview blocked = %+v, want invocation_not_allowed for %s", preview.Blocked, privateSquadID)
	}
}

// TestPreviewCommentTriggers_DelegateInvalid covers AC-5.1 preview: /delegate
// with no squad mention surfaces invalid_command in blocked.
func TestPreviewCommentTriggers_DelegateInvalid(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	leaderID := createHandlerTestAgent(t, "Delegate Preview Invalid Leader", nil)
	fx := createDelegationIssue(t, leaderID, "delegate preview invalid")

	preview := previewDelegationCommentAsAgent(t, fx, leaderID, "/delegate 完成一份任务")
	found := false
	for _, b := range preview.Blocked {
		if b.ReasonCode == ReasonInvalidCommand {
			found = true
		}
	}
	if !found {
		t.Fatalf("preview blocked = %+v, want invalid_command", preview.Blocked)
	}
}

// TestCreateComment_DelegateRedundantUniqueLeaderMention covers AC-2.2/F4
// with the SR3 unique-leader upgrade: when the comment ALSO @mentions the
// delegated squad's unique leader as an agent, the mention must NOT produce a
// squad-leader task on the main issue (the delegation already dispatches that
// leader on the child); only the child dispatch happens.
func TestCreateComment_DelegateRedundantUniqueLeaderMention(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	leaderID := createHandlerTestAgent(t, "Delegate Redundant Leader", nil)
	squadLeaderID := createHandlerTestAgent(t, "Delegate Redundant Squad Leader", nil)
	squadID := createCommentTriggerPreviewSquad(t, "Delegate Redundant Squad", squadLeaderID)
	fx := createDelegationIssue(t, leaderID, "delegate redundant unique leader mention")

	content := fmt.Sprintf("/delegate [@Squad](mention://squad/%s) [@Leader](mention://agent/%s) 完成任务", squadID, squadLeaderID)
	resp := postDelegationCommentAsAgent(t, fx, leaderID, content)

	if got := countDelegatedChildren(t, resp.ID); got != 1 {
		t.Fatalf("delegated children = %d, want 1", got)
	}
	// The leader is dispatched on the CHILD only (F4 / AC-2.2): no task on the
	// main issue, even though the SR3 upgrade would have fired the squad-leader
	// trigger for the redundant @agent mention.
	if got := countQueuedCommentTriggerTasks(t, fx.IssueID, squadLeaderID); got != 0 {
		t.Fatalf("main issue queued squad leader tasks = %d, want 0 (AC-2.2)", got)
	}
}

// TestPreviewCommentTriggers_DelegateOfflineLeaderBlocked covers 缺陷 #1:
// an offline leader is gate-refused in the preview too — the squad lands in
// blocked[] with runtime_offline (matching the create-path outcome), never in
// delegations as a queued "will create child issue" promise.
func TestPreviewCommentTriggers_DelegateOfflineLeaderBlocked(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	leaderID := createHandlerTestAgent(t, "Delegate Preview Offline Leader", nil)
	onlineLeaderID := createHandlerTestAgent(t, "Delegate Preview Offline Squad Leader", nil)
	offlineLeaderID := createHandlerTestAgentOffline(t, "Delegate Preview Offline Squad Leader 2")
	onlineSquadID := createCommentTriggerPreviewSquad(t, "Delegate Preview Offline Online Squad", onlineLeaderID)
	offlineSquadID := createCommentTriggerPreviewSquad(t, "Delegate Preview Offline Offline Squad", offlineLeaderID)
	fx := createDelegationIssue(t, leaderID, "delegate preview offline")

	content := fmt.Sprintf("/delegate [@Online](mention://squad/%s) [@Offline](mention://squad/%s) 完成任务", onlineSquadID, offlineSquadID)
	preview := previewDelegationCommentAsAgent(t, fx, leaderID, content)

	// The online squad is delegated as before.
	if len(preview.Delegations) != 1 || preview.Delegations[0].TargetID != onlineSquadID {
		t.Fatalf("preview delegations = %+v, want [squad %s]", preview.Delegations, onlineSquadID)
	}
	// The offline squad is reported blocked runtime_offline, matching the
	// create outcome (blocked + backlog child).
	foundOfflineBlocked := false
	for _, b := range preview.Blocked {
		if b.TargetID == offlineSquadID && b.Status == DispatchBlocked && b.ReasonCode == ReasonRuntimeOffline {
			foundOfflineBlocked = true
		}
	}
	if !foundOfflineBlocked {
		t.Fatalf("preview blocked = %+v, want blocked runtime_offline for %s (缺陷 #1)", preview.Blocked, offlineSquadID)
	}
	for _, d := range preview.Delegations {
		if d.TargetID == offlineSquadID {
			t.Fatalf("offline squad must not appear in delegations: %+v", preview.Delegations)
		}
	}
}

// TestCreateComment_DelegateBoundOfflineLeaderParksBacklog covers 缺陷 #2: a
// leader with a runtime BOUND but not 'online' is offline for the delegation
// gate too (AgentReadiness is the shared dispatch truth) — the child parks in
// backlog with outcome blocked runtime_offline, never a stuck todo with a
// misreported internal_error.
func TestCreateComment_DelegateBoundOfflineLeaderParksBacklog(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	leaderID := createHandlerTestAgent(t, "Delegate BoundOffline Main Leader", nil)
	boundOfflineLeaderID := createHandlerTestAgentOfflineRuntime(t, "Delegate BoundOffline Squad Leader")
	squadID := createCommentTriggerPreviewSquad(t, "Delegate BoundOffline Squad", boundOfflineLeaderID)
	fx := createDelegationIssue(t, leaderID, "delegate bound-offline leader")

	content := fmt.Sprintf("/delegate [@BoundOffline](mention://squad/%s) 完成任务", squadID)
	resp := postDelegationCommentAsAgent(t, fx, leaderID, content)

	outcome := delegationOutcomeFor(resp, squadID)
	if outcome == nil || outcome.Status != DispatchBlocked || outcome.ReasonCode != ReasonRuntimeOffline {
		t.Fatalf("outcomes = %+v, want blocked runtime_offline (缺陷 #2)", resp.TriggerOutcomes)
	}
	var childID, status string
	if err := testPool.QueryRow(ctx, `
		SELECT id, status FROM issue WHERE delegation_comment_id = $1
	`, resp.ID).Scan(&childID, &status); err != nil {
		t.Fatalf("load backlog child: %v", err)
	}
	if status != "backlog" {
		t.Fatalf("bound-offline child status = %q, want backlog (缺陷 #2)", status)
	}
	// The dispatch correctly refused, so no pending task exists on the child —
	// and no internal_error was reported (AC-5.5 verification never ran).
	if got := countQueuedCommentTriggerTasks(t, childID, boundOfflineLeaderID); got != 0 {
		t.Fatalf("child queued leader tasks = %d, want 0", got)
	}
}

// TestPreviewCommentTriggers_DelegateBoundOfflineLeaderBlocked covers 缺陷 #2
// on the preview path: a bound-but-offline leader is gate-refused there too —
// blocked[] runtime_offline, never a queued delegations promise.
func TestPreviewCommentTriggers_DelegateBoundOfflineLeaderBlocked(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	leaderID := createHandlerTestAgent(t, "Delegate Preview BoundOffline Leader", nil)
	boundOfflineLeaderID := createHandlerTestAgentOfflineRuntime(t, "Delegate Preview BoundOffline Squad Leader")
	squadID := createCommentTriggerPreviewSquad(t, "Delegate Preview BoundOffline Squad", boundOfflineLeaderID)
	fx := createDelegationIssue(t, leaderID, "delegate preview bound-offline")

	content := fmt.Sprintf("/delegate [@BoundOffline](mention://squad/%s) 完成任务", squadID)
	preview := previewDelegationCommentAsAgent(t, fx, leaderID, content)

	if len(preview.Delegations) != 0 {
		t.Fatalf("preview delegations = %+v, want none (bound-offline leader)", preview.Delegations)
	}
	foundBlocked := false
	for _, b := range preview.Blocked {
		if b.TargetID == squadID && b.Status == DispatchBlocked && b.ReasonCode == ReasonRuntimeOffline {
			foundBlocked = true
		}
	}
	if !foundBlocked {
		t.Fatalf("preview blocked = %+v, want blocked runtime_offline for %s (缺陷 #2)", preview.Blocked, squadID)
	}
}

// TestUpdateComment_DelegateStructuralEditBlocked covers AC-6.1: editing a
// delegation comment's squad set or removing the /delegate prefix returns 409
// and changes nothing.
func TestUpdateComment_DelegateStructuralEditBlocked(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	leaderID := createHandlerTestAgent(t, "Delegate Edit Leader", nil)
	squadLeaderID := createHandlerTestAgent(t, "Delegate Edit Squad Leader", nil)
	squadID := createCommentTriggerPreviewSquad(t, "Delegate Edit Squad", squadLeaderID)
	fx := createDelegationIssue(t, leaderID, "delegate structural edit")

	content := fmt.Sprintf("/delegate [@Squad](mention://squad/%s) 完成任务", squadID)
	resp := postDelegationCommentAsAgent(t, fx, leaderID, content)
	if got := countDelegatedChildren(t, resp.ID); got != 1 {
		t.Fatalf("delegated children = %d, want 1", got)
	}

	editExpectConflict := func(t *testing.T, newContent string) {
		t.Helper()
		w := httptest.NewRecorder()
		r := newRequest(http.MethodPut, "/api/comments/"+resp.ID, map[string]any{"content": newContent})
		r = withURLParam(r, "commentId", resp.ID)
		testHandler.UpdateComment(w, r)
		if w.Code != http.StatusConflict {
			t.Fatalf("UpdateComment: expected 409, got %d: %s", w.Code, w.Body.String())
		}
	}
	t.Run("remove prefix", func(t *testing.T) {
		editExpectConflict(t, "完成任务")
	})
	t.Run("change squad set", func(t *testing.T) {
		otherSquadID := createCommentTriggerPreviewSquad(t, "Delegate Edit Other Squad", squadLeaderID)
		editExpectConflict(t, fmt.Sprintf("/delegate [@Other](mention://squad/%s) 完成任务", otherSquadID))
	})
	if got := countDelegatedChildren(t, resp.ID); got != 1 {
		t.Fatalf("delegated children after rejected edits = %d, want 1", got)
	}
}

// TestUpdateComment_DelegateDescriptionEditIdempotent covers AC-6.2/6.3: a
// description-class edit by the delegating agent is allowed and does not
// re-create the child ((delegation_comment_id, assignee_id) idempotency).
func TestUpdateComment_DelegateDescriptionEditIdempotent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	leaderID := createHandlerTestAgent(t, "Delegate EditDesc Leader", nil)
	squadLeaderID := createHandlerTestAgent(t, "Delegate EditDesc Squad Leader", nil)
	squadID := createCommentTriggerPreviewSquad(t, "Delegate EditDesc Squad", squadLeaderID)
	fx := createDelegationIssue(t, leaderID, "delegate description edit")

	content := fmt.Sprintf("/delegate [@Squad](mention://squad/%s) 完成任务", squadID)
	resp := postDelegationCommentAsAgent(t, fx, leaderID, content)
	if got := countDelegatedChildren(t, resp.ID); got != 1 {
		t.Fatalf("delegated children = %d, want 1", got)
	}

	edited := fmt.Sprintf("/delegate [@Squad](mention://squad/%s) 完成任务（补充细节）", squadID)
	w := httptest.NewRecorder()
	r := newRequest(http.MethodPut, "/api/comments/"+resp.ID, map[string]any{"content": edited})
	r = withURLParam(r, "commentId", resp.ID)
	r.Header.Set("X-Agent-ID", leaderID)
	r.Header.Set("X-Task-ID", fx.TaskID)
	testHandler.UpdateComment(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateComment: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := countDelegatedChildren(t, resp.ID); got != 1 {
		t.Fatalf("delegated children after description edit = %d, want 1 (AC-6.3)", got)
	}
}

// TestDelegationEditIsStructural covers the structural-vs-description edit
// classifier (O5).
func TestDelegationEditIsStructural(t *testing.T) {
	squadA := "11111111-1111-1111-1111-111111111111"
	squadB := "22222222-2222-2222-2222-222222222222"
	delA := fmt.Sprintf("/delegate [@A](mention://squad/%s) 任务", squadA)
	cases := []struct {
		name string
		oldC string
		newC string
		want bool
	}{
		{"description change", delA, fmt.Sprintf("/delegate [@A](mention://squad/%s) 任务（补充）", squadA), false},
		{"remove prefix", delA, "任务", true},
		{"add squad", delA, fmt.Sprintf("/delegate [@A](mention://squad/%s) [@B](mention://squad/%s) 任务", squadA, squadB), true},
		{"swap squad", delA, fmt.Sprintf("/delegate [@B](mention://squad/%s) 任务", squadB), true},
		{"drop squad", fmt.Sprintf("/delegate [@A](mention://squad/%s) [@B](mention://squad/%s) 任务", squadA, squadB), delA, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := delegationEditIsStructural(tc.oldC, tc.newC); got != tc.want {
				t.Fatalf("delegationEditIsStructural = %v, want %v", got, tc.want)
			}
		})
	}
}
