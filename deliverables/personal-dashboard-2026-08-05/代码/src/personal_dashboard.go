package handler

import (
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// Personal-dimension usage dashboard (CLO-206 / CLO-212).
//
// Seven endpoints power the developer's personal dashboard:
//
//   GET /api/dashboard/personal/summary        KPI card (tokens / cost / tasks / runtime / success)
//   GET /api/dashboard/personal/trend          token / cost time series
//   GET /api/dashboard/personal/models         per-model token + cost split
//   GET /api/dashboard/personal/duration       single-task runtime histogram
//   GET /api/dashboard/personal/runtime-trend  total runtime time series
//   GET /api/dashboard/personal/errors         failure rate trend + error-type distribution
//   GET /api/dashboard/personal/rank           my rank + anonymous team aggregates
//
// Every endpoint follows the API contract (API-CLO-211):
//   * Personal scope: the current user from the login session (X-User-ID).
//     A client-supplied `user_id` is never accepted — callers are filtered
//     by session identity, so cross-user reads are impossible by construction.
//   * Window: ?range=today|week|month (default week), sliced under ?tz=.
//   * Attribution: agent_task_queue.initiator_user_id (autopilot runs count
//     toward the user who configured them); terminal tasks only, windowed on
//     completed_at.
//   * Cost: the raw split (cost_usd_ticks + uncosted_*_tokens) is returned
//     verbatim; USD→CNY conversion happens client-side from /rates.
// ---------------------------------------------------------------------------

// personalRange values.
const (
	personalRangeToday = "today"
	personalRangeWeek  = "week"
	personalRangeMonth = "month"
)

// parsePersonalRange reads ?range= and falls back to week for anything
// invalid or missing (API contract §0.2 — never a 4xx).
func parsePersonalRange(r *http.Request) string {
	switch r.URL.Query().Get("range") {
	case personalRangeToday, personalRangeMonth:
		return r.URL.Query().Get("range")
	default:
		return personalRangeWeek
	}
}

// personalScope carries the resolved auth / window context shared by every
// personal endpoint.
type personalScope struct {
	workspaceID pgtype.UUID
	userID      pgtype.UUID
	rangeKey    string
	tz          string
	loc         *time.Location
	since       pgtype.Timestamptz
	until       pgtype.Timestamptz
}

// resolvePersonalScope resolves the workspace, the session user, the ?range=
// window and the viewer tz for a personal endpoint. It writes the error
// response and returns ok=false when auth / membership fails.
func (h *Handler) resolvePersonalScope(w http.ResponseWriter, r *http.Request) (personalScope, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return personalScope{}, false
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return personalScope{}, false
	}
	uid, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return personalScope{}, false
	}

	rangeKey := parsePersonalRange(r)
	tz := h.resolveViewingTZ(r)
	loc, _ := time.LoadLocation(tz)
	if loc == nil {
		loc = time.UTC
	}
	start, until := personalWindow(rangeKey, loc)

	return personalScope{
		workspaceID: parseUUID(workspaceID),
		userID:      uid,
		rangeKey:    rangeKey,
		tz:          tz,
		loc:         loc,
		since:       pgtype.Timestamptz{Time: start, Valid: true},
		until:       pgtype.Timestamptz{Time: until, Valid: true},
	}, true
}

// personalWindow returns the [start, until) window for a range in the given
// location: today → today 00:00; week → this Monday 00:00; month → the 1st.
// `until` is "now", matching the API contract's window semantics.
func personalWindow(rangeKey string, loc *time.Location) (time.Time, time.Time) {
	now := time.Now().In(loc)
	var start time.Time
	switch rangeKey {
	case personalRangeToday:
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	case personalRangeMonth:
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	default:
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		start = start.AddDate(0, 0, -((int(now.Weekday())+6)%7))
	}
	return start, now
}

// personalBucketAxis builds the zero-fill axis shared by the trend endpoints
// (2 / 5 / 6): hourly buckets for `today`, daily buckets for week/month,
// each expressed as a local start-of-bucket instant.
func personalBucketAxis(rangeKey string, start time.Time, loc *time.Location) []time.Time {
	now := time.Now().In(loc)
	var buckets []time.Time
	if rangeKey == personalRangeToday {
		cur := start
		end := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, loc)
		for !cur.After(end) {
			buckets = append(buckets, cur)
			cur = cur.Add(time.Hour)
		}
		return buckets
	}
	cur := start
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	for !cur.After(end) {
		buckets = append(buckets, cur)
		cur = cur.AddDate(0, 0, 1)
	}
	return buckets
}

// round1 snaps a percentage to one decimal place.
func round1(v float64) float64 {
	return math.Round(v*10) / 10
}

// ratePct returns part/total×100 rounded to 1 decimal; nil when total is 0.
func ratePct(part, total int64) *float64 {
	if total <= 0 {
		return nil
	}
	v := round1(float64(part) / float64(total) * 100)
	return &v
}

// ---------------------------------------------------------------------------
// 1. GET /api/dashboard/personal/summary
// ---------------------------------------------------------------------------

// PersonalUsageSummaryResponse is the KPI card payload (API contract §1).
type PersonalUsageSummaryResponse struct {
	Range                    string  `json:"range"`
	TokenInput               int64   `json:"token_input"`
	TokenOutput              int64   `json:"token_output"`
	TokenCacheRead           int64   `json:"token_cache_read"`
	TokenCacheWrite          int64   `json:"token_cache_write"`
	TokenTotal               int64   `json:"token_total"`
	CostUSDTicks             int64   `json:"cost_usd_ticks"`
	UncostedInputTokens      int64   `json:"uncosted_input_tokens"`
	UncostedOutputTokens     int64   `json:"uncosted_output_tokens"`
	UncostedCacheReadTokens  int64   `json:"uncosted_cache_read_tokens"`
	UncostedCacheWriteTokens int64   `json:"uncosted_cache_write_tokens"`
	TaskCount                int64   `json:"task_count"`
	CompletedCount           int64   `json:"completed_count"`
	FailedCount              int64   `json:"failed_count"`
	RunSeconds               int64   `json:"run_seconds"`
	SuccessRate              *float64 `json:"success_rate"`
}

// GetPersonalUsageSummary serves the KPI card for the current user.
func (h *Handler) GetPersonalUsageSummary(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolvePersonalScope(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	usage, err := h.Queries.GetPersonalUsageSummary(ctx, db.GetPersonalUsageSummaryParams{
		WorkspaceID: scope.workspaceID,
		UserID:      scope.userID,
		Since:       scope.since,
		Until:       scope.until,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load usage summary")
		return
	}
	task, err := h.Queries.GetPersonalTaskSummary(ctx, db.GetPersonalTaskSummaryParams{
		WorkspaceID: scope.workspaceID,
		UserID:      scope.userID,
		Since:       scope.since,
		Until:       scope.until,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load task summary")
		return
	}

	resp := PersonalUsageSummaryResponse{
		Range:                    scope.rangeKey,
		TokenInput:               usage.InputTokens,
		TokenOutput:              usage.OutputTokens,
		TokenCacheRead:           usage.CacheReadTokens,
		TokenCacheWrite:          usage.CacheWriteTokens,
		TokenTotal:               usage.InputTokens + usage.OutputTokens + usage.CacheReadTokens + usage.CacheWriteTokens,
		CostUSDTicks:             usage.CostUsdTicks,
		UncostedInputTokens:      usage.UncostedInputTokens,
		UncostedOutputTokens:     usage.UncostedOutputTokens,
		UncostedCacheReadTokens:  usage.UncostedCacheReadTokens,
		UncostedCacheWriteTokens: usage.UncostedCacheWriteTokens,
		TaskCount:                task.TaskCount,
		CompletedCount:           task.CompletedCount,
		FailedCount:              task.FailedCount,
		RunSeconds:               task.RuntimeSeconds,
		SuccessRate:              ratePct(task.CompletedCount, task.CompletedCount+task.FailedCount),
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// 2. GET /api/dashboard/personal/trend
// ---------------------------------------------------------------------------

// PersonalTrendPoint is one token/cost trend bucket.
type PersonalTrendPoint struct {
	Time                     string `json:"time"`
	Tokens                   int64  `json:"tokens"`
	CostUSDTicks             int64  `json:"cost_usd_ticks"`
	UncostedInputTokens      int64  `json:"uncosted_input_tokens"`
	UncostedOutputTokens     int64  `json:"uncosted_output_tokens"`
	UncostedCacheReadTokens  int64  `json:"uncosted_cache_read_tokens"`
	UncostedCacheWriteTokens int64  `json:"uncosted_cache_write_tokens"`
	TaskCount                int64  `json:"task_count"`
}

// PersonalTrendResponse is the trend payload (API contract §2). The response
// always carries every series; `metric` only drives the client's initial tab.
type PersonalTrendResponse struct {
	Range  string              `json:"range"`
	Points []PersonalTrendPoint `json:"points"`
}

// GetPersonalUsageTrend serves the token / cost trend series.
func (h *Handler) GetPersonalUsageTrend(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolvePersonalScope(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	axis := personalBucketAxis(scope.rangeKey, scope.since.Time, scope.loc)
	counts := make(map[time.Time]dbPersonalTrendAgg, len(axis))
	if scope.rangeKey == personalRangeToday {
		rows, err := h.Queries.ListPersonalUsageTrendHourly(ctx, db.ListPersonalUsageTrendHourlyParams{
			Tz:          scope.tz,
			WorkspaceID: scope.workspaceID,
			UserID:      scope.userID,
			Since:       scope.since,
			Until:       scope.until,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load usage trend")
			return
		}
		for _, row := range rows {
			if row.Bucket.Valid {
				counts[hourKey(scope.loc, row.Bucket.Time)] = dbPersonalTrendAggFromUsage(row.InputTokens, row.OutputTokens, row.CacheReadTokens, row.CacheWriteTokens, row.CostUsdTicks, row.UncostedInputTokens, row.UncostedOutputTokens, row.UncostedCacheReadTokens, row.UncostedCacheWriteTokens, row.TaskCount)
			}
		}
	} else {
		rows, err := h.Queries.ListPersonalUsageTrendDaily(ctx, db.ListPersonalUsageTrendDailyParams{
			Tz:          scope.tz,
			WorkspaceID: scope.workspaceID,
			UserID:      scope.userID,
			Since:       scope.since,
			Until:       scope.until,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load usage trend")
			return
		}
		for _, row := range rows {
			if row.Date.Valid {
				counts[dayKey(scope.loc, row.Date.Time)] = dbPersonalTrendAggFromUsage(row.InputTokens, row.OutputTokens, row.CacheReadTokens, row.CacheWriteTokens, row.CostUsdTicks, row.UncostedInputTokens, row.UncostedOutputTokens, row.UncostedCacheReadTokens, row.UncostedCacheWriteTokens, row.TaskCount)
			}
		}
	}

	points := make([]PersonalTrendPoint, 0, len(axis))
	for _, bucket := range axis {
		agg := counts[bucket]
		points = append(points, PersonalTrendPoint{
			Time:                     bucket.UTC().Format(time.RFC3339),
			Tokens:                   agg.input + agg.output + agg.cacheRead + agg.cacheWrite,
			CostUSDTicks:             agg.costUsdTicks,
			UncostedInputTokens:      agg.uncostedInput,
			UncostedOutputTokens:     agg.uncostedOutput,
			UncostedCacheReadTokens:  agg.uncostedCacheRead,
			UncostedCacheWriteTokens: agg.uncostedCacheWrite,
			TaskCount:                agg.taskCount,
		})
	}
	writeJSON(w, http.StatusOK, PersonalTrendResponse{Range: scope.rangeKey, Points: points})
}

// ---------------------------------------------------------------------------
// 3. GET /api/dashboard/personal/models
// ---------------------------------------------------------------------------

// PersonalModelItem is one (provider, model) distribution row.
type PersonalModelItem struct {
	Provider                 string `json:"provider"`
	Model                    string `json:"model"`
	TokenInput               int64  `json:"token_input"`
	TokenOutput              int64  `json:"token_output"`
	TokenCacheRead           int64  `json:"token_cache_read"`
	TokenCacheWrite          int64  `json:"token_cache_write"`
	TokenTotal               int64  `json:"token_total"`
	CostUSDTicks             int64  `json:"cost_usd_ticks"`
	UncostedInputTokens      int64  `json:"uncosted_input_tokens"`
	UncostedOutputTokens     int64  `json:"uncosted_output_tokens"`
	UncostedCacheReadTokens  int64  `json:"uncosted_cache_read_tokens"`
	UncostedCacheWriteTokens int64  `json:"uncosted_cache_write_tokens"`
	TaskCount                int64  `json:"task_count"`
}

// PersonalModelsResponse is the model-distribution payload (API contract §3).
type PersonalModelsResponse struct {
	Range       string             `json:"range"`
	TotalTokens int64              `json:"total_tokens"`
	Items       []PersonalModelItem `json:"items"`
}

// GetPersonalUsageModels serves the per-model token / cost distribution.
func (h *Handler) GetPersonalUsageModels(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolvePersonalScope(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListPersonalUsageModels(r.Context(), db.ListPersonalUsageModelsParams{
		WorkspaceID: scope.workspaceID,
		UserID:      scope.userID,
		Since:       scope.since,
		Until:       scope.until,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load model distribution")
		return
	}

	items := make([]PersonalModelItem, 0, len(rows))
	var total int64
	for _, row := range rows {
		tokenTotal := row.InputTokens + row.OutputTokens + row.CacheReadTokens + row.CacheWriteTokens
		total += tokenTotal
		items = append(items, PersonalModelItem{
			Provider:                 row.Provider,
			Model:                    row.Model,
			TokenInput:               row.InputTokens,
			TokenOutput:              row.OutputTokens,
			TokenCacheRead:           row.CacheReadTokens,
			TokenCacheWrite:          row.CacheWriteTokens,
			TokenTotal:               tokenTotal,
			CostUSDTicks:             row.CostUsdTicks,
			UncostedInputTokens:      row.UncostedInputTokens,
			UncostedOutputTokens:     row.UncostedOutputTokens,
			UncostedCacheReadTokens:  row.UncostedCacheReadTokens,
			UncostedCacheWriteTokens: row.UncostedCacheWriteTokens,
			TaskCount:                row.TaskCount,
		})
	}
	writeJSON(w, http.StatusOK, PersonalModelsResponse{Range: scope.rangeKey, TotalTokens: total, Items: items})
}

// ---------------------------------------------------------------------------
// 4. GET /api/dashboard/personal/duration
// ---------------------------------------------------------------------------

// PersonalDurationBucket is one histogram bucket. max_seconds is null for the
// open-ended final bucket (API contract §4).
type PersonalDurationBucket struct {
	Bucket      string `json:"bucket"`
	Label       string `json:"label"`
	MinSeconds  int64  `json:"min_seconds"`
	MaxSeconds  *int64 `json:"max_seconds"`
	Count       int64  `json:"count"`
}

// PersonalDurationResponse is the runtime histogram payload (API contract §4).
// Always exactly 5 buckets; empty buckets carry count=0.
type PersonalDurationResponse struct {
	Range   string                  `json:"range"`
	Total   int64                   `json:"total"`
	Buckets []PersonalDurationBucket `json:"buckets"`
}

var personalDurationBuckets = []PersonalDurationBucket{
	{Bucket: "<1m", Label: "<1分钟", MinSeconds: 0, MaxSeconds: int64Ptr(60)},
	{Bucket: "1-5m", Label: "1-5分钟", MinSeconds: 60, MaxSeconds: int64Ptr(300)},
	{Bucket: "5-15m", Label: "5-15分钟", MinSeconds: 300, MaxSeconds: int64Ptr(900)},
	{Bucket: "15-30m", Label: "15-30分钟", MinSeconds: 900, MaxSeconds: int64Ptr(1800)},
	{Bucket: ">30m", Label: ">30分钟", MinSeconds: 1800, MaxSeconds: nil},
}

func int64Ptr(v int64) *int64 { return &v }

// GetPersonalUsageDuration serves the single-task runtime histogram.
func (h *Handler) GetPersonalUsageDuration(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolvePersonalScope(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListPersonalTaskDurations(r.Context(), db.ListPersonalTaskDurationsParams{
		WorkspaceID: scope.workspaceID,
		UserID:      scope.userID,
		Since:       scope.since,
		Until:       scope.until,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load duration distribution")
		return
	}

	counts := make([]int64, 6)
	for _, row := range rows {
		if row.Bucket >= 1 && int(row.Bucket) <= 5 {
			counts[row.Bucket] = row.TaskCount
		}
	}

	buckets := make([]PersonalDurationBucket, len(personalDurationBuckets))
	var total int64
	for i, def := range personalDurationBuckets {
		buckets[i] = def
		buckets[i].Count = counts[i+1]
		total += counts[i+1]
	}
	writeJSON(w, http.StatusOK, PersonalDurationResponse{Range: scope.rangeKey, Total: total, Buckets: buckets})
}

// ---------------------------------------------------------------------------
// 5. GET /api/dashboard/personal/runtime-trend
// ---------------------------------------------------------------------------

// PersonalRuntimeTrendPoint is one total-runtime bucket.
type PersonalRuntimeTrendPoint struct {
	Time       string `json:"time"`
	RunSeconds int64  `json:"run_seconds"`
	TaskCount  int64  `json:"task_count"`
}

// PersonalRuntimeTrendResponse is the total-runtime trend payload
// (API contract §5). Shares the exact bucket axis with /trend and /errors.
type PersonalRuntimeTrendResponse struct {
	Range  string                     `json:"range"`
	Points []PersonalRuntimeTrendPoint `json:"points"`
}

// GetPersonalRuntimeTrend serves the daily/weekly (or hourly for today)
// total-run-time series.
func (h *Handler) GetPersonalRuntimeTrend(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolvePersonalScope(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	axis := personalBucketAxis(scope.rangeKey, scope.since.Time, scope.loc)
	seconds := make(map[time.Time]int64, len(axis))
	counts := make(map[time.Time]int64, len(axis))
	if scope.rangeKey == personalRangeToday {
		rows, err := h.Queries.ListPersonalRuntimeTrendHourly(ctx, db.ListPersonalRuntimeTrendHourlyParams{
			Tz:          scope.tz,
			WorkspaceID: scope.workspaceID,
			UserID:      scope.userID,
			Since:       scope.since,
			Until:       scope.until,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load runtime trend")
			return
		}
		for _, row := range rows {
			if row.Bucket.Valid {
				key := hourKey(scope.loc, row.Bucket.Time)
				seconds[key] = row.TotalSeconds
				counts[key] = row.TaskCount
			}
		}
	} else {
		rows, err := h.Queries.ListPersonalRuntimeTrendDaily(ctx, db.ListPersonalRuntimeTrendDailyParams{
			Tz:          scope.tz,
			WorkspaceID: scope.workspaceID,
			UserID:      scope.userID,
			Since:       scope.since,
			Until:       scope.until,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load runtime trend")
			return
		}
		for _, row := range rows {
			if row.Date.Valid {
				key := dayKey(scope.loc, row.Date.Time)
				seconds[key] = row.TotalSeconds
				counts[key] = row.TaskCount
			}
		}
	}

	points := make([]PersonalRuntimeTrendPoint, 0, len(axis))
	for _, bucket := range axis {
		points = append(points, PersonalRuntimeTrendPoint{
			Time:       bucket.UTC().Format(time.RFC3339),
			RunSeconds: seconds[bucket],
			TaskCount:  counts[bucket],
		})
	}
	writeJSON(w, http.StatusOK, PersonalRuntimeTrendResponse{Range: scope.rangeKey, Points: points})
}

// ---------------------------------------------------------------------------
// 6. GET /api/dashboard/personal/errors
// ---------------------------------------------------------------------------

// PersonalErrorTrendPoint is one failure-rate trend bucket; FailureRate is
// null when the bucket has no terminal task (API contract §6).
type PersonalErrorTrendPoint struct {
	Time        string   `json:"time"`
	Completed   int64    `json:"completed"`
	Failed      int64    `json:"failed"`
	FailureRate *float64 `json:"failure_rate"`
}

// PersonalErrorType is one error-class bucket (taskfailure taxonomy).
type PersonalErrorType struct {
	Reason string  `json:"reason"`
	Count  int64   `json:"count"`
	Pct    float64 `json:"pct"`
}

// PersonalErrorsResponse is the failure-rate + error-type payload
// (API contract §6).
type PersonalErrorsResponse struct {
	Range          string                  `json:"range"`
	CompletedCount int64                   `json:"completed_count"`
	FailedCount    int64                   `json:"failed_count"`
	FailureRate    *float64                `json:"failure_rate"`
	Trend          []PersonalErrorTrendPoint `json:"trend"`
	Types          []PersonalErrorType     `json:"types"`
}

// GetPersonalUsageErrors serves the failure-rate trend and the error-type
// distribution for the current user.
func (h *Handler) GetPersonalUsageErrors(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolvePersonalScope(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	// Overall completed / failed and the error-type distribution.
	errorRows, err := h.Queries.ListPersonalErrors(ctx, db.ListPersonalErrorsParams{
		WorkspaceID: scope.workspaceID,
		UserID:      scope.userID,
		Since:       scope.since,
		Until:       scope.until,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load error distribution")
		return
	}

	var completedCount, failedCount int64
	types := make([]PersonalErrorType, 0, len(errorRows))
	for _, row := range errorRows {
		if row.FailureReason == "" {
			completedCount = row.TaskCount
			continue
		}
		failedCount += row.TaskCount
	}
	for _, row := range errorRows {
		if row.FailureReason == "" {
			continue
		}
		pct := 0.0
		if failedCount > 0 {
			pct = round1(float64(row.TaskCount) / float64(failedCount) * 100)
		}
		types = append(types, PersonalErrorType{
			Reason: row.FailureReason,
			Count:  row.TaskCount,
			Pct:    pct,
		})
	}

	// Failure-rate trend, zero-filled on the shared bucket axis.
	axis := personalBucketAxis(scope.rangeKey, scope.since.Time, scope.loc)
	type bucketCounts struct{ completed, failed int64 }
	trendMap := make(map[time.Time]bucketCounts, len(axis))
	if scope.rangeKey == personalRangeToday {
		rows, err := h.Queries.ListPersonalFailuresTrendHourly(ctx, db.ListPersonalFailuresTrendHourlyParams{
			Tz:          scope.tz,
			WorkspaceID: scope.workspaceID,
			UserID:      scope.userID,
			Since:       scope.since,
			Until:       scope.until,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load failure trend")
			return
		}
		for _, row := range rows {
			if row.Bucket.Valid {
				trendMap[hourKey(scope.loc, row.Bucket.Time)] = bucketCounts{completed: row.CompletedCount, failed: row.FailedCount}
			}
		}
	} else {
		rows, err := h.Queries.ListPersonalFailuresTrendDaily(ctx, db.ListPersonalFailuresTrendDailyParams{
			Tz:          scope.tz,
			WorkspaceID: scope.workspaceID,
			UserID:      scope.userID,
			Since:       scope.since,
			Until:       scope.until,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load failure trend")
			return
		}
		for _, row := range rows {
			if row.Date.Valid {
				trendMap[dayKey(scope.loc, row.Date.Time)] = bucketCounts{completed: row.CompletedCount, failed: row.FailedCount}
			}
		}
	}

	trend := make([]PersonalErrorTrendPoint, 0, len(axis))
	for _, bucket := range axis {
		bc := trendMap[bucket]
		trend = append(trend, PersonalErrorTrendPoint{
			Time:        bucket.UTC().Format(time.RFC3339),
			Completed:   bc.completed,
			Failed:      bc.failed,
			FailureRate: ratePct(bc.failed, bc.completed+bc.failed),
		})
	}

	writeJSON(w, http.StatusOK, PersonalErrorsResponse{
		Range:          scope.rangeKey,
		CompletedCount: completedCount,
		FailedCount:    failedCount,
		FailureRate:    ratePct(failedCount, completedCount+failedCount),
		Trend:          trend,
		Types:          types,
	})
}

// ---------------------------------------------------------------------------
// 7. GET /api/dashboard/personal/rank
// ---------------------------------------------------------------------------

// PersonalRankResponse is the leaderboard card payload (API contract §7).
// Security: it contains ONLY the caller's own numbers plus anonymous team
// aggregates — never another member's identity or totals.
type PersonalRankResponse struct {
	Range            string   `json:"range"`
	MyTokens         int64    `json:"my_tokens"`
	Rank             *int64   `json:"rank"`
	TotalMembers     int64    `json:"total_members"`
	ExceedPct        *float64 `json:"exceed_pct"`
	TeamAvgTokens    *int64   `json:"team_avg_tokens"`
	TeamMedianTokens *int64   `json:"team_median_tokens"`
}

// GetPersonalUsageRank serves the caller's position on the team leaderboard
// plus anonymous team aggregates. Rank is competition-style (ties share a
// position); exceed_pct is the share of members the caller beat.
func (h *Handler) GetPersonalUsageRank(w http.ResponseWriter, r *http.Request) {
	scope, ok := h.resolvePersonalScope(w, r)
	if !ok {
		return
	}
	members, err := h.Queries.ListPersonalRankMembers(r.Context(), db.ListPersonalRankMembersParams{
		WorkspaceID: scope.workspaceID,
		Since:       scope.since,
		Until:       scope.until,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load leaderboard")
		return
	}

	meID := uuidToString(scope.userID)
	total := int64(len(members))

	// The member list is sorted tokens DESC, user_id — but competition rank
	// must still be derived from counts, because ties share a position and a
	// 0-token member (terminal task, no usage rows) ranks after every
	// non-zero-token member.
	var myTokens int64
	callerPresent := false
	var above, below int64
	for _, m := range members {
		if uuidToString(m.UserID) == meID {
			callerPresent = true
			myTokens = m.Tokens
		}
		if m.Tokens > myTokens {
			above++
		}
		if m.Tokens < myTokens {
			below++
		}
	}

	resp := PersonalRankResponse{
		Range:        scope.rangeKey,
		MyTokens:     myTokens,
		TotalMembers: total,
	}
	if callerPresent {
		rank := above + 1
		resp.Rank = &rank
		if total <= 1 {
			zero := 0.0
			resp.ExceedPct = &zero
		} else {
			resp.ExceedPct = float64Ptr(round1(float64(below) / float64(total-1) * 100))
		}
	}
	if total > 0 {
		var sum int64
		tokens := make([]int64, 0, total)
		for _, m := range members {
			sum += m.Tokens
			tokens = append(tokens, m.Tokens)
		}
		avg := int64(math.Round(float64(sum) / float64(total)))
		resp.TeamAvgTokens = &avg

		sort.Slice(tokens, func(i, j int) bool { return tokens[i] < tokens[j] })
		median := tokens[len(tokens)/2]
		if len(tokens)%2 == 0 {
			median = int64(math.Round(float64(tokens[len(tokens)/2-1]+tokens[len(tokens)/2]) / 2))
		}
		resp.TeamMedianTokens = &median
	}

	writeJSON(w, http.StatusOK, resp)
}

func float64Ptr(v float64) *float64 { return &v }

// ---------------------------------------------------------------------------
// shared bucket-key helpers
// ---------------------------------------------------------------------------

// dbPersonalTrendAgg is the internal aggregate the token/cost trend maps into
// a point; fields mirror the sqlc row so daily and hourly feeds share one
// builder.
type dbPersonalTrendAgg struct {
	input, output, cacheRead, cacheWrite int64
	costUsdTicks                         int64
	uncostedInput, uncostedOutput        int64
	uncostedCacheRead, uncostedCacheWrite int64
	taskCount                            int64
}

func dbPersonalTrendAggFromUsage(input, output, cacheRead, cacheWrite, cost int64, uncostedInput, uncostedOutput, uncostedCacheRead, uncostedCacheWrite, taskCount int64) dbPersonalTrendAgg {
	return dbPersonalTrendAgg{
		input: input, output: output, cacheRead: cacheRead, cacheWrite: cacheWrite,
		costUsdTicks: cost,
		uncostedInput: uncostedInput, uncostedOutput: uncostedOutput,
		uncostedCacheRead: uncostedCacheRead, uncostedCacheWrite: uncostedCacheWrite,
		taskCount: taskCount,
	}
}

// hourKey reinterprets a local wall-clock hour (as returned by DATE_TRUNC in
// the viewer tz) as an instant in that tz and truncates to the hour.
func hourKey(loc *time.Location, wall time.Time) time.Time {
	local := time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), 0, 0, 0, loc)
	return local.Truncate(time.Hour)
}

// dayKey maps a calendar date (as returned by DATE(...) in the viewer tz) to
// the start of that local day.
func dayKey(loc *time.Location, d time.Time) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
}
