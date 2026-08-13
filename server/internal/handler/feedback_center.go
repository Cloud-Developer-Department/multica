package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/logger"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Feedback Center (WS-24) — Public Read + Authenticated Write.
//
// All routes are workspace-scoped (RequireWorkspaceMember); write endpoints
// additionally enforce requireUserID. No admin role, no review flow, no
// extra RBAC — only logged-in users write, everyone in the workspace reads.

const (
	// feedbackCenterMaxTitle caps feedback titles at 200 chars (validation).
	feedbackCenterMaxTitle = 200
	// feedbackCenterMaxDescription aligns with the legacy 10k message cap.
	feedbackCenterMaxDescription = 10000
	// feedbackCenterMaxComment caps comment bodies at 5000 chars.
	feedbackCenterMaxComment = 5000
	// feedbackCenterDefaultPageSize / MaxPageSize bound list pagination.
	feedbackCenterDefaultPageSize = 20
	feedbackCenterMaxPageSize     = 100
)

// feedbackTypes is the allow-list for the feedback `type` enum. The DB CHECK
// constraint enforces the same set; this list lets the handler return a clean
// 400 before hitting the database.
var feedbackTypes = map[string]struct{}{
	"bug":         {},
	"feature":     {},
	"improvement": {},
	"other":       {},
}

// feedbackSorts is the allow-list for the `sort` query param.
var feedbackSorts = map[string]struct{}{
	"latest":   {},
	"hot":      {},
	"comments": {},
}

// FeedbackSummary is the wire shape for feedback list items and the detail
// view. Counts are aggregate queries (no redundant counter columns).
type FeedbackSummary struct {
	ID               string  `json:"id"`
	WorkspaceID      string  `json:"workspace_id"`
	CreatorID        string  `json:"creator_id"`
	CreatorName      string  `json:"creator_name"`
	CreatorAvatarURL *string `json:"creator_avatar_url"`
	Title            string  `json:"title"`
	Description      string  `json:"description"`
	Type             string  `json:"type"`
	VoteCount        int64   `json:"vote_count"`
	CommentCount     int64   `json:"comment_count"`
	MyVote           bool    `json:"my_vote"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

type FeedbackListResponse struct {
	Items    []FeedbackSummary `json:"items"`
	Total    int64             `json:"total"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
	HasMore  bool              `json:"has_more"`
}

type FeedbackCommentResponse struct {
	ID            string  `json:"id"`
	FeedbackID    string  `json:"feedback_id"`
	UserID        string  `json:"user_id"`
	UserName      string  `json:"user_name"`
	UserAvatarURL *string `json:"user_avatar_url"`
	Content       string  `json:"content"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

type FeedbackCommentListResponse struct {
	Items []FeedbackCommentResponse `json:"items"`
	Total int64                     `json:"total"`
}

// CreateFeedbackCenterRequest is the Feedback Center submission body. Note
// that creator_id / workspace_id are NOT accepted here — they are resolved
// from the authenticated session and workspace context on the server.
type CreateFeedbackCenterRequest struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

func feedbackSummaryFromRow(row db.ListFeedbacksRow) FeedbackSummary {
	return FeedbackSummary{
		ID:               uuidToString(row.ID),
		WorkspaceID:      uuidToString(row.WorkspaceID),
		CreatorID:        uuidToString(row.CreatorID),
		CreatorName:      row.CreatorName,
		CreatorAvatarURL: textToPtr(row.CreatorAvatarUrl),
		Title:            row.Title,
		Description:      row.Description,
		Type:             row.Type,
		VoteCount:        row.VoteCount,
		CommentCount:     row.CommentCount,
		MyVote:           row.MyVote,
		CreatedAt:        timestampToString(row.CreatedAt),
		UpdatedAt:        timestampToString(row.UpdatedAt),
	}
}

func feedbackSummaryFromDetail(row db.GetFeedbackRow) FeedbackSummary {
	return FeedbackSummary{
		ID:               uuidToString(row.ID),
		WorkspaceID:      uuidToString(row.WorkspaceID),
		CreatorID:        uuidToString(row.CreatorID),
		CreatorName:      row.CreatorName,
		CreatorAvatarURL: textToPtr(row.CreatorAvatarUrl),
		Title:            row.Title,
		Description:      row.Description,
		Type:             row.Type,
		VoteCount:        row.VoteCount,
		CommentCount:     row.CommentCount,
		MyVote:           row.MyVote,
		CreatedAt:        timestampToString(row.CreatedAt),
		UpdatedAt:        timestampToString(row.UpdatedAt),
	}
}

// viewerUUID returns the current user's UUID for the my_vote flag, or a zero
// UUID when the request carries no authenticated user. A zero UUID never
// matches a real feedback_vote row, so my_vote=false for unauthenticated
// readers (which is the correct Public Read behavior).
func viewerUUID(r *http.Request) pgtype.UUID {
	userID := requestUserID(r)
	if userID == "" {
		return pgtype.UUID{}
	}
	return parseUUID(userID)
}

// ListFeedbacks returns the Feedback Center list with optional type / keyword
// filters, sort (latest/hot/comments), and page+page_size pagination (default
// page_size 20, capped at 100).
func (h *Handler) ListFeedbacks(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	var typeFilter pgtype.Text
	if t := r.URL.Query().Get("type"); t != "" {
		if _, valid := feedbackTypes[t]; !valid {
			writeError(w, http.StatusBadRequest, "invalid feedback type")
			return
		}
		typeFilter = pgtype.Text{String: t, Valid: true}
	}

	var keywordFilter pgtype.Text
	if k := strings.TrimSpace(r.URL.Query().Get("keyword")); k != "" {
		keywordFilter = pgtype.Text{String: k, Valid: true}
	}

	sort := r.URL.Query().Get("sort")
	if sort == "" {
		sort = "latest"
	}
	if _, valid := feedbackSorts[sort]; !valid {
		writeError(w, http.StatusBadRequest, "invalid sort")
		return
	}

	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v >= 1 {
			page = v
		}
	}
	pageSize := feedbackCenterDefaultPageSize
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v >= 1 {
			pageSize = v
		}
	}
	if pageSize > feedbackCenterMaxPageSize {
		pageSize = feedbackCenterMaxPageSize
	}

	offset := (page - 1) * pageSize

	viewer := viewerUUID(r)
	rows, err := h.Queries.ListFeedbacks(r.Context(), db.ListFeedbacksParams{
		WorkspaceID: wsUUID,
		UserID:      viewer,
		Limit:       int32(pageSize),
		Offset:      int32(offset),
		Type:        typeFilter,
		Keyword:     keywordFilter,
		Sort:        pgtype.Text{String: sort, Valid: true},
	})
	if err != nil {
		slog.Warn("list feedbacks failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list feedbacks")
		return
	}

	total, err := h.Queries.CountFeedbacks(r.Context(), db.CountFeedbacksParams{
		WorkspaceID: wsUUID,
		Type:        typeFilter,
		Keyword:     keywordFilter,
	})
	if err != nil {
		slog.Warn("count feedbacks failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list feedbacks")
		return
	}

	items := make([]FeedbackSummary, len(rows))
	for i, row := range rows {
		items[i] = feedbackSummaryFromRow(row)
	}

	writeJSON(w, http.StatusOK, FeedbackListResponse{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		HasMore:  int64(offset+len(rows)) < total,
	})
}

// GetFeedback returns a single feedback's detail including aggregate vote and
// comment counts and the current user's my_vote flag.
func (h *Handler) GetFeedback(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "feedback id")
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	row, err := h.Queries.GetFeedback(r.Context(), db.GetFeedbackParams{
		ID:          idUUID,
		UserID:      viewerUUID(r),
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "feedback not found")
			return
		}
		slog.Warn("get feedback failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load feedback")
		return
	}

	writeJSON(w, http.StatusOK, feedbackSummaryFromDetail(row))
}

// CreateFeedbackCenter creates a feedback for the Feedback Center. Requires a
// logged-in user; creator_id comes from the session and workspace_id from the
// current workspace context — neither is accepted from the request body.
func (h *Handler) CreateFeedbackCenter(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, feedbackBodyLimit)
	var req CreateFeedbackCenterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Type = strings.TrimSpace(req.Type)
	req.Title = strings.TrimSpace(req.Title)
	req.Description = strings.TrimSpace(req.Description)

	if _, valid := feedbackTypes[req.Type]; !valid {
		writeError(w, http.StatusBadRequest, "invalid feedback type")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if len(req.Title) > feedbackCenterMaxTitle {
		writeError(w, http.StatusBadRequest, "title too long")
		return
	}
	if req.Description == "" {
		writeError(w, http.StatusBadRequest, "description is required")
		return
	}
	if len(req.Description) > feedbackCenterMaxDescription {
		writeError(w, http.StatusBadRequest, "description too long")
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	// Reuse the legacy hourly per-user submission rate limit.
	count, err := h.Queries.CountRecentFeedbackByUser(r.Context(), parseUUID(userID))
	if err != nil {
		slog.Warn("count recent feedback failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to check rate limit")
		return
	}
	if count >= feedbackHourlyRateLimit {
		writeError(w, http.StatusTooManyRequests, "too many feedback submissions, please try again later")
		return
	}

	platform, version, clientOS := middleware.ClientMetadataFromContext(r.Context())
	metadata := map[string]any{
		"platform":   platform,
		"version":    version,
		"os":         clientOS,
		"user_agent": r.UserAgent(),
	}
	metaBytes, err := json.Marshal(metadata)
	if err != nil {
		metaBytes = []byte("{}")
	}

	fb, err := h.Queries.CreateFeedback(r.Context(), db.CreateFeedbackParams{
		CreatorID:   parseUUID(userID),
		Title:       req.Title,
		Type:        req.Type,
		Description: req.Description,
		Metadata:    metaBytes,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		slog.Warn("create center feedback failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to submit feedback")
		return
	}

	slog.Info("feedback center submission", append(logger.RequestAttrs(r), "feedback_id", uuidToString(fb.ID))...)

	obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.FeedbackSubmitted(
		userID,
		uuidToString(fb.WorkspaceID),
		req.Type,
		len(req.Description),
		feedbackImageRegex.MatchString(req.Description),
		platform,
		version,
	))

	writeJSON(w, http.StatusCreated, FeedbackSummary{
		ID:           uuidToString(fb.ID),
		WorkspaceID:  uuidToString(fb.WorkspaceID),
		CreatorID:    uuidToString(fb.CreatorID),
		CreatorName:  "",
		Title:        fb.Title,
		Description:  fb.Description,
		Type:         fb.Type,
		VoteCount:    0,
		CommentCount: 0,
		MyVote:       false,
		CreatedAt:    timestampToString(fb.CreatedAt),
		UpdatedAt:    timestampToString(fb.UpdatedAt),
	})
}

// feedbackInWorkspace returns true when the feedback exists in the given
// workspace. All vote / comment endpoints gate on this so a foreign feedback
// id is a 404 rather than a cross-workspace write.
func (h *Handler) feedbackInWorkspace(w http.ResponseWriter, r *http.Request, feedbackID, workspaceID pgtype.UUID) bool {
	_, err := h.Queries.GetFeedbackInWorkspace(r.Context(), db.GetFeedbackInWorkspaceParams{
		ID:          feedbackID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "feedback not found")
			return false
		}
		slog.Warn("load feedback for vote/comment failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load feedback")
		return false
	}
	return true
}

// CreateFeedbackVote adds a vote from the current user to a feedback.
// Idempotent: a duplicate vote is a no-op (UNIQUE(feedback_id, user_id) +
// ON CONFLICT DO NOTHING).
func (h *Handler) CreateFeedbackVote(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	id := chi.URLParam(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "feedback id")
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	if !h.feedbackInWorkspace(w, r, idUUID, wsUUID) {
		return
	}

	if err := h.Queries.CreateFeedbackVote(r.Context(), db.CreateFeedbackVoteParams{
		FeedbackID: idUUID,
		UserID:     parseUUID(userID),
	}); err != nil {
		slog.Warn("create feedback vote failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to vote")
		return
	}

	count, err := h.Queries.CountFeedbackVotes(r.Context(), idUUID)
	if err != nil {
		slog.Warn("count feedback votes failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to vote")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"voted": true, "vote_count": count})
}

// DeleteFeedbackVote removes the current user's vote from a feedback.
// Idempotent: removing a nonexistent vote is a no-op.
func (h *Handler) DeleteFeedbackVote(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	id := chi.URLParam(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "feedback id")
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	if !h.feedbackInWorkspace(w, r, idUUID, wsUUID) {
		return
	}

	if err := h.Queries.DeleteFeedbackVote(r.Context(), db.DeleteFeedbackVoteParams{
		FeedbackID: idUUID,
		UserID:     parseUUID(userID),
	}); err != nil {
		slog.Warn("delete feedback vote failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to remove vote")
		return
	}

	count, err := h.Queries.CountFeedbackVotes(r.Context(), idUUID)
	if err != nil {
		slog.Warn("count feedback votes failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to remove vote")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"voted": false, "vote_count": count})
}

// ListFeedbackComments returns the comments for a feedback in chronological
// order, including author display info.
func (h *Handler) ListFeedbackComments(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "feedback id")
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	if !h.feedbackInWorkspace(w, r, idUUID, wsUUID) {
		return
	}

	rows, err := h.Queries.ListFeedbackComments(r.Context(), idUUID)
	if err != nil {
		slog.Warn("list feedback comments failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load comments")
		return
	}

	total, err := h.Queries.CountFeedbackComments(r.Context(), idUUID)
	if err != nil {
		slog.Warn("count feedback comments failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load comments")
		return
	}

	items := make([]FeedbackCommentResponse, len(rows))
	for i, row := range rows {
		items[i] = FeedbackCommentResponse{
			ID:            uuidToString(row.ID),
			FeedbackID:    uuidToString(row.FeedbackID),
			UserID:        uuidToString(row.UserID),
			UserName:      row.UserName,
			UserAvatarURL: textToPtr(row.UserAvatarUrl),
			Content:       row.Content,
			CreatedAt:     timestampToString(row.CreatedAt),
			UpdatedAt:     timestampToString(row.UpdatedAt),
		}
	}

	writeJSON(w, http.StatusOK, FeedbackCommentListResponse{Items: items, Total: total})
}

type CreateFeedbackCommentRequest struct {
	Content string `json:"content"`
}

// CreateFeedbackComment posts a comment on a feedback. Requires login.
func (h *Handler) CreateFeedbackComment(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	id := chi.URLParam(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "feedback id")
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	if !h.feedbackInWorkspace(w, r, idUUID, wsUUID) {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, feedbackBodyLimit)
	var req CreateFeedbackCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeError(w, http.StatusBadRequest, "comment content is required")
		return
	}
	if len(content) > feedbackCenterMaxComment {
		writeError(w, http.StatusBadRequest, "comment too long")
		return
	}

	comment, err := h.Queries.CreateFeedbackComment(r.Context(), db.CreateFeedbackCommentParams{
		FeedbackID: idUUID,
		UserID:     parseUUID(userID),
		Content:    content,
	})
	if err != nil {
		slog.Warn("create feedback comment failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to post comment")
		return
	}

	// Hydrate author display info for the created comment.
	resp := FeedbackCommentResponse{
		ID:         uuidToString(comment.ID),
		FeedbackID: uuidToString(comment.FeedbackID),
		UserID:     uuidToString(comment.UserID),
		Content:    comment.Content,
		CreatedAt:  timestampToString(comment.CreatedAt),
		UpdatedAt:  timestampToString(comment.UpdatedAt),
	}
	if users, err := h.Queries.GetUsersByIDs(r.Context(), []pgtype.UUID{comment.UserID}); err == nil && len(users) > 0 {
		resp.UserName = users[0].Name
		if users[0].AvatarUrl.Valid {
			url := users[0].AvatarUrl.String
			resp.UserAvatarURL = &url
		}
	}

	writeJSON(w, http.StatusCreated, resp)
}
