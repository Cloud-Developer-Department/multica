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

// ---------------------------------------------------------------------------
// Engineering analytics platform endpoint tests (CLO-239).
//
// Contract: API-CLO-228 v2.0. Covers:
//   * Tab1 A1-A5: activity summary / heatmap / top-members / adoption summary /
//     adoption trend (math + window).
//   * Tab2 B1-B6: funnel (merged=null without VCS), performance, top, skills,
//     collaboration summary + blockers.
//   * Tab3 G1-G4: guide states (source_status.ready=false) when the Git source
//     is unconnected, and 200 responses never 4xx/5xx for an unconnected source.
//   * Tab4 D1-D2: guide states for unconnected deployment pipeline.
//   * L1-L2: guide states for unconnected identity source; department_id → 400
//     when department data is not configured (E18).
//   * Auth: every endpoint rejects a missing session identity with 401.
//
// Seeded rows carry a distinctive marker ("clotest-") in titles so they can be
// counted and cleaned up without colliding with other tests in the shared
// fixture workspace.
// ---------------------------------------------------------------------------

// analyticsTestAgent returns the fixture workspace's first agent + runtime
// (mirrors personalTestRuntimeAgent).
func analyticsTestAgent(t *testing.T, ctx context.Context) (runtimeID, agentID string) {
	t.Helper()
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 AND archived_at IS NULL LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}
	return
}

// seedAnalyticsIssue inserts a test issue (creator = testUserID) and cleans up.
func seedAnalyticsIssue(t *testing.T, ctx context.Context, title string, assigneeType, assigneeID string, createdAt time.Time) string {
	t.Helper()
	var id string
	err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id, assignee_type, assignee_id, created_at, updated_at, number)
		VALUES ($1, $2, 'member', $3, NULLIF($4, '')::text, NULLIF($5, '')::uuid, $6, $6,
		        COALESCE((SELECT MAX(number) FROM issue WHERE workspace_id = $1), 0) + 1)
		RETURNING id
	`, testWorkspaceID, title, testUserID, assigneeType, assigneeID, createdAt).Scan(&id)
	if err != nil {
		t.Fatalf("insert analytics issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, id) })
	return id
}

// seedAnalyticsComment attaches a member/agent comment to an issue.
func seedAnalyticsComment(t *testing.T, ctx context.Context, issueID string, authorType, authorID string, createdAt time.Time) {
	t.Helper()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content, type, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'analytics-test-comment', 'comment', $5, $5)
	`, testWorkspaceID, issueID, authorType, authorID, createdAt); err != nil {
		t.Fatalf("insert analytics comment: %v", err)
	}
}

// seedAnalyticsTask inserts an agent_task_queue row (runtime required). issueID
// links the task to a workspace issue so funnel/execute joins resolve.
func seedAnalyticsTask(t *testing.T, ctx context.Context, runtimeID, agentID, issueID, status string, startedAt, completedAt *time.Time, failureReason string, createdAt time.Time) {
	t.Helper()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, started_at, completed_at, failure_reason, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, agentID, runtimeID, issueID, status, startedAt, completedAt, failureReason, createdAt); err != nil {
		t.Fatalf("insert analytics task: %v", err)
	}
}

// seedAnalyticsSkill creates a skill and binds it to the fixture agent.
func seedAnalyticsSkill(t *testing.T, ctx context.Context, agentID, name string) string {
	t.Helper()
	var skillID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO skill (workspace_id, name, description, content, created_by)
		VALUES ($1, $2, '', 'content', $3)
		RETURNING id
	`, testWorkspaceID, name, testUserID).Scan(&skillID); err != nil {
		t.Fatalf("insert analytics skill: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO agent_skill (agent_id, skill_id) VALUES ($1, $2)`, agentID, skillID); err != nil {
		t.Fatalf("bind analytics skill: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_skill WHERE skill_id = $1`, skillID)
		testPool.Exec(ctx, `DELETE FROM skill WHERE id = $1`, skillID)
	})
	return skillID
}

// assertSourceGuide decodes a payload with a top-level source_status and
// asserts ready=false + a human reason (guide state, §0.7).
func assertSourceGuide(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("guide state: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		SourceStatus struct {
			Ready  bool    `json:"ready"`
			Reason *string `json:"reason"`
		} `json:"source_status"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode source_status: %v", err)
	}
	if body.SourceStatus.Ready {
		t.Errorf("guide state: expected ready=false, got true: %s", w.Body.String())
	}
	if body.SourceStatus.Reason == nil || *body.SourceStatus.Reason == "" {
		t.Errorf("guide state: expected a human reason, got %v", body.SourceStatus.Reason)
	}
}

// ---------------------------------------------------------------------------
// Auth (all endpoints reject missing identity with 401)
// ---------------------------------------------------------------------------

func TestAnalyticsUnauthorized(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	routes := map[string]http.HandlerFunc{
		"/activity/summary":           testHandler.GetAnalyticsActivitySummary,
		"/activity/heatmap":           testHandler.GetAnalyticsActivityHeatmap,
		"/activity/top-members":       testHandler.GetAnalyticsActivityTopMembers,
		"/adoption/summary":           testHandler.GetAnalyticsAdoptionSummary,
		"/adoption/trend":             testHandler.GetAnalyticsAdoptionTrend,
		"/agents/funnel":              testHandler.GetAnalyticsAgentsFunnel,
		"/agents/performance":         testHandler.GetAnalyticsAgentsPerformance,
		"/agents/top":                 testHandler.GetAnalyticsAgentsTop,
		"/skills/overview":            testHandler.GetAnalyticsSkillsOverview,
		"/collaboration/summary":      testHandler.GetAnalyticsCollaborationSummary,
		"/collaboration/blockers":     testHandler.GetAnalyticsCollaborationBlockers,
		"/git/eloc":                   testHandler.GetAnalyticsGitEloc,
		"/git/quality":                testHandler.GetAnalyticsGitQuality,
		"/git/repos":                  testHandler.GetAnalyticsGitRepos,
		"/git/prs?repo=x/y":           testHandler.GetAnalyticsGitPRs,
		"/dora/lead-time":             testHandler.GetAnalyticsDoraLeadTime,
		"/dora/deployments":           testHandler.GetAnalyticsDoraDeployments,
		"/identity/lifecycle":         testHandler.GetAnalyticsIdentityLifecycle,
		"/identity/departments":       testHandler.GetAnalyticsIdentityDepartments,
	}
	for path, hfn := range routes {
		req := httptest.NewRequest("GET", "/api/analytics"+path, nil)
		req.Header.Set("X-Workspace-ID", testWorkspaceID)
		w := httptest.NewRecorder()
		hfn(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: expected 401, got %d", path, w.Code)
		}
	}
}

// ---------------------------------------------------------------------------
// Tab1 A1: activity summary math
// ---------------------------------------------------------------------------

func TestAnalyticsActivitySummary(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID, agentID := analyticsTestAgent(t, ctx)
	now := time.Now().UTC()

	// 2 member-created issues in the window → total_issues >= 2, active_members >= 1.
	issue1 := seedAnalyticsIssue(t, ctx, "clotest-a1-i1", "", "", now.Add(-24*time.Hour))
	seedAnalyticsIssue(t, ctx, "clotest-a1-i2", "agent", agentID, now.Add(-2*time.Hour))
	// A task initiated by the member counts as activity.
	started := now.Add(-time.Hour)
	completed := now
	seedAnalyticsTask(t, ctx, runtimeID, agentID, issue1, "completed", &started, &completed, "", now.Add(-2*time.Hour))

	w := httptest.NewRecorder()
	testHandler.GetAnalyticsActivitySummary(w, newRequest("GET", "/api/analytics/activity/summary?days=30", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("summary: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var s AnalyticsActivitySummary
	if err := json.NewDecoder(w.Body).Decode(&s); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if s.TotalMembers < 1 {
		t.Errorf("summary total_members = %d, want >= 1", s.TotalMembers)
	}
	if s.ActiveMembers < 1 {
		t.Errorf("summary active_members = %d, want >= 1", s.ActiveMembers)
	}
	if s.TotalIssues < 2 {
		t.Errorf("summary total_issues = %d, want >= 2", s.TotalIssues)
	}
	if s.Window.Start == "" || s.Window.End == "" {
		t.Errorf("summary window = %+v, want start/end dates", s.Window)
	}
	if s.PerCapitaIssueVolume != nil && *s.PerCapitaIssueVolume <= 0 {
		t.Errorf("summary per_capita_issue_volume = %v, want > 0", *s.PerCapitaIssueVolume)
	}
}

// ---------------------------------------------------------------------------
// Tab1 A2: heatmap shape (metric echo + days/hours array)
// ---------------------------------------------------------------------------

func TestAnalyticsActivityHeatmap(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	seedAnalyticsIssue(t, ctx, "clotest-a2-i1", "", "", time.Now().UTC().Add(-24*time.Hour))

	for _, metric := range []string{"activity_events", "issue_volume", "active_days"} {
		w := httptest.NewRecorder()
		testHandler.GetAnalyticsActivityHeatmap(w, newRequest("GET", "/api/analytics/activity/heatmap?metric="+metric, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("heatmap %s: expected 200, got %d: %s", metric, w.Code, w.Body.String())
		}
		var h AnalyticsActivityHeatmap
		if err := json.NewDecoder(w.Body).Decode(&h); err != nil {
			t.Fatalf("decode heatmap %s: %v", metric, err)
		}
		if h.Metric != metric {
			t.Errorf("heatmap %s: metric = %q, want echo %q", metric, h.Metric, metric)
		}
		if h.MaxValue < 0 {
			t.Errorf("heatmap %s: max_value = %d, want >= 0", metric, h.MaxValue)
		}
		for _, d := range h.Days {
			if d.Date == "" {
				t.Errorf("heatmap %s: day with empty date", metric)
			}
			for _, cell := range d.Hours {
				if cell.Hour < 0 || cell.Hour > 23 {
					t.Errorf("heatmap %s: hour = %d out of range", metric, cell.Hour)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Tab1 A4/A5: adoption summary + trend
// ---------------------------------------------------------------------------

func TestAnalyticsAdoption(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	_, agentID := analyticsTestAgent(t, ctx)
	now := time.Now().UTC()

	seedAnalyticsIssue(t, ctx, "clotest-a4-i1", "", "", now.Add(-48*time.Hour))
	seedAnalyticsIssue(t, ctx, "clotest-a4-i2", "agent", agentID, now.Add(-24*time.Hour))
	seedAnalyticsIssue(t, ctx, "clotest-a4-i3", "agent", agentID, now.Add(-3*time.Hour))

	// A4 summary.
	{
		w := httptest.NewRecorder()
		testHandler.GetAnalyticsAdoptionSummary(w, newRequest("GET", "/api/analytics/adoption/summary", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("adoption summary: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var s AnalyticsAdoptionSummary
		if err := json.NewDecoder(w.Body).Decode(&s); err != nil {
			t.Fatalf("decode adoption summary: %v", err)
		}
		if s.TotalIssues < 3 {
			t.Errorf("adoption total_issues = %d, want >= 3", s.TotalIssues)
		}
		if s.AgentAssignedIssues < 2 {
			t.Errorf("adoption agent_assigned = %d, want >= 2", s.AgentAssignedIssues)
		}
		if s.AssignmentRatio == nil || *s.AssignmentRatio <= 0 {
			t.Errorf("adoption assignment_ratio = %v, want > 0", s.AssignmentRatio)
		}
	}

	// A5 trend.
	{
		w := httptest.NewRecorder()
		testHandler.GetAnalyticsAdoptionTrend(w, newRequest("GET", "/api/analytics/adoption/trend", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("adoption trend: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var body struct {
			Points []AnalyticsAdoptionTrendPoint `json:"points"`
		}
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("decode adoption trend: %v", err)
		}
		if len(body.Points) == 0 {
			t.Errorf("adoption trend: expected >= 1 point, got 0")
		}
		for _, p := range body.Points {
			if p.Date == "" {
				t.Errorf("adoption trend: point with empty date")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Tab2 B1: funnel — merged_count=null when no VCS connection
// ---------------------------------------------------------------------------

func TestAnalyticsAgentFunnelNoVCS(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID, agentID := analyticsTestAgent(t, ctx)
	now := time.Now().UTC()

	// Assign + Execute stages exist; no VCS connection → merged stays null.
	issue := seedAnalyticsIssue(t, ctx, "clotest-funnel-"+now.Format("150405"), "", "", now.Add(-2*time.Hour))
	started := now.Add(-30 * time.Minute)
	seedAnalyticsTask(t, ctx, runtimeID, agentID, issue, "completed", &started, &now, "", now.Add(-time.Hour))

	w := httptest.NewRecorder()
	testHandler.GetAnalyticsAgentsFunnel(w, newRequest("GET", "/api/analytics/agents/funnel", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("funnel: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var f AnalyticsAgentFunnel
	if err := json.NewDecoder(w.Body).Decode(&f); err != nil {
		t.Fatalf("decode funnel: %v", err)
	}
	if f.AssignCount < 1 {
		t.Errorf("funnel assign_count = %d, want >= 1", f.AssignCount)
	}
	if f.ExecuteCount < 1 {
		t.Errorf("funnel execute_count = %d, want >= 1", f.ExecuteCount)
	}
	// merged_count: null (VCS 未接入), never 0-without-source and never error.
	if f.MergedCount != nil {
		t.Errorf("funnel merged_count = %v, want null (VCS not connected)", *f.MergedCount)
	}
}

// ---------------------------------------------------------------------------
// Tab2 B2/B3: agent performance + top
// ---------------------------------------------------------------------------

func TestAnalyticsAgentPerformance(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID, agentID := analyticsTestAgent(t, ctx)
	now := time.Now().UTC()

	started := now.Add(-10 * time.Minute)
	perfIssue := seedAnalyticsIssue(t, ctx, "clotest-perf-"+now.Format("150405"), "", "", now.Add(-time.Hour))
	seedAnalyticsTask(t, ctx, runtimeID, agentID, perfIssue, "completed", &started, &now, "", now.Add(-time.Hour))
	seedAnalyticsTask(t, ctx, runtimeID, agentID, perfIssue, "failed", &started, &now, "clotest-failure", now.Add(-30*time.Minute))

	{
		w := httptest.NewRecorder()
		testHandler.GetAnalyticsAgentsPerformance(w, newRequest("GET", "/api/analytics/agents/performance", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("performance: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var p AnalyticsAgentPerformance
		if err := json.NewDecoder(w.Body).Decode(&p); err != nil {
			t.Fatalf("decode performance: %v", err)
		}
		if p.TerminalCount < 2 {
			t.Errorf("performance terminal_count = %d, want >= 2", p.TerminalCount)
		}
		if p.CompletedCount < 1 || p.FailedCount < 1 {
			t.Errorf("performance completed/failed = %d/%d, want >= 1 each", p.CompletedCount, p.FailedCount)
		}
		if p.AvgDurationSeconds == nil || *p.AvgDurationSeconds <= 0 {
			t.Errorf("performance avg_duration = %v, want > 0", p.AvgDurationSeconds)
		}
		if p.P50DurationSeconds == nil || p.P95DurationSeconds == nil {
			t.Errorf("performance p50/p95 = %v/%v, want non-null", p.P50DurationSeconds, p.P95DurationSeconds)
		}
	}

	{
		w := httptest.NewRecorder()
		testHandler.GetAnalyticsAgentsTop(w, newRequest("GET", "/api/analytics/agents/top?limit=5", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("top agents: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var body struct {
			Items []AnalyticsTopAgent `json:"items"`
		}
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("decode top agents: %v", err)
		}
		found := false
		for _, a := range body.Items {
			if a.Name != "" {
				found = true
			}
		}
		if !found {
			t.Errorf("top agents: expected at least one named agent in items")
		}
	}
}

// ---------------------------------------------------------------------------
// Tab2 B4: skills overview
// ---------------------------------------------------------------------------

func TestAnalyticsSkillsOverview(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	_, agentID := analyticsTestAgent(t, ctx)
	seedAnalyticsSkill(t, ctx, agentID, "clotest-skill-"+time.Now().Format("150405"))

	w := httptest.NewRecorder()
	testHandler.GetAnalyticsSkillsOverview(w, newRequest("GET", "/api/analytics/skills/overview", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("skills: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var s AnalyticsSkillsOverview
	if err := json.NewDecoder(w.Body).Decode(&s); err != nil {
		t.Fatalf("decode skills: %v", err)
	}
	if s.TotalSkills < 1 {
		t.Errorf("skills total_skills = %d, want >= 1", s.TotalSkills)
	}
	if s.NewSkills < 0 {
		t.Errorf("skills new_skills = %d, want >= 0", s.NewSkills)
	}
	if s.Accumulation == nil || s.TopReused == nil {
		t.Errorf("skills accumulation/top_reused = %v/%v, want arrays", s.Accumulation, s.TopReused)
	}
}

// ---------------------------------------------------------------------------
// Tab3 G1-G4: guide states (Git source unconnected)
// ---------------------------------------------------------------------------

func TestAnalyticsGitGuideStates(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	// G1 eloc.
	{
		w := httptest.NewRecorder()
		testHandler.GetAnalyticsGitEloc(w, newRequest("GET", "/api/analytics/git/eloc?group_by=member", nil))
		assertSourceGuide(t, w)
	}
	// G2 quality.
	{
		w := httptest.NewRecorder()
		testHandler.GetAnalyticsGitQuality(w, newRequest("GET", "/api/analytics/git/quality", nil))
		assertSourceGuide(t, w)
	}
	// G3 repos.
	{
		w := httptest.NewRecorder()
		testHandler.GetAnalyticsGitRepos(w, newRequest("GET", "/api/analytics/git/repos", nil))
		assertSourceGuide(t, w)
	}
	// G4 prs — repo required; guide state when git unconnected even with repo.
	{
		w := httptest.NewRecorder()
		testHandler.GetAnalyticsGitPRs(w, newRequest("GET", "/api/analytics/git/prs?repo=org/repo", nil))
		assertSourceGuide(t, w)
	}
}

// ---------------------------------------------------------------------------
// Tab4 D1-D2: guide states (deployment pipeline unconnected)
// ---------------------------------------------------------------------------

func TestAnalyticsDoraGuideStates(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	// D1 lead-time metric=deploy → guide state without deployment events.
	{
		w := httptest.NewRecorder()
		testHandler.GetAnalyticsDoraLeadTime(w, newRequest("GET", "/api/analytics/dora/lead-time?metric=deploy", nil))
		assertSourceGuide(t, w)
	}
	// D1 lead-time metric=merged → guide state without VCS data.
	{
		w := httptest.NewRecorder()
		testHandler.GetAnalyticsDoraLeadTime(w, newRequest("GET", "/api/analytics/dora/lead-time?metric=merged", nil))
		assertSourceGuide(t, w)
	}
	// D2 deployments → guide state without deployment events.
	{
		w := httptest.NewRecorder()
		testHandler.GetAnalyticsDoraDeployments(w, newRequest("GET", "/api/analytics/dora/deployments", nil))
		assertSourceGuide(t, w)
	}
}

// ---------------------------------------------------------------------------
// L1-L2: identity guide states + department_id 400 (E18)
// ---------------------------------------------------------------------------

func TestAnalyticsIdentityGuideStates(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	// L1 lifecycle → guide state without identity_import rows.
	{
		w := httptest.NewRecorder()
		testHandler.GetAnalyticsIdentityLifecycle(w, newRequest("GET", "/api/analytics/identity/lifecycle", nil))
		assertSourceGuide(t, w)
	}
	// L2 departments → guide state when no member has a department.
	{
		w := httptest.NewRecorder()
		testHandler.GetAnalyticsIdentityDepartments(w, newRequest("GET", "/api/analytics/identity/departments", nil))
		assertSourceGuide(t, w)
	}
}

func TestAnalyticsDepartmentFilterUnconfigured(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	// No member in the fixture workspace has a department configured, so passing
	// department_id must return 400 (E18) on department-capable endpoints.
	req := newRequest("GET", "/api/analytics/activity/summary?department_id=dept-x", nil)
	w := httptest.NewRecorder()
	testHandler.GetAnalyticsActivitySummary(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("department filter: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if body.Error == "" {
		t.Errorf("department filter: expected human-readable error")
	}
}

func TestAnalyticsProjectIDMalformed(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	req := newRequest("GET", "/api/analytics/activity/summary?project_id=not-a-uuid", nil)
	w := httptest.NewRecorder()
	testHandler.GetAnalyticsActivitySummary(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("malformed project_id: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// B6 collaboration blockers
// ---------------------------------------------------------------------------

func TestAnalyticsCollaborationBlockers(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	now := time.Now().UTC()
	issueID := seedAnalyticsIssue(t, ctx, "clotest-blocker-"+now.Format("150405"), "", "", now.Add(-48*time.Hour))

	// Mark it blocked via activity_log (the blocker query's source).
	if _, err := testPool.Exec(ctx, `
		INSERT INTO activity_log (workspace_id, issue_id, actor_type, actor_id, action, details, created_at)
		VALUES ($1, $2, 'member', $3, 'status_changed', '{"to":"blocked"}'::jsonb, $4)
	`, testWorkspaceID, issueID, testUserID, now.Add(-24*time.Hour)); err != nil {
		t.Fatalf("insert blocker activity: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.GetAnalyticsCollaborationSummary(w, newRequest("GET", "/api/analytics/collaboration/summary", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("collab summary: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var c AnalyticsCollaborationSummary
	if err := json.NewDecoder(w.Body).Decode(&c); err != nil {
		t.Fatalf("decode collab summary: %v", err)
	}
	if c.TotalIssues < 1 {
		t.Errorf("collab total_issues = %d, want >= 1", c.TotalIssues)
	}
	if c.BlockerOpenCount < 0 {
		t.Errorf("collab blocker_open_count = %d, want >= 0", c.BlockerOpenCount)
	}

	w2 := httptest.NewRecorder()
	testHandler.GetAnalyticsCollaborationBlockers(w2, newRequest("GET", "/api/analytics/collaboration/blockers?limit=20", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("blockers: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var body struct {
		Items []AnalyticsBlocker `json:"items"`
	}
	if err := json.NewDecoder(w2.Body).Decode(&body); err != nil {
		t.Fatalf("decode blockers: %v", err)
	}
	found := false
	for _, b := range body.Items {
		if b.IssueID == issueID {
			found = true
			if b.Status == "" || b.BlockedAt == "" {
				t.Errorf("blocker: empty status/blocked_at = %q/%q", b.Status, b.BlockedAt)
			}
		}
	}
	if !found {
		t.Errorf("blockers: expected seeded blocker issue in items")
	}
}

// ---------------------------------------------------------------------------
// Limit parse sanity
// ---------------------------------------------------------------------------

func TestAnalyticsLimitClamping(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/analytics/activity/top-members?limit=99999", nil)
	if got := parseAnalyticsLimit(req, 10, 50); got != 50 {
		t.Errorf("limit clamp: got %d, want 50", got)
	}
	req2 := httptest.NewRequest("GET", "/api/analytics/activity/top-members?limit=abc", nil)
	if got := parseAnalyticsLimit(req2, 10, 50); got != 10 {
		t.Errorf("limit default: got %d, want 10", got)
	}
	req3 := httptest.NewRequest("GET", "/api/analytics/activity/top-members?days=999", nil)
	if got := parseAnalyticsDays(req3); got != 30 {
		t.Errorf("days fallback: got %d, want 30", got)
	}
}

// ---------------------------------------------------------------------------
// Stage-10 review fixes
// ---------------------------------------------------------------------------

// TestAnalyticsQualityNullableSnapshot (P1-03) seeds a quality snapshot whose
// coverage / duplication_rate are NULL and asserts G2 returns 200 with null
// values — not a 500 from scanning NULL into a float64.
func TestAnalyticsQualityNullableSnapshot(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO repo_quality_snapshot (workspace_id, repo, coverage, vulnerabilities, duplication_rate, snapshot_at)
		VALUES ($1, 'clotest-repo-null', NULL, 3, NULL, now())
	`, testWorkspaceID); err != nil {
		t.Fatalf("insert nullable snapshot: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM repo_quality_snapshot WHERE repo = 'clotest-repo-null' AND workspace_id = $1`, testWorkspaceID)
	})

	w := httptest.NewRecorder()
	testHandler.GetAnalyticsGitQuality(w, newRequest("GET", "/api/analytics/git/quality?repo=clotest-repo-null", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("nullable snapshot: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var q AnalyticsQuality
	if err := json.NewDecoder(w.Body).Decode(&q); err != nil {
		t.Fatalf("decode quality: %v", err)
	}
	if !q.SourceStatus.Ready {
		t.Errorf("quality: source_status.ready should be true when snapshots exist")
	}
	if q.Coverage != nil {
		t.Errorf("quality coverage = %v, want null for NULL column", *q.Coverage)
	}
	if q.DuplicationRate != nil {
		t.Errorf("quality duplication_rate = %v, want null for NULL column", *q.DuplicationRate)
	}
	if q.Vulnerabilities == nil || *q.Vulnerabilities != 3 {
		t.Errorf("quality vulnerabilities = %v, want 3", q.Vulnerabilities)
	}
}

// TestAnalyticsDeploymentsTrendMttr (P1-02) seeds recovered failed deployments
// and asserts D2 trend[].mttr_seconds is populated per week.
func TestAnalyticsDeploymentsTrendMttr(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	now := time.Now().UTC()
	// Two failed deployments ~1h ago with a 30-min recovery each.
	for i := 0; i < 2; i++ {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO deployment_event (workspace_id, deployment_id, app, started_at, finished_at, result, recovered_at)
			VALUES ($1, $2, 'clotest-app', $3, $4, 'failed', $5)
		`, testWorkspaceID, fmt.Sprintf("clotest-deploy-%d", i), now.Add(-2*time.Hour), now.Add(-time.Hour), now.Add(-30*time.Minute)); err != nil {
			t.Fatalf("insert deployment: %v", err)
		}
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM deployment_event WHERE deployment_id LIKE 'clotest-deploy-%' AND workspace_id = $1`, testWorkspaceID)
	})

	w := httptest.NewRecorder()
	testHandler.GetAnalyticsDoraDeployments(w, newRequest("GET", "/api/analytics/dora/deployments?days=7", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("deployments: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var d AnalyticsDeployments
	if err := json.NewDecoder(w.Body).Decode(&d); err != nil {
		t.Fatalf("decode deployments: %v", err)
	}
	if !d.SourceStatus.Ready {
		t.Errorf("deployments: source_status.ready should be true with events")
	}
	if d.SourceStatus.UpdatedAt == nil {
		t.Errorf("deployments: source_status.updated_at should be set (P3-02)")
	}
	if d.TotalDeployments < 2 || d.FailedDeployments < 2 {
		t.Errorf("deployments totals = %d/%d, want >= 2/2", d.TotalDeployments, d.FailedDeployments)
	}
	found := false
	for _, p := range d.Trend {
		if p.Deployments >= 2 && p.Failed >= 2 && p.MTTRSeconds != nil && *p.MTTRSeconds > 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("deployments trend: expected a week with failed>=2 and mttr_seconds populated")
	}
}

// TestAnalyticsLeadTimeDeployUpdatedAt (P3-02) asserts D1 metric=deploy returns
// source_status.updated_at when deployment events exist.
func TestAnalyticsLeadTimeDeployUpdatedAt(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	now := time.Now().UTC()
	// A successful deployment linked to a seeded issue.
	issueID := seedAnalyticsIssue(t, ctx, "clotest-d1-"+now.Format("150405"), "", "", now.Add(-72*time.Hour))
	if _, err := testPool.Exec(ctx, `
		INSERT INTO deployment_event (workspace_id, deployment_id, app, started_at, finished_at, result, issue_ids)
		VALUES ($1, 'clotest-d1-deploy', 'clotest-app', $2, $3, 'success', $4::jsonb)
	`, testWorkspaceID, now.Add(-2*time.Hour), now.Add(-time.Hour), fmt.Sprintf(`["%s"]`, issueID)); err != nil {
		t.Fatalf("insert deployment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM deployment_event WHERE deployment_id = 'clotest-d1-deploy' AND workspace_id = $1`, testWorkspaceID)
	})

	w := httptest.NewRecorder()
	testHandler.GetAnalyticsDoraLeadTime(w, newRequest("GET", "/api/analytics/dora/lead-time?metric=deploy&days=7", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("lead-time: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var lt AnalyticsLeadTime
	if err := json.NewDecoder(w.Body).Decode(&lt); err != nil {
		t.Fatalf("decode lead time: %v", err)
	}
	if !lt.SourceStatus.Ready {
		t.Errorf("lead-time: source_status.ready should be true with deployments")
	}
	if lt.SourceStatus.UpdatedAt == nil {
		t.Errorf("lead-time: source_status.updated_at should be set (P3-02)")
	}
	if len(lt.Points) < 1 {
		t.Errorf("lead-time: expected >= 1 weekly point, got %d", len(lt.Points))
	}
}

// TestAnalyticsIdentityResultNullable (P2-04) seeds an identity event with a
// NULL result (offboard) and asserts L1 recent_events[].result is null.
func TestAnalyticsIdentityResultNullable(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO identity_import (workspace_id, event_type, member_name, occurred_at, result)
		VALUES ($1, 'offboard', 'clotest-user', $2, NULL)
	`, testWorkspaceID, now.Add(-24*time.Hour)); err != nil {
		t.Fatalf("insert identity event: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM identity_import WHERE member_name = 'clotest-user' AND workspace_id = $1`, testWorkspaceID)
	})

	w := httptest.NewRecorder()
	testHandler.GetAnalyticsIdentityLifecycle(w, newRequest("GET", "/api/analytics/identity/lifecycle", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("lifecycle: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var l AnalyticsLifecycle
	if err := json.NewDecoder(w.Body).Decode(&l); err != nil {
		t.Fatalf("decode lifecycle: %v", err)
	}
	if !l.SourceStatus.Ready {
		t.Errorf("lifecycle: source_status.ready should be true with identity events")
	}
	found := false
	for _, e := range l.RecentEvents {
		if e.MemberName == "clotest-user" {
			found = true
			if e.Result != nil {
				t.Errorf("lifecycle: offboard result = %q, want null (P2-04)", *e.Result)
			}
		}
	}
	if !found {
		t.Errorf("lifecycle: expected seeded offboard event in recent_events")
	}
}

// TestAnalyticsMergedCountFiltered (P2-01) wires up a VCS connection + merged
// PR linked to an issue and asserts B1 merged_count becomes a number (>= 1)
// rather than staying null, with the issue visible under the workspace scope.
func TestAnalyticsMergedCountFiltered(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	now := time.Now().UTC()

	// VCS connection toggles the merged guide state.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO vcs_connection (workspace_id, provider, instance_url, account_login, access_token_encrypted, webhook_secret_encrypted, connected_by_id, created_at)
		VALUES ($1, 'forgejo', $2, 'clotest', 'enc', 'enc', $3, now())
	`, testWorkspaceID, "https://clotest.example.com", testUserID); err != nil {
		t.Fatalf("insert vcs connection: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM vcs_connection WHERE instance_url = 'https://clotest.example.com' AND workspace_id = $1`, testWorkspaceID)
	})

	issueID := seedAnalyticsIssue(t, ctx, "clotest-merged-"+now.Format("150405"), "", "", now.Add(-72*time.Hour))
	connID := ""
	if err := testPool.QueryRow(ctx, `SELECT id FROM vcs_connection WHERE workspace_id = $1 AND instance_url = $2`, testWorkspaceID, "https://clotest.example.com").Scan(&connID); err != nil {
		t.Fatalf("fetch vcs connection: %v", err)
	}
	var prID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO vcs_pull_request (workspace_id, connection_id, repo_owner, repo_name, pr_number, title, state, html_url, pr_created_at, merged_at, pr_updated_at, additions, deletions)
		VALUES ($1, $2, 'clotest', 'clotest-repo', 1, 'clotest merged pr', 'merged', $3, $4, $5, $5, 10, 5)
		RETURNING id
	`, testWorkspaceID, connID, "https://clotest.example.com/pr/1", now.Add(-72*time.Hour), now.Add(-24*time.Hour)).Scan(&prID); err != nil {
		t.Fatalf("insert merged PR: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_vcs_pull_request WHERE pull_request_id = $1`, prID)
		testPool.Exec(ctx, `DELETE FROM vcs_pull_request WHERE id = $1`, prID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue_vcs_pull_request (issue_id, pull_request_id)
		VALUES ($1, $2)
	`, issueID, prID); err != nil {
		t.Fatalf("link issue to PR: %v", err)
	}

	w := httptest.NewRecorder()
	testHandler.GetAnalyticsAgentsFunnel(w, newRequest("GET", "/api/analytics/agents/funnel?days=30", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("funnel: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var f AnalyticsAgentFunnel
	if err := json.NewDecoder(w.Body).Decode(&f); err != nil {
		t.Fatalf("decode funnel: %v", err)
	}
	if f.MergedCount == nil {
		t.Errorf("funnel merged_count = nil, want a number with VCS + merged PR (P2-01)")
	} else if *f.MergedCount < 1 {
		t.Errorf("funnel merged_count = %d, want >= 1", *f.MergedCount)
	}
	// merged_ratio stays null when execute_count = 0 (contract: null = 分母 0).
	if f.MergedRatio != nil {
		t.Errorf("funnel merged_ratio = %v, want null with no executed tasks", *f.MergedRatio)
	}
}

// TestAnalyticsBlockerNPlusOne (P2-02) verifies the batched resolution query
// returns the same resolved-state the per-issue path did for a seeded blocker.
func TestAnalyticsBlockerNPlusOne(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	now := time.Now().UTC()
	issueID := seedAnalyticsIssue(t, ctx, "clotest-blocker2-"+now.Format("150405"), "", "", now.Add(-48*time.Hour))
	if _, err := testPool.Exec(ctx, `
		INSERT INTO activity_log (workspace_id, issue_id, actor_type, actor_id, action, details, created_at)
		VALUES ($1, $2, 'member', $3, 'status_changed', '{"to":"blocked"}'::jsonb, $4)
	`, testWorkspaceID, issueID, testUserID, now.Add(-24*time.Hour)); err != nil {
		t.Fatalf("insert blocked activity: %v", err)
	}
	// Resolve it with an agent comment one hour later.
	seedAnalyticsComment(t, ctx, issueID, "agent", "00000000-0000-0000-0000-000000000000", now.Add(-23*time.Hour))

	w := httptest.NewRecorder()
	testHandler.GetAnalyticsCollaborationBlockers(w, newRequest("GET", "/api/analytics/collaboration/blockers?limit=50", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("blockers: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Items []AnalyticsBlocker `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode blockers: %v", err)
	}
	for _, b := range body.Items {
		if b.IssueID == issueID {
			if b.Status != "resolved" {
				t.Errorf("blocker status = %q, want resolved after agent comment (P2-02)", b.Status)
			}
			if b.ResponseSeconds == nil || *b.ResponseSeconds <= 0 {
				t.Errorf("blocker response_seconds = %v, want > 0", b.ResponseSeconds)
			}
		}
	}
}
