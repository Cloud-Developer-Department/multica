package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/logger"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// feedbackImageRegex is a coarse check for markdown image syntax ![alt](url).
// It exists only to set the `has_images` analytics flag — we don't need a
// full markdown parser; a false positive on a literal "![" in prose is
// acceptable for a support-triage signal.
var feedbackImageRegex = regexp.MustCompile(`!\[[^\]]*\]\([^)]+\)`)

const (
	feedbackMaxMessageLen                = 10000
	feedbackHourlyRateLimit              = 10
	desktopRouteErrorFeedbackContextKind = "desktop_route_error"
	// feedbackBodyLimit caps the request body at 64 KiB. Message is capped at
	// 10k chars separately; the extra budget covers JSON overhead, optional
	// request fields, and diagnostic context without letting an authenticated
	// client POST megabytes of junk into the metadata JSONB column.
	feedbackBodyLimit = 64 * 1024
)

type FeedbackErrorContext struct {
	Name    string `json:"name"`
	Message string `json:"message"`
	Stack   string `json:"stack,omitempty"`
}

type FeedbackContext struct {
	Kind    string               `json:"kind"`
	Trigger string               `json:"trigger"`
	Error   FeedbackErrorContext `json:"error"`
}

type CreateFeedbackRequest struct {
	Message string `json:"message"`
	URL     string `json:"url"`
	// Kind is the coarse category the feedback picker stamps. The metric
	// label `multica_feedback_submitted_total{kind=...}` reads it via the
	// fixed allow-list in metrics.NormalizeFeedbackKind ("bug", "feature",
	// "general", "praise"); anything outside collapses to "other". Empty /
	// missing falls back to "general" so legacy clients that don't send the
	// field don't blackhole the metric.
	Kind        string           `json:"kind"`
	WorkspaceID *string          `json:"workspace_id,omitempty"`
	Context     *FeedbackContext `json:"context,omitempty"`
}

type FeedbackResponse struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
}

func (h *Handler) CreateFeedback(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, feedbackBodyLimit)
	var req CreateFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	message := strings.TrimSpace(req.Message)
	if message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}
	if len(message) > feedbackMaxMessageLen {
		writeError(w, http.StatusBadRequest, "message too long")
		return
	}
	if !validFeedbackContext(req.Context) {
		writeError(w, http.StatusBadRequest, "invalid feedback context")
		return
	}

	// Per-user rate limit: hourly cap on feedback submissions. DB-backed so it
	// survives process restarts and works across multiple instances without a
	// shared cache — cost is one cheap indexed count per submit.
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
		"url":        req.URL,
		"platform":   platform,
		"version":    version,
		"os":         clientOS,
		"user_agent": r.UserAgent(),
	}
	if req.Context != nil {
		metadata["context"] = req.Context
	}
	metaBytes, err := json.Marshal(metadata)
	if err != nil {
		// The map contains only known JSON-compatible values, but fall through
		// with an empty object rather than 500ing on non-critical metadata.
		metaBytes = []byte("{}")
	}

	var workspaceID pgtype.UUID
	if req.WorkspaceID != nil && *req.WorkspaceID != "" {
		ws, ok := parseUUIDOrBadRequest(w, *req.WorkspaceID, "workspace_id")
		if !ok {
			return
		}
		workspaceID = ws
	}

	fb, err := h.Queries.CreateFeedback(r.Context(), db.CreateFeedbackParams{
		CreatorID:   parseUUID(userID),
		Title:       feedbackTitleFromMessage(message),
		Type:        feedbackTypeFromLegacyKind(req.Kind),
		Description: message,
		Metadata:    metaBytes,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		slog.Warn("create feedback failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to submit feedback")
		return
	}

	slog.Info("feedback submitted", append(logger.RequestAttrs(r), "feedback_id", uuidToString(fb.ID))...)

	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = "general"
	}

	obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.FeedbackSubmitted(
		userID,
		uuidToString(fb.WorkspaceID),
		kind,
		len(message),
		feedbackImageRegex.MatchString(message),
		platform,
		version,
	))

	writeJSON(w, http.StatusCreated, FeedbackResponse{
		ID:        uuidToString(fb.ID),
		CreatedAt: timestampToString(fb.CreatedAt),
	})
}

func validFeedbackContext(context *FeedbackContext) bool {
	if context == nil {
		return true
	}
	return context.Kind == desktopRouteErrorFeedbackContextKind &&
		strings.TrimSpace(context.Trigger) != "" &&
		strings.TrimSpace(context.Error.Name) != "" &&
		strings.TrimSpace(context.Error.Message) != ""
}

// feedbackTitleFromMessage derives a title for the legacy message-only
// submission path (desktop route-error reporting). The Feedback Center schema
// requires a non-empty title; truncate the message or fall back to a
// placeholder so legacy writes stay valid. Truncation is rune-aware: slicing
// by bytes would split multi-byte UTF-8 (e.g. CJK) runes in half and produce
// an invalid title that PostgreSQL rejects.
func feedbackTitleFromMessage(message string) string {
	if strings.TrimSpace(message) == "" {
		return "(no title)"
	}
	const titleMax = 80
	runes := []rune(message)
	if len(runes) <= titleMax {
		return message
	}
	return string(runes[:titleMax])
}

// feedbackTypeFromLegacyKind maps the legacy feedback kind picker
// (bug/feature/general/praise) onto the Feedback Center type enum
// (bug/feature/improvement/other). "general"/"praise" have no v1 counterpart
// and collapse to "other"; unknown values also fall back to "other".
func feedbackTypeFromLegacyKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "bug":
		return "bug"
	case "feature":
		return "feature"
	case "improvement":
		return "improvement"
	default:
		return "other"
	}
}
