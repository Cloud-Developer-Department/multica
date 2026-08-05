package handler

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// Engineering analytics platform (CLO-239).
//
// Backs the `/{slug}/analytics` four-dashboard page:
//
//   Tab1 Adoption & Activity   A1 activity/summary   A2 activity/heatmap
//                              A3 activity/top-members  A4 adoption/summary
//                              A5 adoption/trend
//   Tab2 Agent Performance     B1 agents/funnel      B2 agents/performance
//                              B3 agents/top         B4 skills/overview
//                              B5 collaboration/summary B6 collaboration/blockers
//   Tab3 Git Contributions     G1 git/eloc           G2 git/quality
//                              G3 git/repos          G4 git/prs
//   Tab4 DORA                  D1 dora/lead-time     D2 dora/deployments
//   Identity / departments     L1 identity/lifecycle L2 identity/departments
//
// Contract: API-CLO-228 v2.0 (参见 deliverables/engineering-analytics-2026-08-05).
// Conventions enforced here:
//   * Window: ?days= (1-365, default 30) natural-day window sliced under
//     ?tz= (IANA, fallback user tz → UTC). `since` anchors at local midnight
//     of (today - (days-1)).
//   * Ratios are 0-1 floats (frontend ×100 for %), null when the denominator
//     is 0 — the frontend renders `-`.
//   * G/D/L endpoints always return a top-level `source_status` (§0.7):
//     ready=false with a human reason when the external source isn't
//     connected, never a 4xx/5xx.
//   * `department_id` is only honoured by Tab1/Tab2 endpoints; passing it
//     when no department data is configured returns 400 (E18).
//   * `project_id` is honoured by Tab1/Tab2 issue-scoped aggregates; malformed
//     values return 400.
// ---------------------------------------------------------------------------

// analyticsScope carries the resolved workspace / window / filter context for
// every analytics endpoint.
type analyticsScope struct {
	workspaceID  pgtype.UUID
	tz           string
	loc          *time.Location
	windowStart  time.Time // local midnight of the first day in the window
	windowEnd    time.Time // local midnight of today
	windowStartS string    // "YYYY-MM-DD"
	windowEndS   string    // "YYYY-MM-DD"
	since        pgtype.Timestamptz
	department   string // "" = all departments
	projectID    pgtype.UUID
}

// resolveAnalyticsScope resolves the shared request context for analytics
// endpoints: workspace membership, ?days=?tz= window, ?project_id and
// ?department_id filters. On auth / membership / bad-filter it writes the
// error response and returns ok=false.
func (h *Handler) resolveAnalyticsScope(w http.ResponseWriter, r *http.Request, allowDepartment bool) (analyticsScope, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return analyticsScope{}, false
	}
	wsUUID := parseUUID(workspaceID)

	tz := h.resolveViewingTZ(r)
	loc, err := time.LoadLocation(tz)
	if err != nil || loc == nil {
		loc = time.UTC
	}

	days := parseAnalyticsDays(r)
	now := time.Now().In(loc)
	todayMidnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	start := todayMidnight.AddDate(0, 0, -(days - 1))

	projectID, ok := parseProjectIDParam(w, r)
	if !ok {
		return analyticsScope{}, false
	}

	department := ""
	if dept := r.URL.Query().Get("department_id"); dept != "" {
		if !allowDepartment {
			writeError(w, http.StatusBadRequest, "department data not configured")
			return analyticsScope{}, false
		}
		count, err := h.Queries.CountAnalyticsMembersWithDepartment(r.Context(), wsUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load department configuration")
			return analyticsScope{}, false
		}
		if count == 0 {
			writeError(w, http.StatusBadRequest, "department data not configured")
			return analyticsScope{}, false
		}
		department = dept
	}

	return analyticsScope{
		workspaceID:  wsUUID,
		tz:           tz,
		loc:          loc,
		windowStart:  start,
		windowEnd:    todayMidnight,
		windowStartS: start.Format(time.DateOnly),
		windowEndS:   todayMidnight.Format(time.DateOnly),
		since:        pgtype.Timestamptz{Time: start, Valid: true},
		department:   department,
		projectID:    projectID,
	}, true
}

// parseAnalyticsDays reads ?days= (1-365), default 30; invalid falls back.
func parseAnalyticsDays(r *http.Request) int {
	if d := r.URL.Query().Get("days"); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n >= 1 && n <= 365 {
			return n
		}
	}
	return 30
}

// parseAnalyticsLimit reads ?limit= with a default and a hard cap.
func parseAnalyticsLimit(r *http.Request, def, max int) int {
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			if n > max {
				return max
			}
			return n
		}
	}
	return def
}

// ratio returns a/denominator as a 0-1 float, or nil when denominator <= 0.
func ratio(a, b int64) *float64 {
	if b <= 0 {
		return nil
	}
	v := float64(a) / float64(b)
	return &v
}

// round1p snaps a value to one decimal place (mirror of round1 in
// personal_dashboard.go).
func round1p(v float64) float64 {
	return math.Round(v*10) / 10
}

// ---------------------------------------------------------------------------
// Tab1 A1: activity summary
// ---------------------------------------------------------------------------

// AnalyticsWindow is the natural-day window returned by A1.
type AnalyticsWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// AnalyticsActivitySummary is A1's payload.
type AnalyticsActivitySummary struct {
	Window               AnalyticsWindow `json:"window"`
	TotalMembers         int64           `json:"total_members"`
	ActiveMembers        int64           `json:"active_members"`
	ActiveDaysAvg        *float64        `json:"active_days_avg"`
	DAU                  int64           `json:"dau"`
	MAU                  int64           `json:"mau"`
	DAUMauRatio          *float64        `json:"dau_mau_ratio"`
	ActiveUserRatio      *float64        `json:"active_user_ratio"`
	TotalIssues          int64           `json:"total_issues"`
	PerCapitaIssueVolume *float64        `json:"per_capita_issue_volume"`
}

// GetAnalyticsActivitySummary serves GET /api/analytics/activity/summary.
func (h *Handler) GetAnalyticsActivitySummary(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.resolveAnalyticsScope(w, r, true)
	if !ok {
		return
	}
	ctx := r.Context()

	members, err := h.Queries.CountAnalyticsMembers(ctx, db.CountAnalyticsMembersParams{
		WorkspaceID: sc.workspaceID,
		Column2:     sc.department,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load activity summary")
		return
	}

	activity, err := h.Queries.ListAnalyticsMemberActivityDays(ctx, db.ListAnalyticsMemberActivityDaysParams{
		WorkspaceID: sc.workspaceID,
		Column2:     sc.tz,
		CreatedAt:   sc.since,
		Column4:     sc.department,
		Column5:     sc.projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load activity summary")
		return
	}

	issueCounts, err := h.Queries.ListAnalyticsMemberIssueCounts(ctx, db.ListAnalyticsMemberIssueCountsParams{
		WorkspaceID: sc.workspaceID,
		CreatedAt:   sc.since,
		Column3:     sc.department,
		Column4:     sc.projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load activity summary")
		return
	}

	totalIssues, err := h.Queries.CountAnalyticsIssuesCreated(ctx, db.CountAnalyticsIssuesCreatedParams{
		WorkspaceID: sc.workspaceID,
		CreatedAt:   sc.since,
		Column3:     sc.department,
		Column4:     sc.projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load activity summary")
		return
	}

	// DAU / MAU use fixed windows (today / trailing 30d) regardless of ?days=.
	todayStart := pgtype.Timestamptz{Time: sc.windowEnd, Valid: true}
	mauStart := pgtype.Timestamptz{Time: sc.windowEnd.AddDate(0, 0, -29), Valid: true}
	dau, err := h.Queries.CountAnalyticsMembersActive(ctx, db.CountAnalyticsMembersActiveParams{
		WorkspaceID: sc.workspaceID,
		CreatedAt:   todayStart,
		Column3:     sc.department,
		Column4:     sc.projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load activity summary")
		return
	}
	mau, err := h.Queries.CountAnalyticsMembersActive(ctx, db.CountAnalyticsMembersActiveParams{
		WorkspaceID: sc.workspaceID,
		CreatedAt:   mauStart,
		Column3:     sc.department,
		Column4:     sc.projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load activity summary")
		return
	}

	activeMembers := int64(len(activity))
	issueCountByUser := make(map[string]int64, len(issueCounts))
	for _, ic := range issueCounts {
		issueCountByUser[uuidToString(ic.UserID)] = ic.IssueCount
	}

	var activeDaysSum int64
	for _, a := range activity {
		activeDaysSum += a.ActiveDays
	}

	var activeDaysAvg *float64
	if activeMembers > 0 {
		v := round1p(float64(activeDaysSum) / float64(activeMembers))
		activeDaysAvg = &v
	}

	resp := AnalyticsActivitySummary{
		Window:               AnalyticsWindow{Start: sc.windowStartS, End: sc.windowEndS},
		TotalMembers:         members,
		ActiveMembers:        activeMembers,
		ActiveDaysAvg:        activeDaysAvg,
		DAU:                  dau,
		MAU:                  mau,
		DAUMauRatio:          ratio(dau, mau),
		ActiveUserRatio:      ratio(dau, members),
		TotalIssues:          totalIssues,
		PerCapitaIssueVolume: ratio(totalIssues, activeMembers),
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// Tab1 A2: activity heatmap
// ---------------------------------------------------------------------------

// AnalyticsHeatmapHour is one hour cell.
type AnalyticsHeatmapHour struct {
	Hour  int    `json:"hour"`
	Value *int64 `json:"value"`
}

// AnalyticsHeatmapDay is one day row of the heatmap.
type AnalyticsHeatmapDay struct {
	Date  string                 `json:"date"`
	Hours []AnalyticsHeatmapHour `json:"hours"`
}

// AnalyticsActivityHeatmap is A2's payload.
type AnalyticsActivityHeatmap struct {
	Metric   string                `json:"metric"`
	Days     []AnalyticsHeatmapDay `json:"days"`
	MaxValue int64                 `json:"max_value"`
}

// GetAnalyticsActivityHeatmap serves GET /api/analytics/activity/heatmap.
func (h *Handler) GetAnalyticsActivityHeatmap(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.resolveAnalyticsScope(w, r, true)
	if !ok {
		return
	}
	metric := r.URL.Query().Get("metric")
	if metric == "" {
		metric = "activity_events"
	}
	ctx := r.Context()

	type heatmapCell struct {
		day   pgtype.Date
		hour  int32
		value int64
	}
	var cells []heatmapCell

	switch metric {
	case "issue_volume":
		rows, err := h.Queries.ListAnalyticsIssueVolumeHeatmap(ctx, db.ListAnalyticsIssueVolumeHeatmapParams{
			WorkspaceID: sc.workspaceID,
			Column2:     sc.tz,
			CreatedAt:   sc.since,
			Column4:     sc.department,
			Column5:     sc.projectID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load activity heatmap")
			return
		}
		for _, c := range rows {
			cells = append(cells, heatmapCell{day: c.Day, hour: c.Hour, value: c.Value})
		}
	case "active_days":
		rows, err := h.Queries.ListAnalyticsActiveDaysHeatmap(ctx, db.ListAnalyticsActiveDaysHeatmapParams{
			WorkspaceID: sc.workspaceID,
			Column2:     sc.tz,
			CreatedAt:   sc.since,
			Column4:     sc.department,
			Column5:     sc.projectID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load activity heatmap")
			return
		}
		for _, c := range rows {
			cells = append(cells, heatmapCell{day: c.Day, hour: c.Hour, value: c.Value})
		}
	default:
		metric = "activity_events"
		rows, err := h.Queries.ListAnalyticsActivityHeatmap(ctx, db.ListAnalyticsActivityHeatmapParams{
			WorkspaceID: sc.workspaceID,
			Column2:     sc.tz,
			CreatedAt:   sc.since,
			Column4:     sc.department,
			Column5:     sc.projectID,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load activity heatmap")
			return
		}
		for _, c := range rows {
			cells = append(cells, heatmapCell{day: c.Day, hour: c.Hour, value: c.Value})
		}
	}

	// Fold cells into per-day hour buckets; only days with data are emitted.
	dayIndex := make(map[string]int)
	var days []AnalyticsHeatmapDay
	var maxValue int64
	for _, c := range cells {
		dateStr := c.day.Time.Format(time.DateOnly)
		idx, ok := dayIndex[dateStr]
		if !ok {
			idx = len(days)
			dayIndex[dateStr] = idx
			days = append(days, AnalyticsHeatmapDay{Date: dateStr, Hours: []AnalyticsHeatmapHour{}})
		}
		v := c.value
		days[idx].Hours = append(days[idx].Hours, AnalyticsHeatmapHour{Hour: int(c.hour), Value: &v})
		if c.value > maxValue {
			maxValue = c.value
		}
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	for i := range days {
		sort.Slice(days[i].Hours, func(a, b int) bool { return days[i].Hours[a].Hour < days[i].Hours[b].Hour })
	}

	writeJSON(w, http.StatusOK, AnalyticsActivityHeatmap{
		Metric:   metric,
		Days:     days,
		MaxValue: maxValue,
	})
}

// ---------------------------------------------------------------------------
// Tab1 A3: top members
// ---------------------------------------------------------------------------

// AnalyticsTopMember is one member activity row.
type AnalyticsTopMember struct {
	MemberID     string  `json:"member_id"`
	Name         string  `json:"name"`
	ActiveDays   int64   `json:"active_days"`
	IssueCount   int64   `json:"issue_count"`
	LastActiveAt *string `json:"last_active_at"`
}

// GetAnalyticsActivityTopMembers serves GET /api/analytics/activity/top-members.
func (h *Handler) GetAnalyticsActivityTopMembers(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.resolveAnalyticsScope(w, r, true)
	if !ok {
		return
	}
	limit := parseAnalyticsLimit(r, 10, 50)
	ctx := r.Context()

	activity, err := h.Queries.ListAnalyticsMemberActivityDays(ctx, db.ListAnalyticsMemberActivityDaysParams{
		WorkspaceID: sc.workspaceID,
		Column2:     sc.tz,
		CreatedAt:   sc.since,
		Column4:     sc.department,
		Column5:     sc.projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load top members")
		return
	}
	issueCounts, err := h.Queries.ListAnalyticsMemberIssueCounts(ctx, db.ListAnalyticsMemberIssueCountsParams{
		WorkspaceID: sc.workspaceID,
		CreatedAt:   sc.since,
		Column3:     sc.department,
		Column4:     sc.projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load top members")
		return
	}
	issueByUser := make(map[string]int64, len(issueCounts))
	for _, ic := range issueCounts {
		issueByUser[uuidToString(ic.UserID)] = ic.IssueCount
	}

	items := make([]AnalyticsTopMember, 0, len(activity))
	for _, a := range activity {
		items = append(items, AnalyticsTopMember{
			MemberID:     uuidToString(a.UserID),
			Name:         a.MemberName,
			ActiveDays:   a.ActiveDays,
			IssueCount:   issueByUser[uuidToString(a.UserID)],
			LastActiveAt: timestampToPtr(a.LastActiveAt),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ActiveDays != items[j].ActiveDays {
			return items[i].ActiveDays > items[j].ActiveDays
		}
		return items[i].IssueCount > items[j].IssueCount
	})
	if len(items) > limit {
		items = items[:limit]
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// ---------------------------------------------------------------------------
// Tab1 A4: adoption summary
// ---------------------------------------------------------------------------

// AnalyticsAdoptionSummary is A4's payload.
type AnalyticsAdoptionSummary struct {
	TotalIssues         int64    `json:"total_issues"`
	AgentAssignedIssues int64    `json:"agent_assigned_issues"`
	AssignmentRatio     *float64 `json:"assignment_ratio"`
	AgentCoveredIssues  int64    `json:"agent_covered_issues"`
	CoverageRatio       *float64 `json:"coverage_ratio"`
}

// GetAnalyticsAdoptionSummary serves GET /api/analytics/adoption/summary.
func (h *Handler) GetAnalyticsAdoptionSummary(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.resolveAnalyticsScope(w, r, true)
	if !ok {
		return
	}
	ctx := r.Context()

	summary, err := h.Queries.GetAnalyticsAdoptionSummary(ctx, db.GetAnalyticsAdoptionSummaryParams{
		WorkspaceID: sc.workspaceID,
		UpdatedAt:   sc.since,
		Column3:     sc.department,
		Column4:     sc.projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load adoption summary")
		return
	}
	coverage, err := h.Queries.GetAnalyticsAdoptionCoverage(ctx, db.GetAnalyticsAdoptionCoverageParams{
		WorkspaceID: sc.workspaceID,
		UpdatedAt:   sc.since,
		Column3:     sc.department,
		Column4:     sc.projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load adoption summary")
		return
	}

	writeJSON(w, http.StatusOK, AnalyticsAdoptionSummary{
		TotalIssues:         summary.TotalIssues,
		AgentAssignedIssues: summary.AgentAssigned,
		AssignmentRatio:     ratio(summary.AgentAssigned, summary.TotalIssues),
		AgentCoveredIssues:  coverage,
		CoverageRatio:       ratio(coverage, summary.TotalIssues),
	})
}

// ---------------------------------------------------------------------------
// Tab1 A5: adoption trend
// ---------------------------------------------------------------------------

// AnalyticsAdoptionTrendPoint is one day of A5.
type AnalyticsAdoptionTrendPoint struct {
	Date            string   `json:"date"`
	TotalIssues     int64    `json:"total_issues"`
	AssignmentRatio *float64 `json:"assignment_ratio"`
	CoverageRatio   *float64 `json:"coverage_ratio"`
}

// GetAnalyticsAdoptionTrend serves GET /api/analytics/adoption/trend.
func (h *Handler) GetAnalyticsAdoptionTrend(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.resolveAnalyticsScope(w, r, true)
	if !ok {
		return
	}
	ctx := r.Context()

	trend, err := h.Queries.ListAnalyticsAdoptionTrend(ctx, db.ListAnalyticsAdoptionTrendParams{
		WorkspaceID: sc.workspaceID,
		Column2:     sc.tz,
		UpdatedAt:   sc.since,
		Column4:     sc.department,
		Column5:     sc.projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load adoption trend")
		return
	}
	coverage, err := h.Queries.ListAnalyticsAdoptionTrendCoverage(ctx, db.ListAnalyticsAdoptionTrendCoverageParams{
		WorkspaceID: sc.workspaceID,
		Column2:     sc.tz,
		UpdatedAt:   sc.since,
		Column4:     sc.department,
		Column5:     sc.projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load adoption trend")
		return
	}
	coveredByDay := make(map[string]int64, len(coverage))
	for _, c := range coverage {
		coveredByDay[c.Day.Time.Format(time.DateOnly)] = c.Covered
	}

	points := make([]AnalyticsAdoptionTrendPoint, 0, len(trend))
	for _, t := range trend {
		dateStr := t.Day.Time.Format(time.DateOnly)
		points = append(points, AnalyticsAdoptionTrendPoint{
			Date:            dateStr,
			TotalIssues:     t.TotalIssues,
			AssignmentRatio: ratio(t.AgentAssigned, t.TotalIssues),
			CoverageRatio:   ratio(coveredByDay[dateStr], t.TotalIssues),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"points": points})
}

// ---------------------------------------------------------------------------
// Tab2 B1: agent funnel
// ---------------------------------------------------------------------------

// AnalyticsAgentFunnel is B1's payload.
type AnalyticsAgentFunnel struct {
	AssignCount        int64    `json:"assign_count"`
	IssueAssignedCount int64    `json:"issue_assigned_count"`
	ExecuteCount       int64    `json:"execute_count"`
	ExecuteRatio       *float64 `json:"execute_ratio"`
	MergedCount        *int64   `json:"merged_count"`
	MergedRatio        *float64 `json:"merged_ratio"`
}

// GetAnalyticsAgentsFunnel serves GET /api/analytics/agents/funnel.
func (h *Handler) GetAnalyticsAgentsFunnel(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.resolveAnalyticsScope(w, r, true)
	if !ok {
		return
	}
	ctx := r.Context()

	assign, err := h.Queries.CountAnalyticsAssign(ctx, db.CountAnalyticsAssignParams{
		WorkspaceID: sc.workspaceID,
		CreatedAt:   sc.since,
		Column3:     sc.department,
		Column4:     sc.projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent funnel")
		return
	}
	execute, err := h.Queries.CountAnalyticsExecute(ctx, db.CountAnalyticsExecuteParams{
		WorkspaceID: sc.workspaceID,
		StartedAt:   sc.since,
		Column3:     sc.department,
		Column4:     sc.projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent funnel")
		return
	}
	issueAssigned, err := h.Queries.CountAnalyticsIssueAssignedToAgent(ctx, db.CountAnalyticsIssueAssignedToAgentParams{
		WorkspaceID: sc.workspaceID,
		UpdatedAt:   sc.since,
		Column3:     sc.department,
		Column4:     sc.projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent funnel")
		return
	}

	// Merged is null until a VCS connection exists (guide state, E10).
	var mergedCount *int64
	var mergedRatio *float64
	vcsCount, err := h.Queries.CountAnalyticsVCSConnections(ctx, sc.workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent funnel")
		return
	}
	if vcsCount > 0 {
		merged, err := h.Queries.CountAnalyticsMergedIssues(ctx, db.CountAnalyticsMergedIssuesParams{
			WorkspaceID: sc.workspaceID,
			MergedAt:    sc.since,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load agent funnel")
			return
		}
		mergedCount = &merged
		mergedRatio = ratio(merged, execute)
	}

	writeJSON(w, http.StatusOK, AnalyticsAgentFunnel{
		AssignCount:        assign,
		IssueAssignedCount: issueAssigned,
		ExecuteCount:       execute,
		ExecuteRatio:       ratio(execute, assign),
		MergedCount:        mergedCount,
		MergedRatio:        mergedRatio,
	})
}

// ---------------------------------------------------------------------------
// Tab2 B2: agent performance
// ---------------------------------------------------------------------------

// AnalyticsFailureClass is one failure-reason bucket.
type AnalyticsFailureClass struct {
	Reason string `json:"reason"`
	Count  int64  `json:"count"`
}

// AnalyticsAgentPerformance is B2's payload.
type AnalyticsAgentPerformance struct {
	TerminalCount      int64                   `json:"terminal_count"`
	CompletedCount     int64                   `json:"completed_count"`
	FailedCount        int64                   `json:"failed_count"`
	SuccessRate        *float64                `json:"success_rate"`
	AvgDurationSeconds *float64                `json:"avg_duration_seconds"`
	P50DurationSeconds *float64                `json:"p50_duration_seconds"`
	P95DurationSeconds *float64                `json:"p95_duration_seconds"`
	FailureClasses     []AnalyticsFailureClass `json:"failure_classes"`
}

// GetAnalyticsAgentsPerformance serves GET /api/analytics/agents/performance.
func (h *Handler) GetAnalyticsAgentsPerformance(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.resolveAnalyticsScope(w, r, false)
	if !ok {
		return
	}
	ctx := r.Context()

	perf, err := h.Queries.GetAnalyticsAgentPerformance(ctx, db.GetAnalyticsAgentPerformanceParams{
		WorkspaceID: sc.workspaceID,
		CompletedAt: sc.since,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent performance")
		return
	}
	durations, err := h.Queries.ListAnalyticsTaskDurations(ctx, db.ListAnalyticsTaskDurationsParams{
		WorkspaceID: sc.workspaceID,
		CompletedAt: sc.since,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent performance")
		return
	}
	failures, err := h.Queries.ListAnalyticsFailureClasses(ctx, db.ListAnalyticsFailureClassesParams{
		WorkspaceID: sc.workspaceID,
		CompletedAt: sc.since,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent performance")
		return
	}

	failureClasses := make([]AnalyticsFailureClass, 0, len(failures))
	for _, f := range failures {
		failureClasses = append(failureClasses, AnalyticsFailureClass{Reason: f.Reason, Count: f.Count})
	}

	avg, p50, p95 := percentileDurations(durations)

	resp := AnalyticsAgentPerformance{
		TerminalCount:      perf.TerminalCount,
		CompletedCount:     perf.CompletedCount,
		FailedCount:        perf.FailedCount,
		SuccessRate:        ratio(perf.CompletedCount, perf.TerminalCount),
		AvgDurationSeconds: avg,
		P50DurationSeconds: p50,
		P95DurationSeconds: p95,
		FailureClasses:     failureClasses,
	}
	writeJSON(w, http.StatusOK, resp)
}

// percentileDurations computes avg / p50 / p95 of a duration list in seconds.
// Returns nil pointers when there are no samples.
func percentileDurations(durations []float64) (*float64, *float64, *float64) {
	if len(durations) == 0 {
		return nil, nil, nil
	}
	sorted := make([]float64, len(durations))
	copy(sorted, durations)
	sort.Float64s(sorted)

	var sum float64
	for _, d := range sorted {
		sum += d
	}
	avg := round1p(sum / float64(len(sorted)))
	p50 := round1p(percentile(sorted, 0.50))
	p95 := round1p(percentile(sorted, 0.95))
	return &avg, &p50, &p95
}

// percentile returns the linear-interpolated percentile (0-1) of a sorted
// slice. Mirror of the SQL percentile_cont used elsewhere.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := p * float64(len(sorted)-1)
	lo := int(rank)
	hi := lo + 1
	if hi >= len(sorted) {
		hi = len(sorted) - 1
	}
	frac := rank - float64(lo)
	return sorted[lo] + (sorted[hi]-sorted[lo])*frac
}

// ---------------------------------------------------------------------------
// Tab2 B3: top agents
// ---------------------------------------------------------------------------

// AnalyticsTopAgent is one agent's performance row.
type AnalyticsTopAgent struct {
	AgentID            string   `json:"agent_id"`
	Name               string   `json:"name"`
	CompletedCount     int64    `json:"completed_count"`
	FailedCount        int64    `json:"failed_count"`
	SuccessRate        *float64 `json:"success_rate"`
	AvgDurationSeconds *float64 `json:"avg_duration_seconds"`
}

// GetAnalyticsAgentsTop serves GET /api/analytics/agents/top.
func (h *Handler) GetAnalyticsAgentsTop(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.resolveAnalyticsScope(w, r, false)
	if !ok {
		return
	}
	limit := parseAnalyticsLimit(r, 10, 50)
	ctx := r.Context()

	rows, err := h.Queries.ListAnalyticsAgentPerformance(ctx, db.ListAnalyticsAgentPerformanceParams{
		WorkspaceID: sc.workspaceID,
		CompletedAt: sc.since,
		Limit:       int32(limit),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load top agents")
		return
	}

	items := make([]AnalyticsTopAgent, 0, len(rows))
	for _, a := range rows {
		var avg *float64
		if a.AvgDurationSeconds > 0 {
			v := round1p(a.AvgDurationSeconds)
			avg = &v
		}
		items = append(items, AnalyticsTopAgent{
			AgentID:            uuidToString(a.AgentID),
			Name:               a.AgentName,
			CompletedCount:     a.CompletedCount,
			FailedCount:        a.FailedCount,
			SuccessRate:        ratio(a.CompletedCount, a.CompletedCount+a.FailedCount),
			AvgDurationSeconds: avg,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// ---------------------------------------------------------------------------
// Tab2 B4: skills overview
// ---------------------------------------------------------------------------

// AnalyticsSkillAccumulationPoint is one month of new-skill accumulation.
type AnalyticsSkillAccumulationPoint struct {
	Bucket string `json:"bucket"`
	Count  int64  `json:"count"`
}

// AnalyticsTopReusedSkill is one top-reused skill.
type AnalyticsTopReusedSkill struct {
	SkillID     string `json:"skill_id"`
	Name        string `json:"name"`
	ReuseCount  int64  `json:"reuse_count"`
	BoundAgents int64  `json:"bound_agents"`
}

// AnalyticsSkillsOverview is B4's payload.
type AnalyticsSkillsOverview struct {
	TotalSkills  int64                             `json:"total_skills"`
	NewSkills    int64                             `json:"new_skills"`
	Accumulation []AnalyticsSkillAccumulationPoint `json:"accumulation"`
	TopReused    []AnalyticsTopReusedSkill         `json:"top_reused"`
}

// GetAnalyticsSkillsOverview serves GET /api/analytics/skills/overview.
func (h *Handler) GetAnalyticsSkillsOverview(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.resolveAnalyticsScope(w, r, false)
	if !ok {
		return
	}
	ctx := r.Context()

	summary, err := h.Queries.GetAnalyticsSkillsSummary(ctx, db.GetAnalyticsSkillsSummaryParams{
		WorkspaceID: sc.workspaceID,
		CreatedAt:   sc.since,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load skills overview")
		return
	}
	acc, err := h.Queries.ListAnalyticsSkillAccumulation(ctx, db.ListAnalyticsSkillAccumulationParams{
		WorkspaceID: sc.workspaceID,
		Column2:     sc.tz,
		CreatedAt:   sc.since,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load skills overview")
		return
	}
	top, err := h.Queries.ListAnalyticsTopReusedSkills(ctx, db.ListAnalyticsTopReusedSkillsParams{
		WorkspaceID: sc.workspaceID,
		CompletedAt: sc.since,
		Limit:       10,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load skills overview")
		return
	}

	accumulation := make([]AnalyticsSkillAccumulationPoint, 0, len(acc))
	for _, a := range acc {
		accumulation = append(accumulation, AnalyticsSkillAccumulationPoint{Bucket: a.Bucket, Count: a.Count})
	}
	topReused := make([]AnalyticsTopReusedSkill, 0, len(top))
	for _, t := range top {
		topReused = append(topReused, AnalyticsTopReusedSkill{
			SkillID:     uuidToString(t.SkillID),
			Name:        t.SkillName,
			ReuseCount:  t.ReuseCount,
			BoundAgents: t.BoundAgents,
		})
	}

	writeJSON(w, http.StatusOK, AnalyticsSkillsOverview{
		TotalSkills:  summary.TotalSkills,
		NewSkills:    summary.NewSkills,
		Accumulation: accumulation,
		TopReused:    topReused,
	})
}

// ---------------------------------------------------------------------------
// Tab2 B5: collaboration summary
// ---------------------------------------------------------------------------

// AnalyticsCollaborationSummary is B5's payload.
type AnalyticsCollaborationSummary struct {
	CollabIssueCount  int64    `json:"collab_issue_count"`
	TotalIssues       int64    `json:"total_issues"`
	CollabIssueRatio  *float64 `json:"collab_issue_ratio"`
	InteractionFreq   *float64 `json:"interaction_frequency"`
	BlockerAvgSeconds *float64 `json:"blocker_avg_seconds"`
	BlockerP50Seconds *float64 `json:"blocker_p50_seconds"`
	BlockerP95Seconds *float64 `json:"blocker_p95_seconds"`
	BlockerOpenCount  int64    `json:"blocker_open_count"`
}

// GetAnalyticsCollaborationSummary serves GET /api/analytics/collaboration/summary.
func (h *Handler) GetAnalyticsCollaborationSummary(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.resolveAnalyticsScope(w, r, false)
	if !ok {
		return
	}
	ctx := r.Context()

	collab, err := h.Queries.GetAnalyticsCollabSummary(ctx, db.GetAnalyticsCollabSummaryParams{
		WorkspaceID: sc.workspaceID,
		CreatedAt:   sc.since,
		Column3:     sc.department,
		Column4:     sc.projectID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load collaboration summary")
		return
	}

	blockerDurations, blockerOpen, err := h.blockerStats(ctx, sc)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load collaboration summary")
		return
	}
	avg, p50, p95 := percentileDurations(blockerDurations)

	resp := AnalyticsCollaborationSummary{
		CollabIssueCount:  collab.CollabIssueCount,
		TotalIssues:       collab.TotalIssues,
		CollabIssueRatio:  ratio(collab.CollabIssueCount, collab.TotalIssues),
		InteractionFreq:   ratio(collab.CollabCommentTotal, collab.CollabIssueCount),
		BlockerAvgSeconds: avg,
		BlockerP50Seconds: p50,
		BlockerP95Seconds: p95,
		BlockerOpenCount:  blockerOpen,
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// Tab2 B6: collaboration blockers
// ---------------------------------------------------------------------------

// AnalyticsBlocker is one blocker detail row.
type AnalyticsBlocker struct {
	IssueID         string   `json:"issue_id"`
	IssueTitle      string   `json:"issue_title"`
	BlockedAt       string   `json:"blocked_at"`
	ResolvedAt      *string  `json:"resolved_at"`
	ResponseSeconds *float64 `json:"response_seconds"`
	Status          string   `json:"status"`
}

// GetAnalyticsCollaborationBlockers serves GET /api/analytics/collaboration/blockers.
func (h *Handler) GetAnalyticsCollaborationBlockers(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.resolveAnalyticsScope(w, r, false)
	if !ok {
		return
	}
	limit := parseAnalyticsLimit(r, 20, 100)
	ctx := r.Context()

	blockers, err := h.Queries.ListAnalyticsBlockers(ctx, db.ListAnalyticsBlockersParams{
		WorkspaceID: sc.workspaceID,
		CreatedAt:   sc.since,
		Limit:       int32(limit),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load blockers")
		return
	}

	items := make([]AnalyticsBlocker, 0, len(blockers))
	for _, b := range blockers {
		blockedAt := util.TimestampToString(b.BlockedAt)
		item := AnalyticsBlocker{
			IssueID:    uuidToString(b.IssueID),
			IssueTitle: b.IssueTitle,
			BlockedAt:  blockedAt,
			Status:     "open",
		}
		resolved, err := h.Queries.GetAnalyticsBlockerResolution(ctx, db.GetAnalyticsBlockerResolutionParams{
			IssueID:   b.IssueID,
			CreatedAt: b.BlockedAt,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load blockers")
			return
		}
		if resolved.Valid {
			item.Status = "resolved"
			item.ResolvedAt = timestampToPtr(resolved)
			secs := resolved.Time.Sub(b.BlockedAt.Time).Seconds()
			item.ResponseSeconds = &secs
		}
		items = append(items, item)
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// blockerStats returns the resolved blocker durations (seconds) and the count
// of still-open blockers across the window.
func (h *Handler) blockerStats(ctx context.Context, sc analyticsScope) ([]float64, int64, error) {
	blockers, err := h.Queries.ListAnalyticsBlockers(ctx, db.ListAnalyticsBlockersParams{
		WorkspaceID: sc.workspaceID,
		CreatedAt:   sc.since,
		Limit:       100,
	})
	if err != nil {
		return nil, 0, err
	}
	var durations []float64
	var open int64
	for _, b := range blockers {
		resolved, err := h.Queries.GetAnalyticsBlockerResolution(ctx, db.GetAnalyticsBlockerResolutionParams{
			IssueID:   b.IssueID,
			CreatedAt: b.BlockedAt,
		})
		if err != nil {
			return nil, 0, err
		}
		if resolved.Valid {
			durations = append(durations, resolved.Time.Sub(b.BlockedAt.Time).Seconds())
		} else {
			open++
		}
	}
	return durations, open, nil
}

// ---------------------------------------------------------------------------
// source_status helper (G/D/L endpoints)
// ---------------------------------------------------------------------------

// AnalyticsSourceStatus is the §0.7 external-source state object.
type AnalyticsSourceStatus struct {
	Ready     bool    `json:"ready"`
	Reason    *string `json:"reason"`
	UpdatedAt *string `json:"updated_at"`
}

func sourceStatusReady(updatedAt *string) AnalyticsSourceStatus {
	return AnalyticsSourceStatus{Ready: true, Reason: nil, UpdatedAt: updatedAt}
}

func sourceStatusNotReady(reason string) AnalyticsSourceStatus {
	r := reason
	return AnalyticsSourceStatus{Ready: false, Reason: &r, UpdatedAt: nil}
}

// gitSourceStatus reports whether a Git data source is connected (VCS
// connection or configured workspace repos) plus the last PR sync time.
func (h *Handler) gitSourceStatus(ctx context.Context, ws pgtype.UUID) (AnalyticsSourceStatus, bool, error) {
	vcsCount, err := h.Queries.CountAnalyticsVCSConnections(ctx, ws)
	if err != nil {
		return AnalyticsSourceStatus{}, false, err
	}
	reposRaw, err := h.Queries.GetAnalyticsWorkspaceRepos(ctx, ws)
	if err != nil {
		return AnalyticsSourceStatus{}, false, err
	}
	var repos []struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(reposRaw), &repos); err != nil {
		repos = nil
	}
	connected := vcsCount > 0 || len(repos) > 0
	if !connected {
		return sourceStatusNotReady("Git 数据源未接入"), false, nil
	}
	var updatedAt *string
	lastSync, err := h.Queries.GetAnalyticsGitLastSync(ctx, ws)
	if err != nil {
		return AnalyticsSourceStatus{}, false, err
	}
	if lastSync.Valid {
		updatedAt = timestampToPtr(lastSync)
	}
	return sourceStatusReady(updatedAt), true, nil
}

// ---------------------------------------------------------------------------
// Tab3 G1: git eloc
// ---------------------------------------------------------------------------

// AnalyticsElocItem is one ranked ELOC contributor.
type AnalyticsElocItem struct {
	EntityID    string   `json:"entity_id"`
	Name        string   `json:"name"`
	ELOC        int64    `json:"eloc"`
	Ratio       *float64 `json:"ratio"`
	CommitCount int64    `json:"commit_count"`
	Repos       []string `json:"repos"`
}

// AnalyticsEloc is G1's payload.
type AnalyticsEloc struct {
	SourceStatus AnalyticsSourceStatus `json:"source_status"`
	GroupBy      string                `json:"group_by"`
	TotalELOC    int64                 `json:"total_eloc"`
	HumanELOC    int64                 `json:"human_eloc"`
	AgentELOC    int64                 `json:"agent_eloc"`
	Items        []AnalyticsElocItem   `json:"items"`
}

// GetAnalyticsGitEloc serves GET /api/analytics/git/eloc.
func (h *Handler) GetAnalyticsGitEloc(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.resolveAnalyticsScope(w, r, false)
	if !ok {
		return
	}
	groupBy := r.URL.Query().Get("group_by")
	if groupBy != "agent" {
		groupBy = "member"
	}
	ctx := r.Context()

	status, ready, err := h.gitSourceStatus(ctx, sc.workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load git eloc")
		return
	}
	if !ready {
		writeJSON(w, http.StatusOK, AnalyticsEloc{
			SourceStatus: status,
			GroupBy:      groupBy,
			TotalELOC:    0,
			HumanELOC:    0,
			AgentELOC:    0,
			Items:        []AnalyticsElocItem{},
		})
		return
	}

	byAuthor, err := h.Queries.ListAnalyticsElocByAuthor(ctx, db.ListAnalyticsElocByAuthorParams{
		WorkspaceID: sc.workspaceID,
		MergedAt:    sc.since,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load git eloc")
		return
	}
	mappings, err := h.Queries.ListAnalyticsAuthorMappings(ctx, sc.workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load git eloc")
		return
	}
	memberNames, err := h.Queries.ListAnalyticsMemberNames(ctx, sc.workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load git eloc")
		return
	}
	agentNames, err := h.Queries.ListAnalyticsAgentNames(ctx, sc.workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load git eloc")
		return
	}

	// Author -> entity mapping, then entity id -> display name.
	memberNameByID := make(map[string]string, len(memberNames))
	for _, m := range memberNames {
		memberNameByID[uuidToString(m.UserID)] = m.Name
	}
	agentNameByID := make(map[string]string, len(agentNames))
	for _, a := range agentNames {
		agentNameByID[uuidToString(a.ID)] = a.Name
	}

	// entityKey(groupBy) -> aggregated eloc + commit count + repos.
	type elocAcc struct {
		entityID string
		name     string
		eloc     int64
		commits  int64
		repos    map[string]struct{}
	}
	acc := make(map[string]*elocAcc)
	var order []string

	getAcc := func(entityID, name string) *elocAcc {
		a, ok := acc[entityID]
		if !ok {
			a = &elocAcc{entityID: entityID, name: name, repos: map[string]struct{}{}}
			acc[entityID] = a
			order = append(order, entityID)
		}
		return a
	}

	// Fold every author's eloc into its mapped entity (member or agent) or the
	// "unmapped" bucket. Authors mapped to the other group (not requested) are
	// excluded from items but still counted in human/agent totals.
	for _, m := range byAuthor {
		entityID, entityType, ok := resolveAuthorEntity(m.Author, mappings)
		if !ok {
			entityID = "unmapped"
			entityType = "unmapped"
		}
		excludedFromItems := false
		name := "未归属"
		if entityType == "member" {
			name = memberNameByID[entityID]
			if name == "" {
				name = "未归属"
			}
			if groupBy != "member" {
				excludedFromItems = true
			}
		} else if entityType == "agent" {
			name = agentNameByID[entityID]
			if name == "" {
				name = "未归属"
			}
			if groupBy != "agent" {
				excludedFromItems = true
			}
		} else {
			excludedFromItems = groupBy != "member" // unmapped shows in member view
			name = "未归属"
		}

		if !excludedFromItems {
			a := getAcc(entityID, name)
			a.eloc += m.Eloc
			a.commits += m.PrCount
			for _, r := range m.Repos {
				a.repos[r] = struct{}{}
			}
		}
	}

	// Compute human/agent totals from all authors (regardless of groupBy).
	var humanELOC, agentELOC int64
	var totalELOC int64
	for _, m := range byAuthor {
		totalELOC += m.Eloc
		_, entityType, ok := resolveAuthorEntity(m.Author, mappings)
		if !ok {
			entityType = "unmapped"
		}
		switch entityType {
		case "agent":
			agentELOC += m.Eloc
		case "member":
			humanELOC += m.Eloc
		default:
			humanELOC += m.Eloc // unmapped authors count toward human in the global split
		}
	}

	items := make([]AnalyticsElocItem, 0, len(order))
	for _, id := range order {
		a := acc[id]
		repos := make([]string, 0, len(a.repos))
		for r := range a.repos {
			repos = append(repos, r)
		}
		sort.Strings(repos)
		items = append(items, AnalyticsElocItem{
			EntityID:    a.entityID,
			Name:        a.name,
			ELOC:        a.eloc,
			Ratio:       ratio(a.eloc, totalELOC),
			CommitCount: a.commits,
			Repos:       repos,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ELOC > items[j].ELOC })

	writeJSON(w, http.StatusOK, AnalyticsEloc{
		SourceStatus: status,
		GroupBy:      groupBy,
		TotalELOC:    totalELOC,
		HumanELOC:    humanELOC,
		AgentELOC:    agentELOC,
		Items:        items,
	})
}

// resolveAuthorEntity maps an author to (entity_id, entity_type, found).
func resolveAuthorEntity(author string, mappings []db.ListAnalyticsAuthorMappingsRow) (string, string, bool) {
	if author == "" {
		return "", "", false
	}
	for _, m := range mappings {
		if m.Author == author {
			return uuidToString(m.EntityID), m.EntityType, true
		}
	}
	return "", "", false
}

// ---------------------------------------------------------------------------
// Tab3 G2: git quality
// ---------------------------------------------------------------------------

// AnalyticsQuality is G2's payload.
type AnalyticsQuality struct {
	SourceStatus    AnalyticsSourceStatus `json:"source_status"`
	Repo            string                `json:"repo"`
	SnapshotAt      *string               `json:"snapshot_at"`
	Coverage        *float64              `json:"coverage"`
	Vulnerabilities *int64                `json:"vulnerabilities"`
	DuplicationRate *float64              `json:"duplication_rate"`
}

// GetAnalyticsGitQuality serves GET /api/analytics/git/quality.
func (h *Handler) GetAnalyticsGitQuality(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.resolveAnalyticsScope(w, r, false)
	if !ok {
		return
	}
	repo := r.URL.Query().Get("repo")
	if repo == "" {
		repo = "all"
	}
	ctx := r.Context()

	snapshots, err := h.Queries.ListAnalyticsQualitySnapshots(ctx, sc.workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load git quality")
		return
	}
	if len(snapshots) == 0 {
		writeJSON(w, http.StatusOK, AnalyticsQuality{
			SourceStatus:    sourceStatusNotReady("提交质量扫描未配置"),
			Repo:            repo,
			SnapshotAt:      nil,
			Coverage:        nil,
			Vulnerabilities: nil,
			DuplicationRate: nil,
		})
		return
	}

	var snapshotAt *string
	var coverage *float64
	var vulnerabilities *int64
	var duplicationRate *float64

	if repo == "all" {
		// 汇总口径: simple mean across per-repo latest snapshots, snapshot_at =
		// max(snapshot_at) (数据口径 §3.2).
		var covSum, dupSum float64
		var vulnSum int64
		latest := time.Time{}
		for _, s := range snapshots {
			covSum += s.Coverage
			dupSum += s.DuplicationRate
			if s.Vulnerabilities.Valid {
				vulnSum += int64(s.Vulnerabilities.Int32)
			}
			if s.SnapshotAt.Valid && s.SnapshotAt.Time.After(latest) {
				latest = s.SnapshotAt.Time
			}
		}
		n := float64(len(snapshots))
		cov := covSum / n
		dup := dupSum / n
		coverage = &cov
		duplicationRate = &dup
		v := vulnSum
		vulnerabilities = &v
		if !latest.IsZero() {
			s := latest.Format(time.RFC3339)
			snapshotAt = &s
		}
	} else {
		for _, s := range snapshots {
			if s.Repo == repo {
				cov := s.Coverage
				coverage = &cov
				dup := s.DuplicationRate
				duplicationRate = &dup
				if s.Vulnerabilities.Valid {
					v := int64(s.Vulnerabilities.Int32)
					vulnerabilities = &v
				}
				if s.SnapshotAt.Valid {
					ts := util.TimestampToString(s.SnapshotAt)
					snapshotAt = &ts
				}
				break
			}
		}
	}

	writeJSON(w, http.StatusOK, AnalyticsQuality{
		SourceStatus:    sourceStatusReady(snapshotAt),
		Repo:            repo,
		SnapshotAt:      snapshotAt,
		Coverage:        coverage,
		Vulnerabilities: vulnerabilities,
		DuplicationRate: duplicationRate,
	})
}

// ---------------------------------------------------------------------------
// Tab3 G3: git repos
// ---------------------------------------------------------------------------

// AnalyticsRepoActivity is one repo row of G3.
type AnalyticsRepoActivity struct {
	Repo          string   `json:"repo"`
	ActiveCommits int64    `json:"active_commits"`
	ActivePRs     int64    `json:"active_prs"`
	Activity      int64    `json:"activity"`
	OpenPrBacklog int64    `json:"open_pr_backlog"`
	MTMP50Seconds *float64 `json:"mtm_p50_seconds"`
	MTMP95Seconds *float64 `json:"mtm_p95_seconds"`
}

// GetAnalyticsGitRepos serves GET /api/analytics/git/repos.
func (h *Handler) GetAnalyticsGitRepos(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.resolveAnalyticsScope(w, r, false)
	if !ok {
		return
	}
	ctx := r.Context()

	status, ready, err := h.gitSourceStatus(ctx, sc.workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load git repos")
		return
	}
	if !ready {
		writeJSON(w, http.StatusOK, map[string]any{
			"source_status": status,
			"items":         []AnalyticsRepoActivity{},
		})
		return
	}

	activity, err := h.Queries.ListAnalyticsRepoActivity(ctx, db.ListAnalyticsRepoActivityParams{
		WorkspaceID: sc.workspaceID,
		PrCreatedAt: sc.since,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load git repos")
		return
	}
	merged, err := h.Queries.ListAnalyticsRepoMerged(ctx, db.ListAnalyticsRepoMergedParams{
		WorkspaceID: sc.workspaceID,
		MergedAt:    sc.since,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load git repos")
		return
	}

	mtmByRepo := make(map[string][]float64)
	for _, m := range merged {
		mtmByRepo[m.Repo] = append(mtmByRepo[m.Repo], m.MtmSeconds)
	}

	items := make([]AnalyticsRepoActivity, 0, len(activity))
	for _, a := range activity {
		var p50, p95 *float64
		if durs, ok := mtmByRepo[a.Repo]; ok && len(durs) > 0 {
			p50v, p95v, _ := percentileDurations(durs)
			p50, p95 = p50v, p95v
		}
		items = append(items, AnalyticsRepoActivity{
			Repo:          a.Repo,
			ActiveCommits: 0,
			ActivePRs:     a.ActivePrs,
			Activity:      a.ActivePrs,
			OpenPrBacklog: a.OpenPrBacklog,
			MTMP50Seconds: p50,
			MTMP95Seconds: p95,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Activity > items[j].Activity })

	writeJSON(w, http.StatusOK, map[string]any{
		"source_status": status,
		"items":         items,
	})
}

// ---------------------------------------------------------------------------
// Tab3 G4: git PRs
// ---------------------------------------------------------------------------

// AnalyticsPRDetail is one PR row of G4.
type AnalyticsPRDetail struct {
	PrNumber    int     `json:"pr_number"`
	Title       string  `json:"title"`
	State       string  `json:"state"`
	AuthorLogin *string `json:"author_login"`
	PrCreatedAt string  `json:"pr_created_at"`
	MergedAt    *string `json:"merged_at"`
	ClosedAt    *string `json:"closed_at"`
	Additions   int     `json:"additions"`
	Deletions   int     `json:"deletions"`
	HtmlURL     string  `json:"html_url"`
}

// GetAnalyticsGitPRs serves GET /api/analytics/git/prs.
func (h *Handler) GetAnalyticsGitPRs(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.resolveAnalyticsScope(w, r, false)
	if !ok {
		return
	}
	repo := r.URL.Query().Get("repo")
	if repo == "" {
		writeError(w, http.StatusBadRequest, "repo is required")
		return
	}
	state := r.URL.Query().Get("state")
	limit := parseAnalyticsLimit(r, 20, 100)
	ctx := r.Context()

	status, ready, err := h.gitSourceStatus(ctx, sc.workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load git prs")
		return
	}
	if !ready {
		writeJSON(w, http.StatusOK, map[string]any{
			"source_status": status,
			"items":         []AnalyticsPRDetail{},
		})
		return
	}

	rows, err := h.Queries.ListAnalyticsPRs(ctx, db.ListAnalyticsPRsParams{
		WorkspaceID: sc.workspaceID,
		RepoOwner:   repo,
		Column3:     state,
		Limit:       int32(limit),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load git prs")
		return
	}

	items := make([]AnalyticsPRDetail, 0, len(rows))
	for _, pr := range rows {
		items = append(items, AnalyticsPRDetail{
			PrNumber:    int(pr.PrNumber),
			Title:       pr.Title,
			State:       pr.State,
			AuthorLogin: textToPtr(pr.AuthorLogin),
			PrCreatedAt: util.TimestampToString(pr.PrCreatedAt),
			MergedAt:    timestampToPtr(pr.MergedAt),
			ClosedAt:    timestampToPtr(pr.ClosedAt),
			Additions:   int(pr.Additions),
			Deletions:   int(pr.Deletions),
			HtmlURL:     pr.HtmlUrl,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"source_status": status,
		"items":         items,
	})
}

// ---------------------------------------------------------------------------
// Tab4 D1: dora lead time
// ---------------------------------------------------------------------------

// AnalyticsLeadTimePoint is one weekly lead-time bucket.
type AnalyticsLeadTimePoint struct {
	Week        string   `json:"week"`
	P50Seconds  *float64 `json:"p50_seconds"`
	P95Seconds  *float64 `json:"p95_seconds"`
	SampleCount int64    `json:"sample_count"`
}

// AnalyticsLeadTime is D1's payload.
type AnalyticsLeadTime struct {
	SourceStatus AnalyticsSourceStatus    `json:"source_status"`
	Metric       string                   `json:"metric"`
	Points       []AnalyticsLeadTimePoint `json:"points"`
}

// GetAnalyticsDoraLeadTime serves GET /api/analytics/dora/lead-time.
func (h *Handler) GetAnalyticsDoraLeadTime(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.resolveAnalyticsScope(w, r, false)
	if !ok {
		return
	}
	metric := r.URL.Query().Get("metric")
	if metric != "deploy" && metric != "merged" {
		metric = "deploy"
	}
	ctx := r.Context()

	if metric == "deploy" {
		deployCount, err := h.Queries.CountAnalyticsDeployments(ctx, sc.workspaceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load dora lead time")
			return
		}
		if deployCount == 0 {
			writeJSON(w, http.StatusOK, AnalyticsLeadTime{
				SourceStatus: sourceStatusNotReady("部署流水线数据未接入"),
				Metric:       metric,
				Points:       []AnalyticsLeadTimePoint{},
			})
			return
		}
		rows, err := h.Queries.ListAnalyticsLeadTimeDeploy(ctx, db.ListAnalyticsLeadTimeDeployParams{
			WorkspaceID: sc.workspaceID,
			Column2:     sc.tz,
			FinishedAt:  sc.since,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load dora lead time")
			return
		}
		samples := make([]leadTimeSample, 0, len(rows))
		for _, r := range rows {
			samples = append(samples, leadTimeSample{week: r.Week.Time.Format(time.DateOnly), leadSeconds: r.LeadSeconds})
		}
		points := buildWeeklyLeadTime(samples)
		var updatedAt *string
		writeJSON(w, http.StatusOK, AnalyticsLeadTime{
			SourceStatus: sourceStatusReady(updatedAt),
			Metric:       metric,
			Points:       points,
		})
		return
	}

	// metric=merged: guide state when no VCS data at all.
	_, ready, err := h.gitSourceStatus(ctx, sc.workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load dora lead time")
		return
	}
	if !ready {
		writeJSON(w, http.StatusOK, AnalyticsLeadTime{
			SourceStatus: sourceStatusNotReady("部署流水线数据未接入，且无 VCS 合并数据"),
			Metric:       metric,
			Points:       []AnalyticsLeadTimePoint{},
		})
		return
	}
	rows, err := h.Queries.ListAnalyticsLeadTimeMerged(ctx, db.ListAnalyticsLeadTimeMergedParams{
		WorkspaceID: sc.workspaceID,
		Column2:     sc.tz,
		MergedAt:    sc.since,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load dora lead time")
		return
	}
	samples := make([]leadTimeSample, 0, len(rows))
	for _, r := range rows {
		samples = append(samples, leadTimeSample{week: r.Week.Time.Format(time.DateOnly), leadSeconds: r.LeadSeconds})
	}
	points := buildWeeklyLeadTime(samples)
	var updatedAt *string
	writeJSON(w, http.StatusOK, AnalyticsLeadTime{
		SourceStatus: sourceStatusReady(updatedAt),
		Metric:       metric,
		Points:       points,
	})
}

type leadTimeSample struct {
	week        string
	leadSeconds float64
}

func buildWeeklyLeadTime(samples []leadTimeSample) []AnalyticsLeadTimePoint {
	byWeek := make(map[string][]float64)
	for _, r := range samples {
		byWeek[r.week] = append(byWeek[r.week], r.leadSeconds)
	}
	weeks := make([]string, 0, len(byWeek))
	for w := range byWeek {
		weeks = append(weeks, w)
	}
	sort.Strings(weeks)

	points := make([]AnalyticsLeadTimePoint, 0, len(weeks))
	for _, w := range weeks {
		durs := byWeek[w]
		p50, p95, _ := percentileDurations(durs)
		points = append(points, AnalyticsLeadTimePoint{
			Week:        w,
			P50Seconds:  p50,
			P95Seconds:  p95,
			SampleCount: int64(len(durs)),
		})
	}
	return points
}

// ---------------------------------------------------------------------------
// Tab4 D2: dora deployments
// ---------------------------------------------------------------------------

// AnalyticsDeploymentFailure is one failed-deployment row.
type AnalyticsDeploymentFailure struct {
	DeploymentID string   `json:"deployment_id"`
	App          *string  `json:"app"`
	FailedAt     string   `json:"failed_at"`
	RecoveredAt  *string  `json:"recovered_at"`
	MTTRSeconds  *float64 `json:"mttr_seconds"`
	Reason       *string  `json:"reason"`
	Status       string   `json:"status"`
}

// AnalyticsDeploymentTrendPoint is one weekly deployment bucket.
type AnalyticsDeploymentTrendPoint struct {
	Week        string   `json:"week"`
	Deployments int64    `json:"deployments"`
	Failed      int64    `json:"failed"`
	FailureRate *float64 `json:"failure_rate"`
	MTTRSeconds *float64 `json:"mttr_seconds"`
}

// AnalyticsDeployments is D2's payload.
type AnalyticsDeployments struct {
	SourceStatus      AnalyticsSourceStatus           `json:"source_status"`
	TotalDeployments  int64                           `json:"total_deployments"`
	FailedDeployments int64                           `json:"failed_deployments"`
	DeployFreqWeekly  *float64                        `json:"deploy_frequency_weekly"`
	ChangeFailureRate *float64                        `json:"change_failure_rate"`
	MTTRSeconds       *float64                        `json:"mttr_seconds"`
	Trend             []AnalyticsDeploymentTrendPoint `json:"trend"`
	Failures          []AnalyticsDeploymentFailure    `json:"failures"`
}

// GetAnalyticsDoraDeployments serves GET /api/analytics/dora/deployments.
func (h *Handler) GetAnalyticsDoraDeployments(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.resolveAnalyticsScope(w, r, false)
	if !ok {
		return
	}
	ctx := r.Context()

	deployCount, err := h.Queries.CountAnalyticsDeployments(ctx, sc.workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load dora deployments")
		return
	}
	if deployCount == 0 {
		writeJSON(w, http.StatusOK, AnalyticsDeployments{
			SourceStatus:      sourceStatusNotReady("部署流水线数据未接入"),
			TotalDeployments:  0,
			FailedDeployments: 0,
			DeployFreqWeekly:  nil,
			ChangeFailureRate: nil,
			MTTRSeconds:       nil,
			Trend:             []AnalyticsDeploymentTrendPoint{},
			Failures:          []AnalyticsDeploymentFailure{},
		})
		return
	}

	summary, err := h.Queries.GetAnalyticsDeploymentSummary(ctx, db.GetAnalyticsDeploymentSummaryParams{
		WorkspaceID: sc.workspaceID,
		FinishedAt:  sc.since,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load dora deployments")
		return
	}
	trendRows, err := h.Queries.ListAnalyticsDeploymentTrend(ctx, db.ListAnalyticsDeploymentTrendParams{
		WorkspaceID: sc.workspaceID,
		Column2:     sc.tz,
		FinishedAt:  sc.since,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load dora deployments")
		return
	}
	failureRows, err := h.Queries.ListAnalyticsDeploymentFailures(ctx, db.ListAnalyticsDeploymentFailuresParams{
		WorkspaceID: sc.workspaceID,
		FinishedAt:  sc.since,
		Limit:       20,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load dora deployments")
		return
	}

	// Weekly deploy frequency: total deployments / number of weeks covered.
	weeks := 1
	if days := int(sc.windowEnd.Sub(sc.windowStart).Hours() / 24); days > 0 {
		weeks = (days / 7) + 1
	}
	var freq *float64
	if summary.TotalDeployments > 0 {
		v := round1p(float64(summary.TotalDeployments) / float64(weeks))
		freq = &v
	}

	trend := make([]AnalyticsDeploymentTrendPoint, 0, len(trendRows))
	for _, t := range trendRows {
		trend = append(trend, AnalyticsDeploymentTrendPoint{
			Week:        t.Week.Time.Format(time.DateOnly),
			Deployments: t.Deployments,
			Failed:      t.Failed,
			FailureRate: ratio(t.Failed, t.Deployments),
		})
	}

	failures := make([]AnalyticsDeploymentFailure, 0, len(failureRows))
	var mttrSamples []float64
	for _, f := range failureRows {
		item := AnalyticsDeploymentFailure{
			DeploymentID: f.DeploymentID,
			App:          textToPtr(f.App),
			FailedAt:     util.TimestampToString(f.FailedAt),
			RecoveredAt:  timestampToPtr(f.RecoveredAt),
			Reason:       textToPtr(f.Reason),
			Status:       "open",
		}
		if f.RecoveredAt.Valid {
			item.Status = "resolved"
			secs := f.RecoveredAt.Time.Sub(f.FailedAt.Time).Seconds()
			item.MTTRSeconds = &secs
			mttrSamples = append(mttrSamples, secs)
		}
		failures = append(failures, item)
	}
	var mttr *float64
	if len(mttrSamples) > 0 {
		_, p50, _ := percentileDurations(mttrSamples)
		mttr = p50
	}

	writeJSON(w, http.StatusOK, AnalyticsDeployments{
		SourceStatus:      sourceStatusReady(nil),
		TotalDeployments:  summary.TotalDeployments,
		FailedDeployments: summary.FailedDeployments,
		DeployFreqWeekly:  freq,
		ChangeFailureRate: ratio(summary.FailedDeployments, summary.TotalDeployments),
		MTTRSeconds:       mttr,
		Trend:             trend,
		Failures:          failures,
	})
}

// ---------------------------------------------------------------------------
// L1: identity lifecycle
// ---------------------------------------------------------------------------

// AnalyticsLifecycleEvent is one identity event row.
type AnalyticsLifecycleEvent struct {
	EventType  string  `json:"event_type"`
	MemberName string  `json:"member_name"`
	OccurredAt string  `json:"occurred_at"`
	Result     *string `json:"result"`
}

// AnalyticsLifecycle is L1's payload.
type AnalyticsLifecycle struct {
	SourceStatus       AnalyticsSourceStatus     `json:"source_status"`
	OnboardingRate     *float64                  `json:"onboarding_rate"`
	OnboardedMembers   int64                     `json:"onboarded_members"`
	NewHiresTotal      int64                     `json:"new_hires_total"`
	CutoffEvents       int64                     `json:"cutoff_events"`
	CutoffSuccessCount int64                     `json:"cutoff_success_count"`
	CutoffSuccessRate  *float64                  `json:"cutoff_success_rate"`
	RecentEvents       []AnalyticsLifecycleEvent `json:"recent_events"`
}

// GetAnalyticsIdentityLifecycle serves GET /api/analytics/identity/lifecycle.
func (h *Handler) GetAnalyticsIdentityLifecycle(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.resolveAnalyticsScope(w, r, true)
	if !ok {
		return
	}
	ctx := r.Context()

	eventCount, err := h.Queries.CountAnalyticsIdentityEvents(ctx, sc.workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load identity lifecycle")
		return
	}
	if eventCount == 0 {
		writeJSON(w, http.StatusOK, AnalyticsLifecycle{
			SourceStatus:       sourceStatusNotReady("用户身份数据源未接入"),
			OnboardingRate:     nil,
			OnboardedMembers:   0,
			NewHiresTotal:      0,
			CutoffEvents:       0,
			CutoffSuccessCount: 0,
			CutoffSuccessRate:  nil,
			RecentEvents:       []AnalyticsLifecycleEvent{},
		})
		return
	}

	summary, err := h.Queries.GetAnalyticsIdentitySummary(ctx, db.GetAnalyticsIdentitySummaryParams{
		WorkspaceID: sc.workspaceID,
		OccurredAt:  sc.since,
		Column3:     sc.department,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load identity lifecycle")
		return
	}
	events, err := h.Queries.ListAnalyticsIdentityEvents(ctx, db.ListAnalyticsIdentityEventsParams{
		WorkspaceID: sc.workspaceID,
		OccurredAt:  sc.since,
		Column3:     sc.department,
		Limit:       20,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load identity lifecycle")
		return
	}

	recent := make([]AnalyticsLifecycleEvent, 0, len(events))
	for _, e := range events {
		result := e.Result
		recent = append(recent, AnalyticsLifecycleEvent{
			EventType:  e.EventType,
			MemberName: e.MemberName,
			OccurredAt: util.TimestampToString(e.OccurredAt),
			Result:     &result,
		})
	}

	writeJSON(w, http.StatusOK, AnalyticsLifecycle{
		SourceStatus:       sourceStatusReady(nil),
		OnboardingRate:     ratio(summary.OnboardedMembers, summary.NewHiresTotal),
		OnboardedMembers:   summary.OnboardedMembers,
		NewHiresTotal:      summary.NewHiresTotal,
		CutoffEvents:       summary.CutoffEvents,
		CutoffSuccessCount: summary.CutoffSuccessCount,
		CutoffSuccessRate:  ratio(summary.CutoffSuccessCount, summary.CutoffEvents),
		RecentEvents:       recent,
	})
}

// ---------------------------------------------------------------------------
// L2: identity departments
// ---------------------------------------------------------------------------

// AnalyticsDepartment is one department row of L2.
type AnalyticsDepartment struct {
	DepartmentID  string `json:"department_id"`
	Name          string `json:"name"`
	MemberCount   int64  `json:"member_count"`
	ActiveMembers int64  `json:"active_members"`
}

// GetAnalyticsIdentityDepartments serves GET /api/analytics/identity/departments.
func (h *Handler) GetAnalyticsIdentityDepartments(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.resolveAnalyticsScope(w, r, false)
	if !ok {
		return
	}
	ctx := r.Context()

	deptCount, err := h.Queries.CountAnalyticsMembersWithDepartment(ctx, sc.workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load departments")
		return
	}
	if deptCount == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"source_status": sourceStatusNotReady("用户身份数据源未接入"),
			"items":         []AnalyticsDepartment{},
		})
		return
	}

	counts, err := h.Queries.CountAnalyticsMembersByDepartment(ctx, sc.workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load departments")
		return
	}
	active, err := h.Queries.ListAnalyticsActiveMembersByDepartment(ctx, db.ListAnalyticsActiveMembersByDepartmentParams{
		WorkspaceID: sc.workspaceID,
		CreatedAt:   sc.since,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load departments")
		return
	}
	activeByDept := make(map[string]int64, len(active))
	for _, a := range active {
		activeByDept[a.Department] = a.ActiveMembers
	}

	items := make([]AnalyticsDepartment, 0, len(counts))
	for _, c := range counts {
		items = append(items, AnalyticsDepartment{
			DepartmentID:  c.Department,
			Name:          c.Department,
			MemberCount:   c.MemberCount,
			ActiveMembers: activeByDept[c.Department],
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"source_status": sourceStatusReady(nil),
		"items":         items,
	})
}
