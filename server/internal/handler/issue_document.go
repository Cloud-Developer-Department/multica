package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IssueDocumentResponse is the JSON shape for one issue-flow document row in
// the Issue Documents list (CLO-278). It intentionally omits `content` — the
// list stays light; detail is fetched on demand.
type IssueDocumentResponse struct {
	ID               string  `json:"id"`
	WorkspaceID      string  `json:"workspace_id"`
	IssueID          string  `json:"issue_id"`
	IssueIdentifier  string  `json:"issue_identifier"`
	IssueTitle       string  `json:"issue_title"`
	Type             string  `json:"type"`
	Title            string  `json:"title"`
	Version          int32   `json:"version"`
	Status           string  `json:"status"`
	ContentType      string  `json:"content_type"`
	FileAttachmentID *string `json:"file_attachment_id"`
	AuthorType       string  `json:"author_type"`
	AuthorID         string  `json:"author_id"`
	AuthorName       string  `json:"author_name"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

// IssueDocumentDetailResponse extends the list shape with the document body.
// `content` is populated for markdown/json/text documents; file documents carry
// only `file_attachment_id` and are served through the attachment pipeline.
type IssueDocumentDetailResponse struct {
	IssueDocumentResponse
	Content *string `json:"content"`
}

// IssueDocumentVersionResponse is one row of the version history for a single
// (issue, type) document family.
type IssueDocumentVersionResponse struct {
	ID               string  `json:"id"`
	Version          int32   `json:"version"`
	Status           string  `json:"status"`
	Title            string  `json:"title"`
	ContentType      string  `json:"content_type"`
	FileAttachmentID *string `json:"file_attachment_id"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

// validIssueDocumentTypes mirrors the issue_document.type CHECK constraint.
var validIssueDocumentTypes = map[string]bool{
	"requirements":  true,
	"architecture":  true,
	"development":   true,
	"testing":       true,
	"code_review":   true,
	"security":      true,
	"documentation": true,
	"deployment":    true,
	"other":         true,
}

// validIssueDocumentStatuses mirrors the issue_document.status CHECK constraint.
var validIssueDocumentStatuses = map[string]bool{
	"draft":      true,
	"submitted":  true,
	"approved":   true,
	"rejected":   true,
	"superseded": true,
}

// validIssueDocumentContentTypes mirrors the issue_document.content_type CHECK
// constraint.
var validIssueDocumentContentTypes = map[string]bool{
	"markdown": true,
	"json":     true,
	"text":     true,
	"file":     true,
}

func issueDocumentRowToResponse(row db.ListIssueDocumentsRow, issuePrefix string) IssueDocumentResponse {
	authorName := ""
	if row.AuthorType == "member" {
		authorName = row.MemberAuthorName.String
	} else if row.AuthorType == "agent" {
		authorName = row.AgentAuthorName.String
	}
	return IssueDocumentResponse{
		ID:               uuidToString(row.ID),
		WorkspaceID:      uuidToString(row.WorkspaceID),
		IssueID:          uuidToString(row.IssueID),
		IssueIdentifier:  issuePrefix + "-" + strconv.Itoa(int(row.IssueNumber)),
		IssueTitle:       row.IssueTitle,
		Type:             row.Type,
		Title:            row.Title,
		Version:          row.Version,
		Status:           row.Status,
		ContentType:      row.ContentType,
		FileAttachmentID: uuidToPtr(row.FileAttachmentID),
		AuthorType:       row.AuthorType,
		AuthorID:         uuidToString(row.AuthorID),
		AuthorName:       authorName,
		CreatedAt:        timestampToString(row.CreatedAt),
		UpdatedAt:        timestampToString(row.UpdatedAt),
	}
}

func issueDocumentDetailToResponse(row db.GetIssueDocumentDetailRow, issuePrefix string) IssueDocumentDetailResponse {
	summary := IssueDocumentResponse{
		ID:               uuidToString(row.ID),
		WorkspaceID:      uuidToString(row.WorkspaceID),
		IssueID:          uuidToString(row.IssueID),
		IssueIdentifier:  issuePrefix + "-" + strconv.Itoa(int(row.IssueNumber)),
		IssueTitle:       row.IssueTitle,
		Type:             row.Type,
		Title:            row.Title,
		Version:          row.Version,
		Status:           row.Status,
		ContentType:      row.ContentType,
		FileAttachmentID: uuidToPtr(row.FileAttachmentID),
		AuthorType:       row.AuthorType,
		AuthorID:         uuidToString(row.AuthorID),
		CreatedAt:        timestampToString(row.CreatedAt),
		UpdatedAt:        timestampToString(row.UpdatedAt),
	}
	if row.AuthorType == "member" {
		summary.AuthorName = row.MemberAuthorName.String
	} else if row.AuthorType == "agent" {
		summary.AuthorName = row.AgentAuthorName.String
	}
	return IssueDocumentDetailResponse{
		IssueDocumentResponse: summary,
		Content:               textToPtr(row.Content),
	}
}

func issueDocumentVersionToResponse(row db.ListIssueDocumentVersionsRow) IssueDocumentVersionResponse {
	return IssueDocumentVersionResponse{
		ID:               uuidToString(row.ID),
		Version:          row.Version,
		Status:           row.Status,
		Title:            row.Title,
		ContentType:      row.ContentType,
		FileAttachmentID: uuidToPtr(row.FileAttachmentID),
		CreatedAt:        timestampToString(row.CreatedAt),
		UpdatedAt:        timestampToString(row.UpdatedAt),
	}
}

const (
	defaultIssueDocumentLimit = 50
	maxIssueDocumentLimit     = 100
)

// ListIssueDocuments returns the paginated issue-flow document list for the
// current workspace, filtered by type / status / issue_id and keyword `q`.
func (h *Handler) ListIssueDocuments(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	limit := defaultIssueDocumentLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		if n > maxIssueDocumentLimit {
			n = maxIssueDocumentLimit
		}
		limit = n
	}
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid offset")
			return
		}
		offset = n
	}

	var typeFilter pgtype.Text
	if raw := strings.TrimSpace(r.URL.Query().Get("type")); raw != "" {
		if !validIssueDocumentTypes[raw] {
			writeError(w, http.StatusBadRequest, "invalid document type")
			return
		}
		typeFilter = strToText(raw)
	}
	var statusFilter pgtype.Text
	if raw := strings.TrimSpace(r.URL.Query().Get("status")); raw != "" {
		if !validIssueDocumentStatuses[raw] {
			writeError(w, http.StatusBadRequest, "invalid document status")
			return
		}
		statusFilter = strToText(raw)
	}
	var issueIDFilter pgtype.UUID
	if raw := strings.TrimSpace(r.URL.Query().Get("issue_id")); raw != "" {
		u, err := util.ParseUUID(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid issue_id")
			return
		}
		issueIDFilter = u
	}
	var qFilter pgtype.Text
	if raw := strings.TrimSpace(r.URL.Query().Get("q")); raw != "" {
		qFilter = strToText(raw)
	}

	params := db.ListIssueDocumentsParams{
		WorkspaceID: wsUUID,
		Limit:       int32(limit),
		Offset:      int32(offset),
		Type:        typeFilter,
		Status:      statusFilter,
		IssueID:     issueIDFilter,
		Q:           qFilter,
	}
	rows, err := h.Queries.ListIssueDocuments(r.Context(), params)
	if err != nil {
		slog.Warn("ListIssueDocuments failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list issue documents")
		return
	}
	count, err := h.Queries.CountIssueDocuments(r.Context(), db.CountIssueDocumentsParams{
		WorkspaceID: wsUUID,
		Type:        typeFilter,
		Status:      statusFilter,
		IssueID:     issueIDFilter,
		Q:           qFilter,
	})
	if err != nil {
		slog.Warn("CountIssueDocuments failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list issue documents")
		return
	}

	issuePrefix := h.getIssuePrefix(r.Context(), wsUUID)
	items := make([]IssueDocumentResponse, len(rows))
	for i, row := range rows {
		items[i] = issueDocumentRowToResponse(row, issuePrefix)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": count})
}

// GetIssueDocument returns a single document's detail (metadata + body).
func (h *Handler) GetIssueDocument(w http.ResponseWriter, r *http.Request) {
	documentID := chi.URLParam(r, "documentId")
	workspaceID := h.resolveWorkspaceID(r)
	docUUID, ok := parseUUIDOrBadRequest(w, documentID, "document id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	row, err := h.Queries.GetIssueDocumentDetail(r.Context(), db.GetIssueDocumentDetailParams{
		ID:          docUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "issue document not found")
			return
		}
		slog.Warn("GetIssueDocument failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to get issue document")
		return
	}
	issuePrefix := h.getIssuePrefix(r.Context(), wsUUID)
	writeJSON(w, http.StatusOK, issueDocumentDetailToResponse(row, issuePrefix))
}

// ListIssueDocumentVersions returns the version history for the (issue, type)
// family the requested document belongs to.
func (h *Handler) ListIssueDocumentVersions(w http.ResponseWriter, r *http.Request) {
	documentID := chi.URLParam(r, "documentId")
	workspaceID := h.resolveWorkspaceID(r)
	docUUID, ok := parseUUIDOrBadRequest(w, documentID, "document id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	doc, err := h.Queries.GetIssueDocument(r.Context(), db.GetIssueDocumentParams{
		ID:          docUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "issue document not found")
			return
		}
		slog.Warn("ListIssueDocumentVersions failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list issue document versions")
		return
	}
	rows, err := h.Queries.ListIssueDocumentVersions(r.Context(), db.ListIssueDocumentVersionsParams{
		WorkspaceID: wsUUID,
		IssueID:     doc.IssueID,
		Type:        doc.Type,
	})
	if err != nil {
		slog.Warn("ListIssueDocumentVersions failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list issue document versions")
		return
	}
	items := make([]IssueDocumentVersionResponse, len(rows))
	for i, row := range rows {
		items[i] = issueDocumentVersionToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issue_id": uuidToString(doc.IssueID),
		"type":     doc.Type,
		"items":    items,
	})
}

// CreateIssueDocumentRequest is the body for registering a new version of an
// issue-flow document. `content` and `file_attachment_id` are mutually
// exclusive: inline text for markdown/json/text documents, an attachment id
// (from /api/upload-file) for file documents.
type CreateIssueDocumentRequest struct {
	IssueID          string  `json:"issue_id"`
	Type             string  `json:"type"`
	Title            string  `json:"title"`
	Content          string  `json:"content"`
	ContentType      string  `json:"content_type"`
	FileAttachmentID *string `json:"file_attachment_id"`
	Status           *string `json:"status"`
}

// maxIssueDocumentInlineContentBytes caps inline document bodies. Larger
// documents should be uploaded as files and registered via
// `file_attachment_id`.
const maxIssueDocumentInlineContentBytes = 5 * 1024 * 1024

// CreateIssueDocument registers a new version of an issue-flow document. The
// previous versions of the same (issue_id, type) are marked `superseded`
// atomically (see IssueDocumentService.Submit). This endpoint is the write
// channel used by the CLI / future flow agents; the Issue Documents page itself
// is read-only.
func (h *Handler) CreateIssueDocument(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	var req CreateIssueDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.IssueID) == "" {
		writeError(w, http.StatusBadRequest, "issue_id is required")
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	docType := strings.TrimSpace(req.Type)
	if !validIssueDocumentTypes[docType] {
		writeError(w, http.StatusBadRequest, "invalid document type")
		return
	}
	contentType := strings.TrimSpace(req.ContentType)
	if contentType == "" {
		contentType = "markdown"
	}
	if !validIssueDocumentContentTypes[contentType] {
		writeError(w, http.StatusBadRequest, "invalid content_type")
		return
	}
	status := "submitted"
	if req.Status != nil {
		status = strings.TrimSpace(*req.Status)
		if !validIssueDocumentStatuses[status] {
			writeError(w, http.StatusBadRequest, "invalid status")
			return
		}
	}
	if contentType == "file" {
		if req.FileAttachmentID == nil || strings.TrimSpace(*req.FileAttachmentID) == "" {
			writeError(w, http.StatusBadRequest, "file_attachment_id is required for file documents")
			return
		}
	} else {
		if len(req.Content) > maxIssueDocumentInlineContentBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "document content is too large")
			return
		}
	}

	// The issue must belong to the current workspace; loadIssueForUser resolves
	// both identifier and UUID forms and enforces workspace scope.
	issue, ok := h.loadIssueForUser(w, r, req.IssueID)
	if !ok {
		return
	}

	var fileAttachmentID pgtype.UUID
	if req.FileAttachmentID != nil && strings.TrimSpace(*req.FileAttachmentID) != "" {
		u, err := util.ParseUUID(*req.FileAttachmentID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid file_attachment_id")
			return
		}
		fileAttachmentID = u
	}

	actorType, actorID := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID))
	authorType := "member"
	var authorID pgtype.UUID
	switch actorType {
	case "agent":
		authorType = "agent"
		agentUUID, err := util.ParseUUID(actorID)
		if err != nil {
			writeError(w, http.StatusForbidden, "invalid agent identity")
			return
		}
		authorID = agentUUID
	case "member":
		member, err := h.getWorkspaceMember(r.Context(), userID, uuidToString(issue.WorkspaceID))
		if err != nil {
			writeError(w, http.StatusForbidden, "workspace member not found")
			return
		}
		authorID = member.ID
	}

	doc, err := h.IssueDocumentService.Submit(r.Context(), service.SubmitIssueDocumentParams{
		WorkspaceID:      wsUUID,
		IssueID:          issue.ID,
		Type:             docType,
		Title:            strings.TrimSpace(req.Title),
		Content:          req.Content,
		ContentType:      contentType,
		FileAttachmentID: fileAttachmentID,
		Status:           status,
		AuthorType:       authorType,
		AuthorID:         authorID,
	})
	if err != nil {
		slog.Warn("CreateIssueDocument failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create issue document")
		return
	}

	detail, err := h.Queries.GetIssueDocumentDetail(r.Context(), db.GetIssueDocumentDetailParams{
		ID:          doc.ID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "issue document not found")
			return
		}
		slog.Warn("CreateIssueDocument: detail readback failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create issue document")
		return
	}
	issuePrefix := h.getIssuePrefix(r.Context(), wsUUID)
	writeJSON(w, http.StatusCreated, issueDocumentDetailToResponse(detail, issuePrefix))
}
