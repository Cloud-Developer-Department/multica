package handler

import (
	"encoding/json"
	"errors"
	"fmt"
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

// issueDocumentSortColumns maps the whitelisted `sort` query values to their
// SQL expressions. Only these fixed strings are ever interpolated into the
// ORDER BY clause — client input is rejected unless it names one of them, so
// there is no SQL-injection surface (mirrors ListIssues).
var issueDocumentSortColumns = map[string]string{
	"updated_at": "d.updated_at",
	"title":      "LOWER(d.title)",
	"type":       "d.type",
	"status":     "d.status",
	"version":    "d.version",
}

// ListIssueDocuments returns the paginated issue-flow document list for the
// current workspace, filtered by type / status / issue_id and keyword `q`.
//
// Sorting is server-side (`sort` / `order` query params, whitelisted above):
// the SQL orders before LIMIT/OFFSET, so a non-default sort stays stable
// across pages instead of the client re-sorting just the loaded window
// (CLO-283 R1). The query is built dynamically like ListIssues because the
// sort column is caller-chosen.
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

	// Sort column + direction, whitelisted. Default stays updated_at DESC so
	// callers that predate the sort params keep today's behaviour.
	sortExpr := issueDocumentSortColumns["updated_at"]
	if raw := strings.TrimSpace(r.URL.Query().Get("sort")); raw != "" {
		expr, ok := issueDocumentSortColumns[raw]
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid sort value")
			return
		}
		sortExpr = expr
	}
	sortDir := "DESC"
	if raw := strings.TrimSpace(r.URL.Query().Get("order")); raw != "" {
		switch strings.ToLower(raw) {
		case "asc":
			sortDir = "ASC"
		case "desc":
			sortDir = "DESC"
		default:
			writeError(w, http.StatusBadRequest, "invalid order value")
			return
		}
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

	// Build WHERE + ORDER BY dynamically. `w.issue_prefix` powers the
	// identifier match in the q search (CLO-283 R4): typing "CLO-279" must hit
	// the document whose issue identifier is CLO-279.
	where := []string{"d.workspace_id = $1"}
	args := []any{wsUUID}
	addArg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if typeFilter.Valid {
		where = append(where, "d.type = "+addArg(typeFilter))
	}
	if statusFilter.Valid {
		where = append(where, "d.status = "+addArg(statusFilter))
	}
	if issueIDFilter.Valid {
		where = append(where, "d.issue_id = "+addArg(issueIDFilter))
	}
	if qFilter.Valid {
		qRef := addArg(qFilter)
		where = append(where, fmt.Sprintf(`(
    LOWER(d.title) LIKE '%%' || LOWER(%s) || '%%'
 OR LOWER(i.title) LIKE '%%' || LOWER(%s) || '%%'
 OR CAST(i.number AS TEXT) LIKE LOWER(%s) || '%%'
 OR LOWER(w.issue_prefix || '-' || CAST(i.number AS TEXT)) LIKE '%%' || LOWER(%s) || '%%'
 OR LOWER(COALESCE(u.name, a.name)) LIKE '%%' || LOWER(%s) || '%%'
)`, qRef, qRef, qRef, qRef, qRef))
	}
	whereSql := strings.Join(where, " AND ")

	// d.id is the final tiebreaker so two rows sharing the sort key keep a
	// stable order across requests (same reasoning as ListIssues).
	orderBy := sortExpr + " " + sortDir + ", d.id DESC"

	offsetRef := addArg(int64(offset))
	limitRef := addArg(int64(limit))

	const selectBody = `FROM issue_document d
JOIN issue i ON i.id = d.issue_id
JOIN workspace w ON w.id = d.workspace_id
LEFT JOIN member m ON m.id = d.author_id AND d.author_type = 'member'
LEFT JOIN "user" u ON u.id = m.user_id
LEFT JOIN agent a ON a.id = d.author_id AND d.author_type = 'agent'
WHERE %s`

	query := fmt.Sprintf(`SELECT d.id, d.workspace_id, d.issue_id, d.type, d.title, d.content_type,
       d.file_attachment_id, d.version, d.status, d.author_type, d.author_id,
       d.created_at, d.updated_at,
       i.number AS issue_number, i.title AS issue_title,
       u.name AS member_author_name, a.name AS agent_author_name
`+selectBody+`
ORDER BY %s
LIMIT %s OFFSET %s`, whereSql, orderBy, limitRef, offsetRef)

	rows, err := h.DB.Query(r.Context(), query, args...)
	if err != nil {
		slog.Warn("ListIssueDocuments query failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list issue documents")
		return
	}
	defer rows.Close()

	var items []db.ListIssueDocumentsRow
	for rows.Next() {
		var row db.ListIssueDocumentsRow
		if err := rows.Scan(
			&row.ID, &row.WorkspaceID, &row.IssueID, &row.Type, &row.Title,
			&row.ContentType, &row.FileAttachmentID, &row.Version, &row.Status,
			&row.AuthorType, &row.AuthorID, &row.CreatedAt, &row.UpdatedAt,
			&row.IssueNumber, &row.IssueTitle, &row.MemberAuthorName, &row.AgentAuthorName,
		); err != nil {
			slog.Warn("ListIssueDocuments scan failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to list issue documents")
			return
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("ListIssueDocuments rows failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list issue documents")
		return
	}

	// Total for pagination; same WHERE minus the OFFSET/LIMIT args appended last.
	countQuery := fmt.Sprintf(`SELECT COUNT(*) `+selectBody, whereSql)
	var total int64
	if err := h.DB.QueryRow(r.Context(), countQuery, args[:len(args)-2]...).Scan(&total); err != nil {
		total = int64(len(items))
	}

	issuePrefix := h.getIssuePrefix(r.Context(), wsUUID)
	resp := make([]IssueDocumentResponse, len(items))
	for i, row := range items {
		resp[i] = issueDocumentRowToResponse(row, issuePrefix)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": resp, "total": total})
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
// `file_attachment_id`. The request body cap adds slack for the JSON envelope.
const (
	maxIssueDocumentInlineContentBytes = 5 * 1024 * 1024
	maxIssueDocumentRequestBytes       = 6 * 1024 * 1024
)

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

	// Cap the request body BEFORE decoding (CLO-283 R5, mirrors
	// issue_table_query.go). A huge payload is rejected early instead of being
	// buffered and only caught after the 5MB content check.
	r.Body = http.MaxBytesReader(w, r.Body, maxIssueDocumentRequestBytes)
	var req CreateIssueDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body is too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Strip bytes PostgreSQL's TEXT column rejects before the size check. An
	// embedded NUL (0x00, SQLSTATE 22021) in CLI --content-file payloads
	// otherwise fails the INSERT with an opaque 500 (CLO-283 R2, GH #5388
	// precedent — CreateComment does the same).
	req.Content = sanitizeNullBytes(req.Content)
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
		if errors.Is(err, service.ErrIssueDocumentVersionConflict) {
			writeError(w, http.StatusConflict, "issue document version conflict; retry the submission")
			return
		}
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
