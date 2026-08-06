package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// maxInlineContentBytes caps the text content that may travel inline in the
// artifact JSON body (V-03 security audit). Anything larger must go through
// the attachment upload channel instead (which is bounded by maxUploadSize).
const maxInlineContentBytes = 5 << 20 // 5 MB

// artifactResponse is the artifact object emitted on artifact APIs. Full
// `content` is only included when the requesting actor is a participant of the
// artifact's node (V-04 security audit); everyone else sees the metadata with
// content omitted.
func artifactResponse(a db.Artifact, includeContent bool) map[string]any {
	resp := map[string]any{
		"id":                 uuidToString(a.ID),
		"workspace_id":       uuidToString(a.WorkspaceID),
		"workflow_id":        uuidToString(a.WorkflowID),
		"node_id":            uuidToString(a.NodeID),
		"issue_id":           uuidToPtr(a.IssueID),
		"type":               a.Type,
		"title":              a.Title,
		"content_type":       a.ContentType,
		"file_attachment_id": uuidToPtr(a.FileAttachmentID),
		"version":            a.Version,
		"status":             a.Status,
		"author_type":        a.AuthorType,
		"author_id":          uuidToString(a.AuthorID),
		"created_at":         timestampToString(a.CreatedAt),
		"updated_at":         timestampToString(a.UpdatedAt),
	}
	if includeContent {
		resp["content"] = textToPtr(a.Content)
	}
	return resp
}

// canReadArtifactContent reports whether the requesting actor may read the full
// content of an artifact (V-04 security audit). Allowed: the workspace
// owner/admin, the artifact's author, the workflow creator, the node's
// assignee (or a member of a squad-assigned node), the node's mapped issue
// assignee, and the reviewer (the source_issue creator).
func (h *Handler) canReadArtifactContent(r *http.Request, workspaceID pgtype.UUID, a db.Artifact) bool {
	userID := requestUserID(r)
	if userID == "" {
		return false
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(workspaceID))
	if actorID == "" {
		return false
	}
	actorUUID := parseUUID(actorID)

	if actorType == "member" {
		member, err := h.getWorkspaceMember(r.Context(), actorID, uuidToString(workspaceID))
		if err == nil && (member.Role == "owner" || member.Role == "admin") {
			return true
		}
	}
	if a.AuthorType == actorType && a.AuthorID.Valid && a.AuthorID == actorUUID {
		return true
	}
	wf, err := h.Queries.GetWorkflow(r.Context(), db.GetWorkflowParams{ID: a.WorkflowID, WorkspaceID: workspaceID})
	if err != nil {
		return false
	}
	if wf.CreatedByType == actorType && wf.CreatedByID.Valid && wf.CreatedByID == actorUUID {
		return true
	}
	node, err := h.Queries.GetWorkflowNode(r.Context(), db.GetWorkflowNodeParams{ID: a.NodeID, WorkflowID: a.WorkflowID})
	if err != nil {
		return false
	}
	if node.AssigneeType.Valid && node.AssigneeID.Valid && node.AssigneeType.String != "" {
		if h.actorMatchesAssignee(r.Context(), actorType, actorUUID, node.AssigneeType.String, node.AssigneeID) {
			return true
		}
	}
	if node.IssueID.Valid {
		issue, ierr := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{ID: node.IssueID, WorkspaceID: workspaceID})
		if ierr == nil && issue.AssigneeType.Valid && issue.AssigneeID.Valid && issue.AssigneeType.String != "" {
			if h.actorMatchesAssignee(r.Context(), actorType, actorUUID, issue.AssigneeType.String, issue.AssigneeID) {
				return true
			}
		}
	}
	// Reviewer: the source_issue creator (Q3).
	source, err := h.Queries.GetIssue(r.Context(), wf.SourceIssueID)
	if err != nil {
		return false
	}
	return source.CreatorType == "member" && uuidToString(source.CreatorID) == actorID
}

// actorMatchesAssignee reports whether the (authorType, authorID) actor is the
// assigned actor for (assigneeType, assigneeID), resolving squad membership.
func (h *Handler) actorMatchesAssignee(ctx context.Context, actorType string, actorID pgtype.UUID, assigneeType string, assigneeID pgtype.UUID) bool {
	if assigneeType == "squad" {
		ok, err := h.Queries.IsSquadMember(ctx, db.IsSquadMemberParams{
			SquadID:    assigneeID,
			MemberType: actorType,
			MemberID:   actorID,
		})
		return err == nil && ok
	}
	return actorType == assigneeType && actorID == assigneeID
}

// CreateArtifact handles POST /api/artifacts (Agent submission, FR3.3).
func (h *Handler) CreateArtifact(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req struct {
		WorkflowID       *string `json:"workflow_id"`
		NodeID           *string `json:"node_id"`
		IssueID          *string `json:"issue_id"`
		Type             *string `json:"type"`
		Title            *string `json:"title"`
		Content          string  `json:"content"`
		ContentType      *string `json:"content_type"`
		FileAttachmentID *string `json:"file_attachment_id"`
	}
	// V-03 (security audit): the inline text channel previously had no request
	// body bound, so a huge file inlined into `content` could exhaust memory /
	// bandwidth (DoS). Bound the body to the same 100 MB as the upload channel
	// and reject oversized inline content explicitly.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "artifact request body too large (max 100MB)")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Content) > maxInlineContentBytes {
		writeError(w, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("artifact content too large to inline (max %d bytes); use the file upload channel instead", maxInlineContentBytes))
		return
	}
	if req.WorkflowID == nil || req.NodeID == nil || req.Type == nil || req.Title == nil || *req.Title == "" {
		writeError(w, http.StatusBadRequest, "workflow_id, node_id, type and title are required")
		return
	}
	wfUUID, ok := parseUUIDOrBadRequest(w, *req.WorkflowID, "workflow_id")
	if !ok {
		return
	}
	nodeUUID, ok := parseUUIDOrBadRequest(w, *req.NodeID, "node_id")
	if !ok {
		return
	}
	contentType := service.ContentTypeMarkdown
	if req.ContentType != nil && *req.ContentType != "" {
		contentType = *req.ContentType
	}
	params := service.CreateArtifactParams{
		WorkspaceID: wsUUID,
		WorkflowID:  wfUUID,
		NodeID:      nodeUUID,
		Type:        *req.Type,
		Title:       *req.Title,
		Content:     req.Content,
		ContentType: contentType,
	}
	if req.IssueID != nil && *req.IssueID != "" {
		if u, err := util.ParseUUID(*req.IssueID); err == nil {
			params.IssueID = u
		}
	}
	if req.FileAttachmentID != nil && *req.FileAttachmentID != "" {
		if u, err := util.ParseUUID(*req.FileAttachmentID); err == nil {
			params.FileAttachmentID = u
		}
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	params.AuthorType = actorType
	params.AuthorID = parseUUID(actorID)

	artifact, err := h.ArtifactService.Submit(r.Context(), params)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, artifactResponse(artifact, true))
}

// ListArtifacts handles GET /api/artifacts.
func (h *Handler) ListArtifacts(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	var filters service.ArtifactListFilters
	if v := r.URL.Query().Get("type"); v != "" {
		filters.Type = &v
	}
	if v := r.URL.Query().Get("status"); v != "" {
		filters.Status = &v
	}
	if v := r.URL.Query().Get("node_id"); v != "" {
		if u, err := util.ParseUUID(v); err == nil {
			filters.NodeID = &u
		}
	}
	if v := r.URL.Query().Get("workflow_id"); v != "" {
		if u, err := util.ParseUUID(v); err == nil {
			filters.WorkflowID = &u
		}
	}
	if v := r.URL.Query().Get("author_id"); v != "" {
		if u, err := util.ParseUUID(v); err == nil {
			filters.AuthorID = &u
		}
	}
	limit, offset := parsePagination(r, 50)
	items, total, err := h.ArtifactService.List(r.Context(), wsUUID, filters, limit, offset)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, a := range items {
		out = append(out, artifactResponse(a, h.canReadArtifactContent(r, wsUUID, a)))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "total": total})
}

// GetArtifact handles GET /api/artifacts/{id}.
func (h *Handler) GetArtifact(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	artifactID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "artifact id")
	if !ok {
		return
	}
	a, err := h.ArtifactService.Get(r.Context(), wsUUID, artifactID)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	// Latest version summary for the artifact's (node, type) lineage.
	node, err := h.WorkflowService.ListNodes(r.Context(), wsUUID, a.WorkflowID, nil, nil)
	reviewRequired := false
	if err == nil {
		for _, n := range node {
			if uuidToString(n.ID) == uuidToString(a.NodeID) {
				reviewRequired = n.ReviewRequired
				break
			}
		}
	}
	_, _, versions, _ := h.ArtifactService.Versions(r.Context(), wsUUID, artifactID)
	var latest any
	if len(versions) > 0 {
		latest = map[string]any{"version": versions[len(versions)-1].Version, "status": versions[len(versions)-1].Status}
	}
	resp := artifactResponse(a, h.canReadArtifactContent(r, wsUUID, a))
	resp["latest_version"] = latest
	resp["review_required"] = reviewRequired
	writeJSON(w, http.StatusOK, resp)
}

// ListArtifactVersions handles GET /api/artifacts/{id}/versions.
func (h *Handler) ListArtifactVersions(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	artifactID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "artifact id")
	if !ok {
		return
	}
	nodeID, artifactType, versions, err := h.ArtifactService.Versions(r.Context(), wsUUID, artifactID)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(versions))
	for _, v := range versions {
		out = append(out, map[string]any{
			"id":         uuidToString(v.ID),
			"version":    v.Version,
			"status":     v.Status,
			"title":      v.Title,
			"content_type": v.ContentType,
			"created_at": timestampToString(v.CreatedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"node_id": uuidToString(nodeID),
		"type":    artifactType,
		"items":   out,
	})
}

// DiffArtifactVersions handles GET /api/artifacts/{id}/diff?from=&to= (Q5).
func (h *Handler) DiffArtifactVersions(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	artifactID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "artifact id")
	if !ok {
		return
	}
	fromVersion := int32(1)
	if v := r.URL.Query().Get("from"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil {
			fromVersion = int32(n)
		}
	}
	toVersion := int32(0)
	if v := r.URL.Query().Get("to"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil {
			toVersion = int32(n)
		}
	}
	// to == 0 means latest version.
	_, _, versions, err := h.ArtifactService.Versions(r.Context(), wsUUID, artifactID)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	if toVersion == 0 && len(versions) > 0 {
		toVersion = versions[len(versions)-1].Version
	}
	// V-04 (security audit): a diff exposes the full artifact content text, so
	// it is restricted to the same participants/reviewers who may read content.
	base, err := h.ArtifactService.Get(r.Context(), wsUUID, artifactID)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	if !h.canReadArtifactContent(r, wsUUID, base) {
		writeError(w, http.StatusForbidden, "not authorized to read artifact content")
		return
	}
	from, _, diff, err := h.ArtifactService.Diff(r.Context(), wsUUID, artifactID, fromVersion, toVersion)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	added, removed := 0, 0
	lines := make([]map[string]any, 0, len(diff))
	for _, d := range diff {
		switch d.Op {
		case "+":
			added++
		case "-":
			removed++
		}
		lines = append(lines, map[string]any{"line": d.Line, "op": d.Op, "text": d.Text})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"artifact_id": uuidToString(artifactID),
		"from_version": fromVersion,
		"to_version":   toVersion,
		"content_type": from.ContentType,
		"diff":         lines,
		"summary":      map[string]any{"added": added, "removed": removed, "changed": 0},
	})
}

// ReviewArtifact handles POST /api/artifacts/{id}/review (Q3).
func (h *Handler) ReviewArtifact(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	artifactID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "artifact id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	// H-1 (security audit): the review gate is human-only. The route carries
	// RequireHumanActor; this handler-level check is defense-in-depth so the
	// guard holds even if the handler is ever mounted without the middleware.
	if !requireHumanActor(w, r) {
		return
	}
	var req struct {
		Action  string `json:"action"`
		Comment string `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Action != "approved" && req.Action != "rejected" {
		writeError(w, http.StatusBadRequest, "action must be approved or rejected")
		return
	}
	// Q3 authorization: reviewer = source_issue creator, or workspace
	// owner/admin override.
	if !h.canReviewArtifact(r, wsUUID, artifactID, userID) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "only the issue creator or a workspace owner/admin can review", "code": "forbidden"})
		return
	}
	reviewerUUID := parseUUID(userID)
	result, err := h.ArtifactService.Review(r.Context(), service.ReviewParams{
		ArtifactID:  artifactID,
		WorkspaceID: wsUUID,
		Action:      req.Action,
		Comment:     req.Comment,
		ReviewerID:  reviewerUUID,
	})
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"artifact_id":     uuidToString(result.Artifact.ID),
		"action":          result.Action,
		"comment":         result.Comment,
		"artifact_status": result.Artifact.Status,
		"node":            map[string]any{"id": uuidToString(result.Node.ID), "status": result.NodeStatus},
		"workflow_status": result.Workflow.Status,
		"advanced":        result.Advanced,
		"next_stage":      result.NextStage,
		"reviewed_at":     timestampToString(result.Review.CreatedAt),
	})
}

// canReviewArtifact implements the Q3 gate: the requesting member is the
// source_issue creator, or a workspace owner/admin.
func (h *Handler) canReviewArtifact(r *http.Request, workspaceID pgtype.UUID, artifactID pgtype.UUID, userID string) bool {
	member, err := h.getWorkspaceMember(r.Context(), userID, uuidToString(workspaceID))
	if err != nil {
		return false
	}
	if member.Role == "owner" || member.Role == "admin" {
		return true
	}
	artifact, err := h.Queries.GetArtifact(r.Context(), db.GetArtifactParams{ID: artifactID, WorkspaceID: workspaceID})
	if err != nil {
		return false
	}
	wf, err := h.Queries.GetWorkflow(r.Context(), db.GetWorkflowParams{ID: artifact.WorkflowID, WorkspaceID: workspaceID})
	if err != nil {
		return false
	}
	source, err := h.Queries.GetIssue(r.Context(), wf.SourceIssueID)
	if err != nil {
		return false
	}
	return source.CreatorType == "member" && uuidToString(source.CreatorID) == userID
}

// ListArtifactReviews handles GET /api/artifacts/{id}/reviews (FR4.6).
func (h *Handler) ListArtifactReviews(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	artifactID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "artifact id")
	if !ok {
		return
	}
	reviews, err := h.ArtifactService.Reviews(r.Context(), wsUUID, artifactID)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(reviews))
	for _, rev := range reviews {
		out = append(out, map[string]any{
			"id":            uuidToString(rev.ID),
			"artifact_id":   uuidToString(rev.ArtifactID),
			"action":        rev.Action,
			"comment":       textToPtr(rev.Comment),
			"reviewer_type": rev.ReviewerType,
			"reviewer_id":   uuidToString(rev.ReviewerID),
			"created_at":    timestampToString(rev.CreatedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// ListReviewQueue handles GET /api/reviews/queue (FR4.1).
func (h *Handler) ListReviewQueue(w http.ResponseWriter, r *http.Request) {
	// V-04 (security audit): the queue leaks workflow/node/author metadata;
	// it is a human reviewer surface. The route carries RequireHumanActor; this
	// handler-level check is defense-in-depth.
	if !requireHumanActor(w, r) {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	limit, offset := parsePagination(r, 50)
	items, total, err := h.ArtifactService.ReviewQueue(r.Context(), wsUUID, limit, offset)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, a := range items {
		entry := map[string]any{
			"artifact_id":     uuidToString(a.ID),
			"workflow_id":     uuidToString(a.WorkflowID),
			"node_id":         uuidToString(a.NodeID),
			"type":            a.Type,
			"title":           a.Title,
			"version":         a.Version,
			"content_type":    a.ContentType,
			"file_attachment_id": uuidToPtr(a.FileAttachmentID),
			"author_type":     a.AuthorType,
			"author_id":       uuidToString(a.AuthorID),
			"created_at":      timestampToString(a.CreatedAt),
		}
		// Enrich with workflow + node names.
		if wf, err := h.Queries.GetWorkflow(r.Context(), db.GetWorkflowParams{ID: a.WorkflowID, WorkspaceID: wsUUID}); err == nil {
			entry["workflow_name"] = wf.Name
		}
		if node, err := h.Queries.GetWorkflowNode(r.Context(), db.GetWorkflowNodeParams{ID: a.NodeID, WorkflowID: a.WorkflowID}); err == nil {
			entry["node_name"] = node.Name
			entry["stage"] = node.Stage
		}
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "total": total})
}

// GetArtifactStats handles GET /api/artifacts/stats (Q6).
func (h *Handler) GetArtifactStats(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	now := time.Now().UTC()
	from := now.AddDate(0, -1, 0)
	to := now
	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			from = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			to = t
		}
	}
	stats, err := h.ArtifactService.Stats(r.Context(), wsUUID, from, to)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	byType := make([]map[string]any, 0, len(stats.ByType))
	for _, ts := range stats.ByType {
		byType = append(byType, map[string]any{
			"type":                  ts.Type,
			"submitted":             ts.Submitted,
			"approval_rate":         ts.ApprovalRate,
			"avg_review_duration_ms": ts.AvgReviewDuration.Milliseconds(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"workspace_id":            stats.WorkspaceID,
		"range":                   map[string]any{"from": stats.From.UTC().Format(time.RFC3339), "to": stats.To.UTC().Format(time.RFC3339)},
		"total_submitted":         stats.Total,
		"approved":                stats.Approved,
		"rejected":                stats.Rejected,
		"pending":                 stats.Pending,
		"approval_rate":           stats.ApprovalRate,
		"avg_review_duration_ms":  stats.AvgDuration.Milliseconds(),
		"by_type":                 byType,
	})
}
