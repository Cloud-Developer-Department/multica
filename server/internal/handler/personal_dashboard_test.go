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
// Personal-dimension dashboard endpoint tests (CLO-212).
//
// Seeded rows use a distinct provider ("personal-test") and model names so
// assertions are isolated from rows other tests leave in the shared fixture
// workspace. The personal queries filter on initiator_user_id, and the shared
// fixture's tasks carry NULL initiators, so they never contaminate "my data".
// ---------------------------------------------------------------------------

// personalTestRuntimeAgent returns the fixture workspace's first runtime and
// agent, mirroring the other dashboard tests.
func personalTestRuntimeAgent(t *testing.T, ctx context.Context) (runtimeID, agentID string) {
	t.Helper()
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}
	return
}

// seedPersonalTask inserts a terminal task for initiatorUserID and cleans up
// on test end.
func seedPersonalTask(t *testing.T, ctx context.Context, runtimeID, agentID, initiatorUserID, status string, startedAt, completedAt *time.Time, failureReason string) string {
	t.Helper()
	var id string
	err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, initiator_user_id, started_at, completed_at, failure_reason, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		RETURNING id
	`, agentID, runtimeID, status, initiatorUserID, startedAt, completedAt, failureReason).Scan(&id)
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, id) })
	return id
}

// seedPersonalUsage attaches a task_usage row (cost_usd_ticks nil = uncosted)
// and cleans up on test end.
func seedPersonalUsage(t *testing.T, ctx context.Context, taskID string, input, output, cacheRead, cacheWrite int64, costUsdTicks *int64) {
	t.Helper()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, cost_usd_ticks, created_at)
		VALUES ($1, 'personal-test', $2, $3, $4, $5, $6, $7, now())
	`, taskID, modelFor(input), input, output, cacheRead, cacheWrite, costUsdTicks); err != nil {
		t.Fatalf("insert task_usage: %v", err)
	}
}

func modelFor(input int64) string {
	return fmt.Sprintf("pt-model-%d", input)
}

// TestPersonalDashboardSummaryAndSeries seeds a small personal dataset and
// verifies summary / trend / models / duration / runtime-trend / errors
// against the API contract's numbers.
func TestPersonalDashboardSummaryAndSeries(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID, agentID := personalTestRuntimeAgent(t, ctx)

	now := time.Now().UTC()
	startedA := now.Add(-10 * time.Minute) // 600s run
	startedB := now.Add(-5 * time.Minute)  // 300s run

	// A: completed, priced (cost_usd_ticks set), 10 min.
	taskA := seedPersonalTask(t, ctx, runtimeID, agentID, testUserID, "completed", &startedA, &now, "")
	costA := int64(1000)
	seedPersonalUsage(t, ctx, taskA, 100, 50, 20, 10, &costA)
	// B: completed, UNcosted (cost_usd_ticks NULL), 5 min.
	taskB := seedPersonalTask(t, ctx, runtimeID, agentID, testUserID, "completed", &startedB, &now, "")
	seedPersonalUsage(t, ctx, taskB, 200, 0, 0, 0, nil)
	// C: failed, never started (queued_expired), no duration.
	seedPersonalTask(t, ctx, runtimeID, agentID, testUserID, "failed", nil, &now, "queued_expired")

	// --- summary ---
	{
		w := httptest.NewRecorder()
		testHandler.GetPersonalUsageSummary(w, newRequest("GET", "/api/dashboard/personal/summary?range=week", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("summary: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var s struct {
			Range        string   `json:"range"`
			TokenInput   int64    `json:"token_input"`
			TokenOutput  int64    `json:"token_output"`
			TokenCacheRd int64    `json:"token_cache_read"`
			TokenCacheWr int64    `json:"token_cache_write"`
			TokenTotal   int64    `json:"token_total"`
			CostTicks    int64    `json:"cost_usd_ticks"`
			UncostedIn   int64    `json:"uncosted_input_tokens"`
			TaskCount    int64    `json:"task_count"`
			Completed    int64    `json:"completed_count"`
			Failed       int64    `json:"failed_count"`
			RunSeconds   int64    `json:"run_seconds"`
			SuccessRate  *float64 `json:"success_rate"`
		}
		if err := json.NewDecoder(w.Body).Decode(&s); err != nil {
			t.Fatalf("decode summary: %v", err)
		}
		if s.Range != "week" {
			t.Errorf("summary range = %q, want week", s.Range)
		}
		if s.TokenInput != 300 || s.TokenOutput != 50 || s.TokenCacheRd != 20 || s.TokenCacheWr != 10 || s.TokenTotal != 380 {
			t.Errorf("summary tokens = %d/%d/%d/%d total %d, want 300/50/20/10 total 380", s.TokenInput, s.TokenOutput, s.TokenCacheRd, s.TokenCacheWr, s.TokenTotal)
		}
		if s.CostTicks != 1000 {
			t.Errorf("summary cost_usd_ticks = %d, want 1000", s.CostTicks)
		}
		if s.UncostedIn != 200 {
			t.Errorf("summary uncosted_input = %d, want 200", s.UncostedIn)
		}
		if s.TaskCount != 3 || s.Completed != 2 || s.Failed != 1 {
			t.Errorf("summary counts = %d/%d/%d, want 3/2/1", s.TaskCount, s.Completed, s.Failed)
		}
		if s.RunSeconds != 900 {
			t.Errorf("summary run_seconds = %d, want 900", s.RunSeconds)
		}
		if s.SuccessRate == nil || *s.SuccessRate != 66.7 {
			t.Errorf("summary success_rate = %v, want 66.7", s.SuccessRate)
		}
	}

	// --- trend (today: hourly buckets, zero-filled) ---
	{
		w := httptest.NewRecorder()
		testHandler.GetPersonalUsageTrend(w, newRequest("GET", "/api/dashboard/personal/trend?range=today", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("trend: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var tr struct {
			Range  string `json:"range"`
			Points []struct {
				Time   string `json:"time"`
				Tokens int64  `json:"tokens"`
			} `json:"points"`
		}
		if err := json.NewDecoder(w.Body).Decode(&tr); err != nil {
			t.Fatalf("decode trend: %v", err)
		}
		if len(tr.Points) < 1 {
			t.Fatalf("trend: expected >=1 hourly point, got %d", len(tr.Points))
		}
		last := tr.Points[len(tr.Points)-1]
		if last.Tokens != 380 {
			t.Errorf("trend: last bucket tokens = %d, want 380", last.Tokens)
		}
	}

	// --- models ---
	{
		w := httptest.NewRecorder()
		testHandler.GetPersonalUsageModels(w, newRequest("GET", "/api/dashboard/personal/models?range=week", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("models: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var m struct {
			Total int64 `json:"total_tokens"`
			Items []struct {
				Provider string `json:"provider"`
				TokenIn  int64  `json:"token_input"`
				TokenTot int64  `json:"token_total"`
			} `json:"items"`
		}
		if err := json.NewDecoder(w.Body).Decode(&m); err != nil {
			t.Fatalf("decode models: %v", err)
		}
		if m.Total != 380 {
			t.Errorf("models total_tokens = %d, want 380", m.Total)
		}
		if len(m.Items) != 2 {
			t.Errorf("models items = %d, want 2", len(m.Items))
		}
		for _, it := range m.Items {
			if it.Provider != "personal-test" {
				t.Errorf("models provider = %q, want personal-test", it.Provider)
			}
		}
	}

	// --- duration: A=600s→5-15m, B=300s→5-15m, C excluded ---
	{
		w := httptest.NewRecorder()
		testHandler.GetPersonalUsageDuration(w, newRequest("GET", "/api/dashboard/personal/duration?range=week", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("duration: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var d struct {
			Total   int64 `json:"total"`
			Buckets []struct {
				Bucket string `json:"bucket"`
				Count  int64  `json:"count"`
			} `json:"buckets"`
		}
		if err := json.NewDecoder(w.Body).Decode(&d); err != nil {
			t.Fatalf("decode duration: %v", err)
		}
		if len(d.Buckets) != 5 {
			t.Fatalf("duration buckets = %d, want 5", len(d.Buckets))
		}
		if d.Total != 2 {
			t.Errorf("duration total = %d, want 2", d.Total)
		}
		for _, b := range d.Buckets {
			if b.Bucket == "5-15m" && b.Count != 2 {
				t.Errorf("duration 5-15m count = %d, want 2", b.Count)
			}
			if b.Bucket != "5-15m" && b.Count != 0 {
				t.Errorf("duration bucket %s count = %d, want 0", b.Bucket, b.Count)
			}
		}
	}

	// --- runtime-trend (week: daily buckets) ---
	{
		w := httptest.NewRecorder()
		testHandler.GetPersonalRuntimeTrend(w, newRequest("GET", "/api/dashboard/personal/runtime-trend?range=week", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("runtime-trend: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var rt struct {
			Points []struct {
				RunSeconds int64 `json:"run_seconds"`
				TaskCount  int64 `json:"task_count"`
			} `json:"points"`
		}
		if err := json.NewDecoder(w.Body).Decode(&rt); err != nil {
			t.Fatalf("decode runtime-trend: %v", err)
		}
		if len(rt.Points) < 1 {
			t.Fatalf("runtime-trend: expected >=1 point, got %d", len(rt.Points))
		}
		last := rt.Points[len(rt.Points)-1]
		if last.RunSeconds != 900 || last.TaskCount != 2 {
			t.Errorf("runtime-trend last = %d sec / %d tasks, want 900 / 2", last.RunSeconds, last.TaskCount)
		}
	}

	// --- errors ---
	{
		w := httptest.NewRecorder()
		testHandler.GetPersonalUsageErrors(w, newRequest("GET", "/api/dashboard/personal/errors?range=week", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("errors: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var e struct {
			Completed int64 `json:"completed_count"`
			Failed    int64 `json:"failed_count"`
			Rate      *float64 `json:"failure_rate"`
			Types     []struct {
				Reason string  `json:"reason"`
				Count  int64   `json:"count"`
				Pct    float64 `json:"pct"`
			} `json:"types"`
		}
		if err := json.NewDecoder(w.Body).Decode(&e); err != nil {
			t.Fatalf("decode errors: %v", err)
		}
		if e.Completed != 2 || e.Failed != 1 || e.Rate == nil || *e.Rate != 33.3 {
			t.Errorf("errors counts = %d/%d rate=%v, want 2/1/33.3", e.Completed, e.Failed, e.Rate)
		}
		if len(e.Types) != 1 || e.Types[0].Reason != "queued_expired" || e.Types[0].Count != 1 || e.Types[0].Pct != 100.0 {
			t.Errorf("errors types = %+v, want [queued_expired 1 100.0]", e.Types)
		}
	}
}

// createPersonalRankWorkspace builds a fresh workspace (member = testUserID)
// with a runtime + agent, so the rank test's "team" population is fully
// deterministic.
func createPersonalRankWorkspace(t *testing.T, ctx context.Context) (workspaceID, runtimeID, agentID string) {
	t.Helper()
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug)
		VALUES ('personal-rank-test', 'personal-rank-test-' || gen_random_uuid()::text)
		RETURNING id
	`).Scan(&workspaceID); err != nil {
		t.Fatalf("create rank workspace: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, workspaceID) })
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
	`, workspaceID, testUserID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at)
		VALUES ($1, NULL, 'personal-rank-runtime', 'cloud', 'personal-rank', 'online', '{}'::jsonb, '{}'::jsonb, $2, now())
		RETURNING id
	`, workspaceID, testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("create rank runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id)
		VALUES ($1, 'personal-rank-agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 'public_to', 1, $3)
		RETURNING id
	`, workspaceID, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create rank agent: %v", err)
	}
	return
}

// TestPersonalUsageRank verifies the leaderboard math and the privacy
// boundary (only the caller's own numbers + anonymous aggregates on the wire).
func TestPersonalUsageRank(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	wsID, runtimeID, agentID := createPersonalRankWorkspace(t, ctx)
	now := time.Now().UTC()
	started := now.Add(-2 * time.Minute)

	// Other users with distinct token totals. initiator_user_id has no FK, so
	// plain UUIDs are fine.
	userA := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa" // 1000 tokens
	userB := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb" // 100 tokens
	seedMember := func(uuid string, tokens int64) {
		taskID := seedPersonalTask(t, ctx, runtimeID, agentID, uuid, "completed", &started, &now, "")
		seedPersonalUsage(t, ctx, taskID, tokens, 0, 0, 0, nil)
	}
	seedMember(userA, 1000)
	seedMember(userB, 100)
	// Me: 380 tokens.
	myTask := seedPersonalTask(t, ctx, runtimeID, agentID, testUserID, "completed", &started, &now, "")
	seedPersonalUsage(t, ctx, myTask, 380, 0, 0, 0, nil)

	req := newRequest("GET", "/api/dashboard/personal/rank?range=week", nil)
	req.Header.Set("X-Workspace-ID", wsID)
	w := httptest.NewRecorder()
	testHandler.GetPersonalUsageRank(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("rank: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// The wire shape must contain NO other member identity or token totals.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode rank: %v", err)
	}
	for key := range raw {
		if key == "user_id" || key == "users" || key == "members" || key == "leaderboard" {
			t.Errorf("rank: privacy leak, unexpected field %q", key)
		}
	}

	var r struct {
		MyTokens     int64    `json:"my_tokens"`
		Rank         *int64   `json:"rank"`
		TotalMembers int64    `json:"total_members"`
		ExceedPct    *float64 `json:"exceed_pct"`
		TeamAvg      *int64   `json:"team_avg_tokens"`
		TeamMedian   *int64   `json:"team_median_tokens"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatalf("decode rank struct: %v", err)
	}
	if r.MyTokens != 380 {
		t.Errorf("rank my_tokens = %d, want 380", r.MyTokens)
	}
	if r.Rank == nil || *r.Rank != 2 {
		t.Errorf("rank = %v, want 2", r.Rank)
	}
	if r.TotalMembers != 3 {
		t.Errorf("rank total_members = %d, want 3", r.TotalMembers)
	}
	if r.ExceedPct == nil || *r.ExceedPct != 50.0 {
		t.Errorf("rank exceed_pct = %v, want 50.0", r.ExceedPct)
	}
	// avg = (1000+100+380)/3 = 493.33 → 493; median of [100,380,1000] = 380.
	if r.TeamAvg == nil || *r.TeamAvg != 493 {
		t.Errorf("rank team_avg = %v, want 493", r.TeamAvg)
	}
	if r.TeamMedian == nil || *r.TeamMedian != 380 {
		t.Errorf("rank team_median = %v, want 380", r.TeamMedian)
	}
}

// TestPersonalDashboardEmptyState verifies null/zero semantics when the user
// has no terminal tasks in the window (B1/B2 in the data spec).
func TestPersonalDashboardEmptyState(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	wsID, _, _ := createPersonalRankWorkspace(t, ctx)

	req := newRequest("GET", "/api/dashboard/personal/summary?range=week", nil)
	req.Header.Set("X-Workspace-ID", wsID)
	w := httptest.NewRecorder()
	testHandler.GetPersonalUsageSummary(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("empty summary: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var s struct {
		TokenTotal  int64    `json:"token_total"`
		TaskCount   int64    `json:"task_count"`
		SuccessRate *float64 `json:"success_rate"`
	}
	if err := json.NewDecoder(w.Body).Decode(&s); err != nil {
		t.Fatalf("decode empty summary: %v", err)
	}
	if s.TokenTotal != 0 || s.TaskCount != 0 {
		t.Errorf("empty summary = tokens %d tasks %d, want 0/0", s.TokenTotal, s.TaskCount)
	}
	if s.SuccessRate != nil {
		t.Errorf("empty summary success_rate = %v, want null", *s.SuccessRate)
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/dashboard/personal/rank?range=week", nil)
	req.Header.Set("X-Workspace-ID", wsID)
	testHandler.GetPersonalUsageRank(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("empty rank: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var r struct {
		Rank         *int64   `json:"rank"`
		TotalMembers int64    `json:"total_members"`
		ExceedPct    *float64 `json:"exceed_pct"`
		TeamAvg      *int64   `json:"team_avg_tokens"`
		TeamMedian   *int64   `json:"team_median_tokens"`
	}
	if err := json.NewDecoder(w.Body).Decode(&r); err != nil {
		t.Fatalf("decode empty rank: %v", err)
	}
	if r.Rank != nil || r.ExceedPct != nil {
		t.Errorf("empty rank rank/exceed = %v/%v, want null/null", r.Rank, r.ExceedPct)
	}
	if r.TotalMembers != 0 {
		t.Errorf("empty rank total_members = %d, want 0", r.TotalMembers)
	}
	if r.TeamAvg != nil || r.TeamMedian != nil {
		t.Errorf("empty rank team_avg/median = %v/%v, want null/null", r.TeamAvg, r.TeamMedian)
	}
}

// TestPersonalDashboardUnauthorized ensures a missing session identity is
// rejected with 401.
func TestPersonalDashboardUnauthorized(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	req := httptest.NewRequest("GET", "/api/dashboard/personal/summary?range=week", nil)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	w := httptest.NewRecorder()
	testHandler.GetPersonalUsageSummary(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauthorized summary: expected 401, got %d", w.Code)
	}
}

// TestDashboardRatesLiveAndDegraded covers the /rates endpoint: live fetch,
// cache reuse, and default fallback when the external API fails.
func TestDashboardRatesLiveAndDegraded(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	var hits int
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"result":"success","base_code":"USD","rates":{"CNY":7.25}}`)
	}))
	defer live.Close()
	t.Setenv("MULTICA_DASHBOARD_FX_URL", live.URL)
	t.Setenv("MULTICA_DASHBOARD_FX_DEFAULT_CNY", "7.2")

	old := testHandler.DashboardRates
	svc := NewDashboardRatesService()
	testHandler.DashboardRates = svc
	t.Cleanup(func() { testHandler.DashboardRates = old })

	// Live path via the handler.
	w := httptest.NewRecorder()
	testHandler.GetDashboardRates(w, newRequest("GET", "/api/dashboard/rates", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("rates live: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp DashboardRatesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode rates: %v", err)
	}
	if resp.Source != "live" || resp.Rate != 7.25 {
		t.Errorf("rates live = source %q rate %v, want live/7.25", resp.Source, resp.Rate)
	}
	if resp.UpdatedAt == nil {
		t.Errorf("rates live: updated_at should be set")
	}
	if resp.DefaultRate != 7.2 {
		t.Errorf("rates default_rate = %v, want 7.2", resp.DefaultRate)
	}

	// Cache reuse: a second call must not hit the external API again.
	hits = 0
	w = httptest.NewRecorder()
	testHandler.GetDashboardRates(w, newRequest("GET", "/api/dashboard/rates", nil))
	if hits != 0 {
		t.Errorf("rates cache: expected 0 external calls on second request, got %d", hits)
	}

	// Degraded path: fresh service against a failing endpoint.
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()
	t.Setenv("MULTICA_DASHBOARD_FX_URL", failing.URL)
	svc2 := NewDashboardRatesService()
	testHandler.DashboardRates = svc2

	w = httptest.NewRecorder()
	testHandler.GetDashboardRates(w, newRequest("GET", "/api/dashboard/rates", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("rates degraded: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode degraded rates: %v", err)
	}
	if resp.Source != "default" || resp.Rate != 7.2 {
		t.Errorf("rates degraded = source %q rate %v, want default/7.2", resp.Source, resp.Rate)
	}
	if resp.UpdatedAt != nil {
		t.Errorf("rates degraded: updated_at should be null")
	}
}

// TestDashboardRatesServiceFallbackCache ensures the degraded value is also
// cached so an outage doesn't hammer the external API on every load.
func TestDashboardRatesServiceFallbackCache(t *testing.T) {
	ctx := context.Background()
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	t.Setenv("MULTICA_DASHBOARD_FX_URL", srv.URL)
	t.Setenv("MULTICA_DASHBOARD_FX_DEFAULT_CNY", "7.3")

	svc := NewDashboardRatesService()
	first := svc.Get(ctx)
	if first.Source != "default" || first.Rate != 7.3 {
		t.Fatalf("fallback = source %q rate %v, want default/7.3", first.Source, first.Rate)
	}
	hits = 0
	second := svc.Get(ctx)
	if hits != 0 {
		t.Errorf("fallback cache: expected 0 external calls on second Get, got %d", hits)
	}
	if second.Rate != 7.3 {
		t.Errorf("fallback cache rate = %v, want 7.3", second.Rate)
	}
}

// TestPersonalSummaryIgnoresClientUserID proves the endpoint reads the session
// user, not a client-supplied user_id: seeding usage for ANOTHER user and
// passing their id in the query must still return MY zeros.
func TestPersonalSummaryIgnoresClientUserID(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	wsID, runtimeID, agentID := createPersonalRankWorkspace(t, ctx)
	now := time.Now().UTC()
	started := now.Add(-2 * time.Minute)
	other := "99999999-9999-9999-9999-999999999999"
	taskID := seedPersonalTask(t, ctx, runtimeID, agentID, other, "completed", &started, &now, "")
	seedPersonalUsage(t, ctx, taskID, 5000, 0, 0, 0, nil)

	req := newRequest("GET", "/api/dashboard/personal/summary?range=week&user_id="+other, nil)
	req.Header.Set("X-Workspace-ID", wsID)
	w := httptest.NewRecorder()
	testHandler.GetPersonalUsageSummary(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var s struct {
		TokenTotal int64 `json:"token_total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&s); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if s.TokenTotal != 0 {
		t.Errorf("summary with other user_id = %d tokens, want 0 (session user must win)", s.TokenTotal)
	}
}
