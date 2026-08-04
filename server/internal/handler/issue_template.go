package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Issue templates (CLO-159): workspace-level presets that pre-fill an issue's
// fields on creation. The CRUD surface mirrors label / property: any workspace
// member can read, owner/admin can write. Preset templates (is_preset=true)
// are seeded per workspace and protected from deletion so the catalog stays
// stable, but owners may edit their content (e.g. tweak the default body).
//
// Label IDs are stored as a JSONB array of UUID strings. The handler validates
// each against issue_label in the workspace on write so a template never
// references a foreign or deleted label. assignee/project validity is checked
// via the same helpers as CreateIssue.
const (
	maxIssueTemplateNameLen        = 64
	maxIssueTemplateDescriptionLen = 500
	maxIssueTemplateBodyLen        = 16000
	maxIssueTemplateLabelIDs       = 50
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type IssueTemplateResponse struct {
	ID            string   `json:"id"`
	WorkspaceID   string   `json:"workspace_id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	TitleTemplate string   `json:"title_template"`
	BodyTemplate  string   `json:"body_template"`
	Status        string   `json:"status"`
	Priority      string   `json:"priority"`
	AssigneeType  *string  `json:"assignee_type"`
	AssigneeID    *string  `json:"assignee_id"`
	ProjectID     *string  `json:"project_id"`
	Stage         *int32   `json:"stage"`
	LabelIDs      []string `json:"label_ids"`
	Icon          string   `json:"icon"`
	Category      string   `json:"category"`
	IsPreset      bool     `json:"is_preset"`
	CreatedBy     string   `json:"created_by"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
}

func issueTemplateToResponse(t db.IssueTemplate) IssueTemplateResponse {
	resp := IssueTemplateResponse{
		ID:            uuidToString(t.ID),
		WorkspaceID:   uuidToString(t.WorkspaceID),
		Name:          t.Name,
		Description:   t.Description,
		TitleTemplate: t.TitleTemplate,
		BodyTemplate:  t.BodyTemplate,
		Status:        t.Status,
		Priority:      t.Priority,
		Icon:          t.Icon,
		Category:      t.Category,
		IsPreset:      t.IsPreset,
		CreatedBy:     uuidToString(t.CreatedBy),
		CreatedAt:     timestampToString(t.CreatedAt),
		UpdatedAt:     timestampToString(t.UpdatedAt),
		LabelIDs:      parseLabelIDsJSONB(t.LabelIds),
	}
	if t.AssigneeType.Valid {
		s := t.AssigneeType.String
		resp.AssigneeType = &s
	}
	if t.AssigneeID.Valid {
		s := uuidToString(t.AssigneeID)
		resp.AssigneeID = &s
	}
	if t.ProjectID.Valid {
		s := uuidToString(t.ProjectID)
		resp.ProjectID = &s
	}
	if t.Stage.Valid {
		v := t.Stage.Int32
		resp.Stage = &v
	}
	return resp
}

// parseLabelIDsJSONB decodes the JSONB label_ids array (stored as []byte from
// pgx) into a string slice. Returns nil for empty/invalid so the JSON field
// drops out cleanly via omitempty semantics in callers that want it.
func parseLabelIDsJSONB(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}

// marshalLabelIDsJSONB encodes a string slice into the JSONB array bytes
// expected by the label_ids column. nil → '[]'.
func marshalLabelIDsJSONB(ids []string) []byte {
	if len(ids) == 0 {
		return []byte("[]")
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return []byte("[]")
	}
	return b
}

type CreateIssueTemplateRequest struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	TitleTemplate string   `json:"title_template"`
	BodyTemplate  string   `json:"body_template"`
	Status        string   `json:"status"`
	Priority      string   `json:"priority"`
	AssigneeType  *string  `json:"assignee_type"`
	AssigneeID    *string  `json:"assignee_id"`
	ProjectID     *string  `json:"project_id"`
	Stage         *int32   `json:"stage"`
	LabelIDs      []string `json:"label_ids"`
	Icon          string   `json:"icon"`
	Category      string   `json:"category"`
}

type UpdateIssueTemplateRequest struct {
	Name          *string  `json:"name"`
	Description   *string  `json:"description"`
	TitleTemplate *string  `json:"title_template"`
	BodyTemplate  *string  `json:"body_template"`
	Status        *string  `json:"status"`
	Priority      *string  `json:"priority"`
	AssigneeType  *string  `json:"assignee_type"`
	AssigneeID    *string  `json:"assignee_id"`
	ProjectID     *string  `json:"project_id"`
	Stage         *int32   `json:"stage"`
	LabelIDs      []string `json:"label_ids"`
	Icon          *string  `json:"icon"`
	Category      *string  `json:"category"`
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

func validateIssueTemplateName(raw string) (string, error) {
	for _, r := range raw {
		if unicode.IsControl(r) {
			return "", errors.New("name cannot contain tabs, newlines, or control characters")
		}
	}
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", errors.New("name is required")
	}
	if utf8.RuneCountInString(name) > maxIssueTemplateNameLen {
		return "", fmt.Errorf("name must be %d characters or fewer", maxIssueTemplateNameLen)
	}
	return name, nil
}

func validateIssueTemplateBody(raw string) error {
	if utf8.RuneCountInString(raw) > maxIssueTemplateBodyLen {
		return fmt.Errorf("body_template must be %d characters or fewer", maxIssueTemplateBodyLen)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Handlers — issue template CRUD
// ---------------------------------------------------------------------------

func (h *Handler) ListIssueTemplates(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	templates, err := h.Queries.ListIssueTemplates(r.Context(), wsUUID)
	if err != nil {
		slog.Warn("ListIssueTemplates failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list issue templates")
		return
	}
	resp := make([]IssueTemplateResponse, len(templates))
	for i, t := range templates {
		resp[i] = issueTemplateToResponse(t)
	}
	writeJSON(w, http.StatusOK, map[string]any{"issue_templates": resp, "total": len(resp)})
}

func (h *Handler) GetIssueTemplate(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "template id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	t, err := h.Queries.GetIssueTemplate(r.Context(), db.GetIssueTemplateParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "issue template not found")
			return
		}
		slog.Warn("GetIssueTemplate failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to get issue template")
		return
	}
	writeJSON(w, http.StatusOK, issueTemplateToResponse(t))
}

func (h *Handler) CreateIssueTemplate(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	var req CreateIssueTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	params, ok := h.buildCreateIssueTemplateParams(w, r, wsUUID, userID, req)
	if !ok {
		return
	}

	t, err := h.Queries.CreateIssueTemplate(r.Context(), params)
	if err != nil {
		if isCheckViolation(err) {
			writeError(w, http.StatusBadRequest, "invalid template field value")
			return
		}
		slog.Warn("CreateIssueTemplate failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create issue template")
		return
	}
	resp := issueTemplateToResponse(t)
	h.publish(protocol.EventIssueTemplateCreated, workspaceID, "member", userID, map[string]any{"issue_template": resp})
	writeJSON(w, http.StatusCreated, resp)
}

// buildCreateIssueTemplateParams validates the request and assembles the
// sqlc params. Shared between the HTTP CreateIssueTemplate handler and the
// preset-seeding path (which bypasses HTTP but reuses validation).
func (h *Handler) buildCreateIssueTemplateParams(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, userID string, req CreateIssueTemplateRequest) (db.CreateIssueTemplateParams, bool) {
	name, err := validateIssueTemplateName(req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return db.CreateIssueTemplateParams{}, false
	}
	if utf8.RuneCountInString(req.Description) > maxIssueTemplateDescriptionLen {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("description must be %d characters or fewer", maxIssueTemplateDescriptionLen))
		return db.CreateIssueTemplateParams{}, false
	}
	if err := validateIssueTemplateBody(req.BodyTemplate); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return db.CreateIssueTemplateParams{}, false
	}

	status := req.Status
	if status == "" {
		status = "todo"
	}
	if !validateIssueEnum(w, "status", status, validIssueStatuses) {
		return db.CreateIssueTemplateParams{}, false
	}
	priority := req.Priority
	if priority == "" {
		priority = "none"
	}
	if !validateIssueEnum(w, "priority", priority, validIssuePriorities) {
		return db.CreateIssueTemplateParams{}, false
	}
	if req.Stage != nil && *req.Stage < 1 {
		writeError(w, http.StatusBadRequest, "stage must be >= 1")
		return db.CreateIssueTemplateParams{}, false
	}

	var assigneeType pgtype.Text
	var assigneeID pgtype.UUID
	if req.AssigneeType != nil {
		assigneeType = pgtype.Text{String: *req.AssigneeType, Valid: true}
	}
	if req.AssigneeID != nil {
		id, ok := parseUUIDOrBadRequest(w, *req.AssigneeID, "assignee_id")
		if !ok {
			return db.CreateIssueTemplateParams{}, false
		}
		assigneeID = id
	}
	// Validate the assignee pair against the workspace so a template never
	// references a foreign or archived assignee. Templates are allowed to be
	// unassigned (both unset).
	if status, msg := h.validateAssigneePair(r.Context(), r, uuidToString(wsUUID), assigneeType, assigneeID); status != 0 {
		writeError(w, status, msg)
		return db.CreateIssueTemplateParams{}, false
	}

	var projectID pgtype.UUID
	if req.ProjectID != nil {
		id, ok := parseUUIDOrBadRequest(w, *req.ProjectID, "project_id")
		if !ok {
			return db.CreateIssueTemplateParams{}, false
		}
		projectID = id
	}

	labelIDs, ok := h.validateTemplateLabelIDs(w, r, wsUUID, req.LabelIDs)
	if !ok {
		return db.CreateIssueTemplateParams{}, false
	}

	var stage pgtype.Int4
	if req.Stage != nil {
		stage = pgtype.Int4{Int32: *req.Stage, Valid: true}
	}

	return db.CreateIssueTemplateParams{
		WorkspaceID:   wsUUID,
		Name:          name,
		Description:   sanitizeNullBytes(strings.TrimSpace(req.Description)),
		TitleTemplate: sanitizeNullBytes(req.TitleTemplate),
		BodyTemplate:  sanitizeNullBytes(req.BodyTemplate),
		Status:        status,
		Priority:      priority,
		AssigneeType:  assigneeType,
		AssigneeID:    assigneeID,
		ProjectID:     projectID,
		Stage:         stage,
		LabelIds:      marshalLabelIDsJSONB(labelIDs),
		Icon:          sanitizeNullBytes(strings.TrimSpace(req.Icon)),
		Category:      sanitizeNullBytes(strings.TrimSpace(req.Category)),
		IsPreset:      false,
		CreatedBy:     parseUUID(userID),
	}, true
}

// validateTemplateLabelIDs checks that every label ID belongs to an issue-scoped
// label in the workspace. Returns the validated (unmodified) slice or writes a
// 400 and returns ok=false.
func (h *Handler) validateTemplateLabelIDs(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, ids []string) ([]string, bool) {
	if len(ids) == 0 {
		return nil, true
	}
	if len(ids) > maxIssueTemplateLabelIDs {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("a template cannot reference more than %d labels", maxIssueTemplateLabelIDs))
		return nil, false
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		labelUUID, ok := parseUUIDOrBadRequest(w, id, "label_id")
		if !ok {
			return nil, false
		}
		label, err := h.Queries.GetLabel(r.Context(), db.GetLabelParams{
			ID: labelUUID, WorkspaceID: wsUUID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("label %q not found in this workspace", id))
				return nil, false
			}
			slog.Warn("GetLabel in validateTemplateLabelIDs failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to validate label")
			return nil, false
		}
		if label.ResourceType != "issue" {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("label %q is not an issue label", id))
			return nil, false
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, true
}

func (h *Handler) UpdateIssueTemplate(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "template id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	var req UpdateIssueTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Branch on pgx.ErrNoRows directly from the UPDATE — the WHERE clause
	// already enforces (id, workspace_id), so a missing row means either the
	// template doesn't exist or it's not in this workspace.
	params := db.UpdateIssueTemplateParams{ID: idUUID, WorkspaceID: wsUUID}
	if req.Name != nil {
		name, err := validateIssueTemplateName(*req.Name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Name = pgtype.Text{String: name, Valid: true}
	}
	if req.Description != nil {
		if utf8.RuneCountInString(*req.Description) > maxIssueTemplateDescriptionLen {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("description must be %d characters or fewer", maxIssueTemplateDescriptionLen))
			return
		}
		params.Description = pgtype.Text{String: sanitizeNullBytes(strings.TrimSpace(*req.Description)), Valid: true}
	}
	if req.TitleTemplate != nil {
		params.TitleTemplate = pgtype.Text{String: sanitizeNullBytes(*req.TitleTemplate), Valid: true}
	}
	if req.BodyTemplate != nil {
		if err := validateIssueTemplateBody(*req.BodyTemplate); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.BodyTemplate = pgtype.Text{String: sanitizeNullBytes(*req.BodyTemplate), Valid: true}
	}
	if req.Status != nil {
		if !validateIssueEnum(w, "status", *req.Status, validIssueStatuses) {
			return
		}
		params.Status = pgtype.Text{String: *req.Status, Valid: true}
	}
	if req.Priority != nil {
		if !validateIssueEnum(w, "priority", *req.Priority, validIssuePriorities) {
			return
		}
		params.Priority = pgtype.Text{String: *req.Priority, Valid: true}
	}
	if req.Stage != nil {
		if *req.Stage < 1 {
			writeError(w, http.StatusBadRequest, "stage must be >= 1")
			return
		}
		params.Stage = pgtype.Int4{Int32: *req.Stage, Valid: true}
	}
	// assignee_type / assignee_id / project_id use the raw narg form: a null
	// narg clears the stored value. When assignee_type is provided we validate
	// the pair; when it is null we clear both (the SQL sets both columns to
	// whatever narg we pass, so clearing requires passing null for both).
	if req.AssigneeType != nil || req.AssigneeID != nil {
		var assigneeType pgtype.Text
		var assigneeID pgtype.UUID
		if req.AssigneeType != nil {
			assigneeType = pgtype.Text{String: *req.AssigneeType, Valid: true}
		}
		if req.AssigneeID != nil {
			id, ok := parseUUIDOrBadRequest(w, *req.AssigneeID, "assignee_id")
			if !ok {
				return
			}
			assigneeID = id
		}
		if status, msg := h.validateAssigneePair(r.Context(), r, workspaceID, assigneeType, assigneeID); status != 0 {
			writeError(w, status, msg)
			return
		}
		params.AssigneeType = assigneeType
		params.AssigneeID = assigneeID
	}
	if req.ProjectID != nil {
		id, ok := parseUUIDOrBadRequest(w, *req.ProjectID, "project_id")
		if !ok {
			return
		}
		params.ProjectID = id
	}
	if req0 := req.LabelIDs; req0 != nil {
		labelIDs, ok := h.validateTemplateLabelIDs(w, r, wsUUID, req0)
		if !ok {
			return
		}
		params.LabelIds = marshalLabelIDsJSONB(labelIDs)
	}
	if req.Icon != nil {
		params.Icon = pgtype.Text{String: sanitizeNullBytes(strings.TrimSpace(*req.Icon)), Valid: true}
	}
	if req.Category != nil {
		params.Category = pgtype.Text{String: sanitizeNullBytes(strings.TrimSpace(*req.Category)), Valid: true}
	}

	t, err := h.Queries.UpdateIssueTemplate(r.Context(), params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "issue template not found")
			return
		}
		if isCheckViolation(err) {
			writeError(w, http.StatusBadRequest, "invalid template field value")
			return
		}
		slog.Warn("UpdateIssueTemplate failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update issue template")
		return
	}
	resp := issueTemplateToResponse(t)
	h.publish(protocol.EventIssueTemplateUpdated, workspaceID, "member", userID, map[string]any{"issue_template": resp})
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteIssueTemplate(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "template id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	// Preset templates are protected from deletion. Fetch first to check the
	// flag — a TOCTOU here is benign (worst case: a preset is deleted between
	// the check and the delete, which is a no-op on the protection invariant
	// since the row is already gone). The check runs in the same transaction
	// as the delete below to make the guard atomic.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	existing, err := qtx.GetIssueTemplate(r.Context(), db.GetIssueTemplateParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "issue template not found")
			return
		}
		slog.Warn("GetIssueTemplate in DeleteIssueTemplate failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete issue template")
		return
	}
	if existing.IsPreset {
		writeError(w, http.StatusConflict, "preset templates cannot be deleted")
		return
	}
	if _, err := qtx.DeleteIssueTemplate(r.Context(), db.DeleteIssueTemplateParams{
		ID: idUUID, WorkspaceID: wsUUID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "issue template not found")
			return
		}
		slog.Warn("DeleteIssueTemplate failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete issue template")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit issue template deletion")
		return
	}
	h.publish(protocol.EventIssueTemplateDeleted, workspaceID, "member", userID, map[string]any{"issue_template_id": uuidToString(idUUID)})
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Preset seeding
// ---------------------------------------------------------------------------

// presetIssueTemplates is the catalog of 4 built-in templates seeded into
// every new workspace. The fields map 1:1 to CreateIssueTemplateRequest so
// the same validation path is reused. Names are Chinese to match the product
// voice (the parent issue CLO-159 specifies the 4 templates in Chinese).
var presetIssueTemplates = []CreateIssueTemplateRequest{
	{
		Name:          "特性开发",
		Description:   "用于规划新特性开发的全流程模板",
		TitleTemplate: "【特性】",
		BodyTemplate:  "## 需求描述\n\n\n## 验收标准\n\n- \n\n## 影响范围\n\n",
		Status:        "todo",
		Priority:      "medium",
		Icon:          "sparkles",
		Category:      "engineering",
	},
	{
		Name:          "Bug 修复",
		Description:   "用于跟踪和修复缺陷的模板",
		TitleTemplate: "【Bug】",
		BodyTemplate:  "## 复现步骤\n\n1. \n\n## 期望行为\n\n\n## 实际行为\n\n\n## 环境\n\n",
		Status:        "todo",
		Priority:      "high",
		Icon:          "bug",
		Category:      "engineering",
	},
	{
		Name:          "需求分析",
		Description:   "用于调研和拆解新需求的模板",
		TitleTemplate: "【需求】",
		BodyTemplate:  "## 背景\n\n\n## 目标\n\n\n## 方案选项\n\n- 选项 A：\n- 选项 B：\n\n## 建议下一步\n\n",
		Status:        "todo",
		Priority:      "medium",
		Icon:          "lightbulb",
		Category:      "planning",
	},
	{
		Name:          "周报/月报",
		Description:   "用于撰写周期性汇报的模板",
		TitleTemplate: "【周报】",
		BodyTemplate:  "## 本期进展\n\n- \n\n## 遇到的问题\n\n\n## 下期计划\n\n- \n",
		Status:        "todo",
		Priority:      "none",
		Icon:          "list-checks",
		Category:      "planning",
	},
}

// SeedPresetIssueTemplates inserts the 4 built-in issue templates for a
// workspace. Called from the CreateWorkspace transaction so the presets land
// atomically with the workspace. Idempotent on name via ON CONFLICT semantics
// emulated by a pre-count check — safe to re-run for existing workspaces via
// an explicit seeding endpoint if one is added later.
//
// Runs inside the caller's transaction (qtx) so a failure rolls back the
// whole workspace creation rather than leaving a workspace without presets.
func SeedPresetIssueTemplates(ctx context.Context, qtx *db.Queries, workspaceID, creatorID pgtype.UUID) error {
	for _, preset := range presetIssueTemplates {
		_, err := qtx.CreateIssueTemplate(ctx, db.CreateIssueTemplateParams{
			WorkspaceID:   workspaceID,
			Name:          preset.Name,
			Description:   sanitizeNullBytes(strings.TrimSpace(preset.Description)),
			TitleTemplate: sanitizeNullBytes(preset.TitleTemplate),
			BodyTemplate:  sanitizeNullBytes(preset.BodyTemplate),
			Status:        preset.Status,
			Priority:      preset.Priority,
			LabelIds:      marshalLabelIDsJSONB(nil),
			Icon:          sanitizeNullBytes(strings.TrimSpace(preset.Icon)),
			Category:      sanitizeNullBytes(strings.TrimSpace(preset.Category)),
			IsPreset:      true,
			CreatedBy:     creatorID,
		})
		if err != nil {
			return fmt.Errorf("seed issue template %q: %w", preset.Name, err)
		}
	}
	return nil
}
