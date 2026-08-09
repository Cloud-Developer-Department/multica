package handler

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// Data-monitoring dashboard (CLO-170)
//
// Four aggregation endpoints power the `/{slug}/dashboard` monitoring page:
//
//   GET /api/dashboard/issue-distribution   status + per-project rollups
//   GET /api/dashboard/activity             agent workload + team activity
//   GET /api/dashboard/comments             comment totals + time series
//   GET /api/dashboard/completion           completion/delay rates + trend
//
// All four accept ?days=N (default 7, capped 365) and ?tz=<IANA zone>; the
// time range drives the count/trend modules while issue-distribution stays a
// current-state snapshot (PRD §4.1 "平台内全部 Issue"). Statistics follow
// prd-data-monitoring-dashboard.md §4 (see monitoring.sql header).
// ---------------------------------------------------------------------------

// MonitoringStatusCounts is a status→count bag keyed by status id. Kept as a
// plain map so a future status added to the DB survives the wire untouched.
type MonitoringStatusCounts map[string]int64

// MonitoringProjectProgress is one project's status composition.
type MonitoringProjectProgress struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Total        int64                  `json:"total"`
	StatusCounts MonitoringStatusCounts `json:"status_counts"`
}

// MonitoringIssueDistribution is the issue-distribution module payload.
type MonitoringIssueDistribution struct {
	Total        int64                       `json:"total"`
	StatusCounts MonitoringStatusCounts      `json:"status_counts"`
	Projects     []MonitoringProjectProgress `json:"projects"`
}

// GetMonitoringIssueDistribution returns the current-state status distribution
// and per-project status composition for the whole workspace (PRD §4.1/§4.2).
// Time range does not apply here: it is a snapshot of every issue in the
// workspace. Issues without a project collapse into a single bucket with an
// empty id/name so project totals always sum to the workspace total.
func (h *Handler) GetMonitoringIssueDistribution(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wid := parseUUID(workspaceID)

	statusRows, err := h.Queries.ListMonitoringIssueStatusCounts(r.Context(), wid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load issue distribution")
		return
	}
	projectRows, err := h.Queries.ListMonitoringProjectStatusCounts(r.Context(), wid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project progress")
		return
	}

	statusCounts := MonitoringStatusCounts{}
	var total int64
	for _, row := range statusRows {
		statusCounts[row.Status] = row.Count
		total += row.Count
	}

	projects := buildProjectProgress(projectRows)
	writeJSON(w, http.StatusOK, MonitoringIssueDistribution{
		Total:        total,
		StatusCounts: statusCounts,
		Projects:     projects,
	})
}

// buildProjectProgress folds raw (project, status, count) rows into
// per-project buckets, preserving status granularity per project.
func buildProjectProgress(rows []db.ListMonitoringProjectStatusCountsRow) []MonitoringProjectProgress {
	index := make(map[string]int)
	out := make([]MonitoringProjectProgress, 0, len(rows))
	for _, row := range rows {
		idx, ok := index[row.ProjectID]
		if !ok {
			idx = len(out)
			index[row.ProjectID] = idx
			out = append(out, MonitoringProjectProgress{
				ID:           row.ProjectID,
				Name:         row.ProjectName,
				Total:        0,
				StatusCounts: MonitoringStatusCounts{},
			})
		}
		out[idx].Total += row.Count
		out[idx].StatusCounts[row.Status] = row.Count
	}
	// Stable ordering: projects with a real id first, then by name, then the
	// no-project bucket last — the frontend re-ranks by total anyway.
	sort.SliceStable(out, func(i, j int) bool {
		li, lj := out[i].ID == "", out[j].ID == ""
		if li != lj {
			return !li
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// MonitoringActivityEntity is one agent/squad workload + activity row.
type MonitoringActivityEntity struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Load     int64  `json:"load"`
	Activity int64  `json:"activity"`
}

// MonitoringActivity is the agent-workload / team-activity module payload.
type MonitoringActivity struct {
	AgentWorkload []MonitoringActivityEntity `json:"agent_workload"`
	TeamActivity  []MonitoringActivityEntity `json:"team_activity"`
}

// GetMonitoringActivity returns per-agent workload (在办负荷: in_progress +
// in_review assigned count) and per-squad team activity. Activity score is
// status_changed + comment + run counts within the window (PRD §4.3). Squad
// activity rolls up from the squad's agent members.
func (h *Handler) GetMonitoringActivity(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wid := parseUUID(workspaceID)
	tz := h.resolveViewingTZ(r)
	since := parseExactSinceParamInTZ(r, 7, tz)

	ctx := r.Context()
	agentActivity, err := h.monitoringAgentActivity(ctx, wid, since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent activity")
		return
	}
	loads, err := h.Queries.ListMonitoringAgentLoad(ctx, wid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agent workload")
		return
	}

	agentWorkload := make([]MonitoringActivityEntity, 0, len(loads))
	for _, row := range loads {
		id := uuidToString(row.ID)
		activity := agentActivity[id]
		if row.LoadCount == 0 && activity == 0 {
			continue
		}
		agentWorkload = append(agentWorkload, MonitoringActivityEntity{
			ID:       id,
			Name:     row.Name,
			Load:     row.LoadCount,
			Activity: activity,
		})
	}
	sort.Slice(agentWorkload, func(i, j int) bool {
		if agentWorkload[i].Load != agentWorkload[j].Load {
			return agentWorkload[i].Load > agentWorkload[j].Load
		}
		return agentWorkload[i].Activity > agentWorkload[j].Activity
	})

	teamActivity, err := h.monitoringTeamActivity(ctx, wid, agentActivity)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load team activity")
		return
	}

	writeJSON(w, http.StatusOK, MonitoringActivity{
		AgentWorkload: agentWorkload,
		TeamActivity:  teamActivity,
	})
}

// monitoringAgentActivity builds an agent-id → activity-score map from the
// three behavior signals within the window.
func (h *Handler) monitoringAgentActivity(ctx context.Context, wid pgtype.UUID, since pgtype.Timestamptz) (map[string]int64, error) {
	score := make(map[string]int64)

	statusChanges, err := h.Queries.CountMonitoringStatusChangesByAgent(ctx, db.CountMonitoringStatusChangesByAgentParams{
		WorkspaceID: wid,
		CreatedAt:   since,
	})
	if err != nil {
		return nil, err
	}
	comments, err := h.Queries.CountMonitoringCommentsByAgent(ctx, db.CountMonitoringCommentsByAgentParams{
		WorkspaceID: wid,
		CreatedAt:   since,
	})
	if err != nil {
		return nil, err
	}
	runs, err := h.Queries.CountMonitoringRunsByAgent(ctx, db.CountMonitoringRunsByAgentParams{
		WorkspaceID: wid,
		CreatedAt:   since,
	})
	if err != nil {
		return nil, err
	}

	for _, row := range statusChanges {
		score[uuidToString(row.ActorID)] += row.Count
	}
	for _, row := range comments {
		score[uuidToString(row.AuthorID)] += row.Count
	}
	for _, row := range runs {
		score[uuidToString(row.AgentID)] += row.Count
	}
	return score, nil
}

// monitoringTeamActivity builds squad activity rows by rolling member-agent
// activity scores up into each squad and merging in squad-assigned load.
func (h *Handler) monitoringTeamActivity(ctx context.Context, wid pgtype.UUID, agentScore map[string]int64) ([]MonitoringActivityEntity, error) {
	squadLoads, err := h.Queries.ListMonitoringSquadLoad(ctx, wid)
	if err != nil {
		return nil, err
	}
	members, err := h.Queries.ListMonitoringSquadMemberAgents(ctx, wid)
	if err != nil {
		return nil, err
	}

	squadScore := make(map[string]int64)
	for _, m := range members {
		squadScore[uuidToString(m.SquadID)] += agentScore[uuidToString(m.AgentID)]
	}

	out := make([]MonitoringActivityEntity, 0, len(squadLoads))
	for _, row := range squadLoads {
		id := uuidToString(row.ID)
		activity := squadScore[id]
		if row.LoadCount == 0 && activity == 0 {
			continue
		}
		out = append(out, MonitoringActivityEntity{
			ID:       id,
			Name:     row.Name,
			Load:     row.LoadCount,
			Activity: activity,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Activity != out[j].Activity {
			return out[i].Activity > out[j].Activity
		}
		return out[i].Load > out[j].Load
	})
	return out, nil
}

// MonitoringCommentPoint is one comment-series bucket (start-of-bucket ISO).
type MonitoringCommentPoint struct {
	Time  string `json:"time"`
	Count int64  `json:"count"`
}

// MonitoringComments is the comment-heat module payload.
type MonitoringComments struct {
	Total  int64                    `json:"total"`
	Today  int64                    `json:"today"`
	Series []MonitoringCommentPoint `json:"series"`
}

// GetMonitoringComments returns total/today counts plus a time series of new
// comments. Hourly buckets for the 24h range (days=1), daily buckets for
// 7d/30d (PRD §4.4). Empty hours/days are zero-filled so the area chart stays
// contiguous.
func (h *Handler) GetMonitoringComments(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wid := parseUUID(workspaceID)
	tz := h.resolveViewingTZ(r)
	loc, _ := time.LoadLocation(tz)
	if loc == nil {
		loc = time.UTC
	}
	days := parseMonitoringDays(r)

	ctx := r.Context()
	var since time.Time
	if days <= 1 {
		// 24h range: hourly buckets across the trailing 24h window.
		since = time.Now().Add(-24 * time.Hour)
	} else {
		// N-day range: exactly N calendar days including today in the viewer tz.
		now := time.Now().In(loc)
		startToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
		since = startToday.AddDate(0, 0, -(days - 1))
	}
	sinceTS := pgtype.Timestamptz{Time: since, Valid: true}

	total, err := h.Queries.CountMonitoringCommentsInWindow(ctx, db.CountMonitoringCommentsInWindowParams{
		WorkspaceID: wid,
		CreatedAt:   sinceTS,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load comment totals")
		return
	}

	now := time.Now().In(loc)
	startToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	today, err := h.Queries.CountMonitoringCommentsInWindow(ctx, db.CountMonitoringCommentsInWindowParams{
		WorkspaceID: wid,
		CreatedAt:   pgtype.Timestamptz{Time: startToday, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load today's comments")
		return
	}

	var series []MonitoringCommentPoint
	if days <= 1 {
		series, err = h.hourlyCommentSeries(ctx, wid, tz, since)
	} else {
		series, err = h.dailyCommentSeries(ctx, wid, tz, since, days)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load comment series")
		return
	}

	writeJSON(w, http.StatusOK, MonitoringComments{
		Total:  total,
		Today:  today,
		Series: series,
	})
}

// hourlyCommentSeries returns 24 zero-filled hourly buckets ending at the
// current hour in the viewer's tz.
func (h *Handler) hourlyCommentSeries(ctx context.Context, wid pgtype.UUID, tz string, since time.Time) ([]MonitoringCommentPoint, error) {
	loc, _ := time.LoadLocation(tz)
	if loc == nil {
		loc = time.UTC
	}
	rows, err := h.Queries.ListMonitoringCommentsHourly(ctx, db.ListMonitoringCommentsHourlyParams{
		WorkspaceID: wid,
		Column2:     tz,
		CreatedAt:   pgtype.Timestamptz{Time: since, Valid: true},
	})
	if err != nil {
		return nil, err
	}

	counts := make(map[time.Time]int64, len(rows))
	for _, row := range rows {
		if !row.Bucket.Valid {
			continue
		}
		// Bucket comes back as a local wall-clock timestamp; reinterpret it in
		// the viewer tz so the emitted instant lines up with the axis label.
		wall := row.Bucket.Time
		local := time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), wall.Second(), 0, loc)
		counts[local.Truncate(time.Hour)] = row.Count
	}

	now := time.Now().In(loc).Truncate(time.Hour)
	start := now.Add(-23 * time.Hour)
	out := make([]MonitoringCommentPoint, 0, 24)
	for bucket := start; !bucket.After(now); bucket = bucket.Add(time.Hour) {
		out = append(out, MonitoringCommentPoint{
			Time:  bucket.UTC().Format(time.RFC3339),
			Count: counts[bucket],
		})
	}
	return out, nil
}

// dailyCommentSeries returns `days` zero-filled daily buckets ending today in
// the viewer's tz.
func (h *Handler) dailyCommentSeries(ctx context.Context, wid pgtype.UUID, tz string, since time.Time, days int) ([]MonitoringCommentPoint, error) {
	loc, _ := time.LoadLocation(tz)
	if loc == nil {
		loc = time.UTC
	}
	rows, err := h.Queries.ListMonitoringCommentsDaily(ctx, db.ListMonitoringCommentsDailyParams{
		WorkspaceID: wid,
		Column2:     tz,
		CreatedAt:   pgtype.Timestamptz{Time: since, Valid: true},
	})
	if err != nil {
		return nil, err
	}

	counts := make(map[time.Time]int64, len(rows))
	for _, row := range rows {
		if !row.Day.Valid {
			continue
		}
		day := row.Day.Time
		counts[time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)] = row.Count
	}

	now := time.Now().In(loc)
	startToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	out := make([]MonitoringCommentPoint, 0, days)
	for i := days - 1; i >= 0; i-- {
		day := startToday.AddDate(0, 0, -i)
		out = append(out, MonitoringCommentPoint{
			Time:  day.UTC().Format(time.RFC3339),
			Count: counts[day],
		})
	}
	return out, nil
}

// MonitoringCompletionPoint is one completion/delay trend day.
type MonitoringCompletionPoint struct {
	Time       string   `json:"time"`
	Completion float64  `json:"completion"`
	Delay      *float64 `json:"delay"`
}

// MonitoringCompletion is the completion/delay module payload.
type MonitoringCompletion struct {
	CompletionRate  float64                     `json:"completion_rate"`
	CompletionDelta float64                     `json:"completion_delta"`
	DelayRate       *float64                    `json:"delay_rate"`
	DelayDelta      *float64                    `json:"delay_delta"`
	HasDueDateTasks bool                        `json:"has_due_date_tasks"`
	Trend           []MonitoringCompletionPoint `json:"trend"`
}

// GetMonitoringCompletion returns completion / delay rates with deltas vs the
// preceding equal-length window, plus a per-day cohort trend (PRD §4.5).
// Delay metrics are null when no issues carry a due_date.
func (h *Handler) GetMonitoringCompletion(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wid := parseUUID(workspaceID)
	tz := h.resolveViewingTZ(r)
	loc, _ := time.LoadLocation(tz)
	if loc == nil {
		loc = time.UTC
	}
	days := parseMonitoringDays(r)

	now := time.Now().In(loc)
	startToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	since := startToday.AddDate(0, 0, -(days - 1))
	prevSince := startToday.AddDate(0, 0, -(2*days - 1))

	ctx := r.Context()
	current, err := h.Queries.GetMonitoringCompletionOverview(ctx, db.GetMonitoringCompletionOverviewParams{
		WorkspaceID: wid,
		Column2:     tz,
		CreatedAt:   pgtype.Timestamptz{Time: since, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load completion overview")
		return
	}
	prev, err := h.Queries.GetMonitoringCompletionOverview(ctx, db.GetMonitoringCompletionOverviewParams{
		WorkspaceID: wid,
		Column2:     tz,
		CreatedAt:   pgtype.Timestamptz{Time: prevSince, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load previous completion overview")
		return
	}

	completionRate := rate(current.Done, current.Total)
	prevCompletionRate := rate(prev.Done, prev.Total)
	delayRate := ratePtr(current.Overdue, current.HasDue)
	prevDelayRate := ratePtr(prev.Overdue, prev.HasDue)

	trendRows, err := h.Queries.ListMonitoringCompletionTrend(ctx, db.ListMonitoringCompletionTrendParams{
		WorkspaceID: wid,
		Column2:     tz,
		CreatedAt:   pgtype.Timestamptz{Time: since, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load completion trend")
		return
	}

	trend := make([]MonitoringCompletionPoint, 0, len(trendRows))
	for _, row := range trendRows {
		if !row.Day.Valid {
			continue
		}
		day := row.Day.Time
		localDay := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
		point := MonitoringCompletionPoint{
			Time:       localDay.UTC().Format(time.RFC3339),
			Completion: rate(row.Done, row.Total),
			Delay:      ratePtr(row.Overdue, row.HasDue),
		}
		trend = append(trend, point)
	}

	writeJSON(w, http.StatusOK, MonitoringCompletion{
		CompletionRate:  completionRate,
		CompletionDelta: delta(completionRate, prevCompletionRate),
		DelayRate:       delayRate,
		DelayDelta:      deltaPtr(delayRate, prevDelayRate),
		HasDueDateTasks: current.HasDue > 0,
		Trend:           trend,
	})
}

// parseMonitoringDays parses ?days= into a bounded (1..365) integer with a
// default of 7.
func parseMonitoringDays(r *http.Request) int {
	days := 7
	if d := strings.TrimSpace(r.URL.Query().Get("days")); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 && parsed <= 365 {
			days = parsed
		}
	}
	return days
}

// rate returns a 0-100 percentage (0 when the denominator is empty).
func rate(part, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

// ratePtr is rate with a nil result when there is no denominator.
func ratePtr(part, total int64) *float64 {
	if total <= 0 {
		return nil
	}
	v := rate(part, total)
	return &v
}

// delta returns the signed percentage-point difference (0 when either side
// is missing).
func delta(current, prev float64) float64 {
	return current - prev
}

// deltaPtr returns the signed percentage-point difference between two rates,
// nil when either side is missing.
func deltaPtr(current, prev *float64) *float64 {
	if current == nil || prev == nil {
		return nil
	}
	v := *current - *prev
	return &v
}
