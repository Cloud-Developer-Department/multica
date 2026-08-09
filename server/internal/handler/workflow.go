package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// workflowErrorCode maps a service-layer WorkflowError to the API error
// contract (api_design.md §2): {error, code, detail}.
func writeWorkflowError(w http.ResponseWriter, err error) {
	var we *service.WorkflowError
	if errors.As(err, &we) {
		writeJSON(w, workflowErrorStatus(we.Code), map[string]any{
			"error":  we.Msg,
			"code":   we.Code,
			"detail": we.Detail,
		})
		return
	}
	writeError(w, http.StatusInternalServerError, "internal error")
}

func workflowErrorStatus(code string) int {
	switch code {
	case service.CodeInvalidRequest, service.CodeInvalidTransition, service.CodeRejectReasonRequired:
		return http.StatusBadRequest
	case service.CodeForbidden:
		return http.StatusForbidden
	case service.CodeNotFound:
		return http.StatusNotFound
	case service.CodeDuplicateVersion, service.CodeAlreadyReviewed, service.CodeStageConflict:
		return http.StatusConflict
	case service.CodeNotReviewable, service.CodeInvalidState:
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}

// workflowNodeResponse is the node object emitted on workflow APIs.
func workflowNodeResponse(n db.WorkflowNode, issue db.Issue) map[string]any {
	artifactReviewStatus := "none"
	if issue.Status == "in_review" {
		artifactReviewStatus = "pending"
	}
	return map[string]any{
		"id":                     uuidToString(n.ID),
		"workflow_id":            uuidToString(n.WorkflowID),
		"stage":                  n.Stage,
		"seq":                    n.Seq,
		"type":                   n.Type,
		"name":                   n.Name,
		"status":                 n.Status,
		"issue_id":               uuidToString(n.IssueID),
		"assignee_type":          textToPtr(n.AssigneeType),
		"assignee_id":            uuidToPtr(n.AssigneeID),
		"review_required":        n.ReviewRequired,
		"artifact_review_status": artifactReviewStatus,
		"started_at":             timestampToPtr(n.StartedAt),
		"completed_at":           timestampToPtr(n.CompletedAt),
		"created_at":             timestampToString(n.CreatedAt),
		"updated_at":             timestampToString(n.UpdatedAt),
	}
}

// workflowResponse builds the workflow detail object including nodes grouped
// by stage and a progress summary.
func workflowResponse(wf db.Workflow, nodes []db.WorkflowNode, issues map[pgtype.UUID]db.Issue) map[string]any {
	total, done, blocked, inReview := 0, 0, 0, 0
	type stageGroup struct {
		Stage  int            `json:"stage"`
		Name   string         `json:"name"`
		Status string         `json:"status"`
		Nodes  []map[string]any `json:"nodes"`
	}
	stageNodes := map[int][]map[string]any{}
	for _, n := range nodes {
		total++
		switch n.Status {
		case "done", "cancelled":
			done++
		case "blocked":
			blocked++
		case "in_review":
			inReview++
		}
		var issue db.Issue
		if issues != nil {
			issue = issues[n.IssueID]
		}
		stageNodes[int(n.Stage)] = append(stageNodes[int(n.Stage)], workflowNodeResponse(n, issue))
	}
	// Stage names from the stored definition.
	var def service.TemplateDefinition
	if len(wf.Definition) > 0 {
		_ = json.Unmarshal(wf.Definition, &def)
	}
	stageNames := map[int]string{}
	for _, st := range def.Stages {
		stageNames[st.Stage] = st.Name
	}
	stages := make([]map[string]any, 0, len(stageNodes))
	for s, ns := range stageNodes {
		statuses := make([]string, len(ns))
		for i, n := range ns {
			statuses[i] = n["status"].(string)
		}
		stages = append(stages, map[string]any{
			"stage":  s,
			"name":   stageNames[s],
			"status": statusOfStage(statuses),
			"nodes":  ns,
		})
	}
	// Deterministic stage ordering.
	for i := 0; i < len(stages); i++ {
		for j := i + 1; j < len(stages); j++ {
			if stages[j]["stage"].(int) < stages[i]["stage"].(int) {
				stages[i], stages[j] = stages[j], stages[i]
			}
		}
	}
	return map[string]any{
		"id":             uuidToString(wf.ID),
		"workspace_id":   uuidToString(wf.WorkspaceID),
		"source_issue_id": uuidToString(wf.SourceIssueID),
		"name":           wf.Name,
		"description":    textToPtr(wf.Description),
		"status":         wf.Status,
		"current_stage":  wf.CurrentStage,
		"created_by_type": wf.CreatedByType,
		"created_by_id":  uuidToString(wf.CreatedByID),
		"created_at":     timestampToString(wf.CreatedAt),
		"updated_at":     timestampToString(wf.UpdatedAt),
		"stages":         stages,
		"progress": map[string]any{
			"total_nodes":    total,
			"done_nodes":     done,
			"blocked_nodes":  blocked,
			"in_review_nodes": inReview,
		},
	}
}

func statusOfStage(statuses []string) string {
	terminal := 0
	anyBlocked, anyReview, anyActive := false, false, false
	for _, s := range statuses {
		if s == "done" || s == "cancelled" {
			terminal++
		}
		switch s {
		case "blocked":
			anyBlocked = true
		case "in_review":
			anyReview = true
		case "in_progress", "todo":
			anyActive = true
		}
	}
	if len(statuses) > 0 && terminal == len(statuses) {
		return "done"
	}
	if anyBlocked {
		return "blocked"
	}
	if anyReview {
		return "in_review"
	}
	if anyActive {
		return "in_progress"
	}
	return "todo"
}

// parsePagination reads limit/offset query params with defaults.
func parsePagination(r *http.Request, defaultLimit int32) (int32, int32) {
	limit := defaultLimit
	offset := int32(0)
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil && n > 0 && n <= 200 {
			limit = int32(n)
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil && n >= 0 {
			offset = int32(n)
		}
	}
	return limit, offset
}

// CreateWorkflow handles POST /api/workflows.
func (h *Handler) CreateWorkflow(w http.ResponseWriter, r *http.Request) {
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
		SourceIssueID *string `json:"source_issue_id"`
		Name          *string `json:"name"`
		Description   string  `json:"description"`
		TemplateKey   string  `json:"template_key"`
		CustomNodes   []struct {
			Type           string `json:"type"`
			AssigneeType   string `json:"assignee_type"`
			AssigneeID     string `json:"assignee_id"`
			ReviewRequired bool   `json:"review_required"`
		} `json:"customizations"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.SourceIssueID == nil || req.Name == nil || *req.Name == "" {
		writeError(w, http.StatusBadRequest, "source_issue_id and name are required")
		return
	}
	sourceUUID, ok := parseUUIDOrBadRequest(w, *req.SourceIssueID, "source_issue_id")
	if !ok {
		return
	}
	// V-01 (security audit): `customizations[].review_required` lets the caller
	// turn off the human review gate on every node, so a machine credential
	// (mat_ task token / mcn_ cloud PAT) could create a review-free workflow
	// and bypass the CLO-175 human review chain. Machine credentials may not
	// carry customizations at all; human callers must be a workspace
	// owner/admin to override node config.
	if len(req.CustomNodes) > 0 {
		if isMachineActorSource(r) {
			writeJSON(w, http.StatusForbidden, map[string]any{
				"error": "machine credentials cannot customize workflow nodes",
				"code":  service.CodeForbidden,
			})
			return
		}
		member, err := h.getWorkspaceMember(r.Context(), userID, uuidToString(wsUUID))
		if err != nil || !roleAllowed(member.Role, "owner", "admin") {
			writeJSON(w, http.StatusForbidden, map[string]any{
				"error": "only workspace owner/admin can customize workflow nodes",
				"code":  service.CodeForbidden,
			})
			return
		}
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	params := service.CreateWorkflowParams{
		WorkspaceID:   wsUUID,
		SourceIssueID: sourceUUID,
		Name:          *req.Name,
		Description:   req.Description,
		TemplateKey:   req.TemplateKey,
		CreatorType:   actorType,
		CreatorID:     parseUUID(actorID),
	}
	for _, c := range req.CustomNodes {
		params.CustomNodes = append(params.CustomNodes, service.TemplateNode{
			Type:           c.Type,
			AssigneeType:   c.AssigneeType,
			AssigneeID:     c.AssigneeID,
			ReviewRequired: c.ReviewRequired,
		})
	}

	result, err := h.WorkflowService.Create(r.Context(), params)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	// Load child issues for node responses.
	issues := map[pgtype.UUID]db.Issue{}
	for _, n := range result.Nodes {
		if issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{ID: n.IssueID, WorkspaceID: wsUUID}); err == nil {
			issues[n.IssueID] = issue
		}
	}
	writeJSON(w, http.StatusCreated, workflowResponse(result.Workflow, result.Nodes, issues))
}

// ListWorkflows handles GET /api/workflows.
func (h *Handler) ListWorkflows(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	var status *string
	if v := r.URL.Query().Get("status"); v != "" {
		status = &v
	}
	limit, offset := parsePagination(r, 50)
	items, total, err := h.WorkflowService.List(r.Context(), wsUUID, status, limit, offset)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	list := make([]map[string]any, 0, len(items))
	for _, wf := range items {
		nodes, err := h.WorkflowService.ListNodes(r.Context(), wsUUID, wf.ID, nil, nil)
		if err != nil {
			nodes = nil
		}
		// Compact list object: keep progress summary without full stage tree.
		resp := workflowResponse(wf, nodes, nil)
		list = append(list, map[string]any{
			"id":             resp["id"],
			"workspace_id":   resp["workspace_id"],
			"source_issue_id": resp["source_issue_id"],
			"name":           resp["name"],
			"status":         resp["status"],
			"current_stage":  resp["current_stage"],
			"progress":       resp["progress"],
			"created_at":     resp["created_at"],
			"updated_at":     resp["updated_at"],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list, "total": total})
}

// GetWorkflow handles GET /api/workflows/{id}.
func (h *Handler) GetWorkflow(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	wfID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow id")
	if !ok {
		return
	}
	wf, err := h.WorkflowService.Get(r.Context(), wsUUID, wfID)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	nodes, err := h.WorkflowService.ListNodes(r.Context(), wsUUID, wfID, nil, nil)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	issues := map[pgtype.UUID]db.Issue{}
	for _, n := range nodes {
		if issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{ID: n.IssueID, WorkspaceID: wsUUID}); err == nil {
			issues[n.IssueID] = issue
		}
	}
	writeJSON(w, http.StatusOK, workflowResponse(wf, nodes, issues))
}

// UpdateWorkflow handles PUT /api/workflows/{id}.
func (h *Handler) UpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	wfID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow id")
	if !ok {
		return
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	wf, err := h.WorkflowService.Update(r.Context(), wsUUID, wfID, req.Name, req.Description)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	nodes, _ := h.WorkflowService.ListNodes(r.Context(), wsUUID, wfID, nil, nil)
	writeJSON(w, http.StatusOK, workflowResponse(wf, nodes, nil))
}

// canManageWorkflow implements the FR5.5 Leader/admin control gate for
// workflow-mutating operations (advance stage, node status override): the
// requesting member must be a workspace owner/admin, the workflow's creator,
// or the source_issue creator. This prevents arbitrary workspace members (or
// a machine credential posing as a member) from forcing stage advancement or
// bypassing the human review loop (M-1 / H-2 security audit).
func (h *Handler) canManageWorkflow(r *http.Request, workspaceID pgtype.UUID, workflowID pgtype.UUID, userID string) bool {
	member, err := h.getWorkspaceMember(r.Context(), userID, uuidToString(workspaceID))
	if err != nil {
		return false
	}
	if member.Role == "owner" || member.Role == "admin" {
		return true
	}
	wf, err := h.Queries.GetWorkflow(r.Context(), db.GetWorkflowParams{ID: workflowID, WorkspaceID: workspaceID})
	if err != nil {
		return false
	}
	if wf.CreatedByType == "member" && uuidToString(wf.CreatedByID) == userID {
		return true
	}
	source, err := h.Queries.GetIssue(r.Context(), wf.SourceIssueID)
	if err != nil {
		return false
	}
	return source.CreatorType == "member" && uuidToString(source.CreatorID) == userID
}

// AdvanceWorkflow handles POST /api/workflows/{id}/advance.
func (h *Handler) AdvanceWorkflow(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	wfID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	// M-1 (security audit): Leader/admin control, human-only. The route
	// carries RequireHumanActor; this handler-level check is defense-in-depth.
	if !requireHumanActor(w, r) {
		return
	}
	// M-1 (security audit): stage advancement is a Leader/admin control
	// (FR5.5). Restrict to the workflow creator / source_issue creator /
	// owner-admin; RequireHumanActor already excluded machine credentials.
	if !h.canManageWorkflow(r, wsUUID, wfID, userID) {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"error": "only the workflow creator, source_issue creator, or a workspace owner/admin can advance the workflow",
			"code":  "forbidden",
		})
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)

	wf, _, err := h.WorkflowService.AdvanceStage(r.Context(), wsUUID, wfID, actorType, parseUUID(actorID), req.Reason)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":             uuidToString(wf.ID),
		"current_stage":  wf.CurrentStage,
		"status":         wf.Status,
		"advanced":       true,
	})
}

// ListWorkflowNodes handles GET /api/workflows/{id}/nodes.
func (h *Handler) ListWorkflowNodes(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	wfID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow id")
	if !ok {
		return
	}
	var stage *int32
	if v := r.URL.Query().Get("stage"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil {
			s := int32(n)
			stage = &s
		}
	}
	var status *string
	if v := r.URL.Query().Get("status"); v != "" {
		status = &v
	}
	nodes, err := h.WorkflowService.ListNodes(r.Context(), wsUUID, wfID, stage, status)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	issues := map[pgtype.UUID]db.Issue{}
	for _, n := range nodes {
		if issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{ID: n.IssueID, WorkspaceID: wsUUID}); err == nil {
			issues[n.IssueID] = issue
		}
	}
	out := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, workflowNodeResponse(n, issues[n.IssueID]))
	}
	writeJSON(w, http.StatusOK, out)
}

// ListWorkflowTransitions handles GET /api/workflows/{id}/transitions.
func (h *Handler) ListWorkflowTransitions(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	wfID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow id")
	if !ok {
		return
	}
	var nodeID *pgtype.UUID
	if v := r.URL.Query().Get("node_id"); v != "" {
		if u, err := util.ParseUUID(v); err == nil {
			nodeID = &u
		}
	}
	limit, offset := parsePagination(r, 50)
	items, total, err := h.WorkflowService.ListTransitions(r.Context(), wsUUID, wfID, nodeID, limit, offset)
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, t := range items {
		out = append(out, map[string]any{
			"id":          uuidToString(t.ID),
			"workflow_id": uuidToString(t.WorkflowID),
			"node_id":     uuidToPtr(t.NodeID),
			"from_status": textToPtr(t.FromStatus),
			"to_status":   t.ToStatus,
			"actor_type":  t.ActorType,
			"actor_id":    uuidToPtr(t.ActorID),
			"reason":      textToPtr(t.Reason),
			"created_at":  timestampToString(t.CreatedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "total": total})
}

// OverrideNodeStatus handles POST /api/workflows/{id}/nodes/{nodeId}/status.
func (h *Handler) OverrideNodeStatus(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	wfID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workflow id")
	if !ok {
		return
	}
	nodeID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "nodeId"), "node id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	// H-2 (security audit): node status override is a Leader/admin control,
	// human-only. The route carries RequireHumanActor; this handler-level
	// check is defense-in-depth.
	if !requireHumanActor(w, r) {
		return
	}
	// H-2 (security audit): node status override can force a review-required
	// node to done and bypass the human review loop, so it is a Leader/admin
	// control: owner/admin, workflow creator, or source_issue creator.
	// RequireHumanActor already excluded machine credentials.
	if !h.canManageWorkflow(r, wsUUID, wfID, userID) {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"error": "only the workflow creator, source_issue creator, or a workspace owner/admin can override node status",
			"code":  "forbidden",
		})
		return
	}
	var req struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	node, err := h.WorkflowService.OverrideNodeStatus(r.Context(), wsUUID, wfID, nodeID, req.Status, req.Reason, actorType, parseUUID(actorID))
	if err != nil {
		writeWorkflowError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":       uuidToString(wfID),
		"node_id":  uuidToString(node.ID),
		"status":   node.Status,
		"issue_id": uuidToString(node.IssueID),
	})
}
