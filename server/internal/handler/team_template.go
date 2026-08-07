package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/agenttmpl"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/teamtmpl"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// teamTemplates is the in-memory catalog of team templates, loaded once at
// package init. Fail-fast here rather than at the first request: a malformed
// template ships in source, so it is a deploy-time defect, not a runtime one.
var teamTemplates *teamtmpl.Registry

func init() {
	reg, err := teamtmpl.Load()
	if err != nil {
		panic("teamtmpl: failed to load templates at startup: " + err.Error())
	}
	teamTemplates = reg
}

// --- Response shapes ---

// TeamTemplateSkillSummaryResponse is the per-skill payload in the list and
// detail endpoints. It omits the embedded Content/Files to keep the list
// payload small; the detail endpoint reuses the same shape for consistency.
type TeamTemplateSkillSummaryResponse struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	SourceURL   string `json:"source_url,omitempty"`
}

// TeamTemplateAgentSummaryResponse is the per-agent payload in the list. It
// omits Instructions; the detail endpoint returns the full AgentDef.
type TeamTemplateAgentSummaryResponse struct {
	Name   string   `json:"name"`
	Model  string   `json:"model,omitempty"`
	Skills []string `json:"skills,omitempty"`
}

// TeamTemplateSquadSummaryResponse is the per-squad payload in the list. It
// carries member_count (leader auto-joins) and omits the full member list.
type TeamTemplateSquadSummaryResponse struct {
	Name        string `json:"name"`
	Leader      string `json:"leader"`
	MemberCount int    `json:"member_count"`
}

// TeamTemplateSummaryResponse is what `GET /api/team-templates` returns per
// entry.
type TeamTemplateSummaryResponse struct {
	Slug        string                             `json:"slug"`
	Name        string                             `json:"name"`
	Description string                             `json:"description"`
	Category    string                             `json:"category,omitempty"`
	Icon        string                             `json:"icon,omitempty"`
	Accent      string                             `json:"accent,omitempty"`
	Skills      []TeamTemplateSkillSummaryResponse `json:"skills"`
	Agents      []TeamTemplateAgentSummaryResponse `json:"agents"`
	Squads      []TeamTemplateSquadSummaryResponse `json:"squads"`
}

// TeamTemplateDetailResponse is what `GET /api/team-templates/{slug}` returns:
// the full template with all content, files, instructions and members. Arrays
// are always non-nil so the wire format never emits `null` for an empty list.
type TeamTemplateDetailResponse struct {
	Slug        string              `json:"slug"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Category    string              `json:"category,omitempty"`
	Icon        string              `json:"icon,omitempty"`
	Accent      string              `json:"accent,omitempty"`
	Skills      []teamtmpl.SkillDef `json:"skills"`
	Agents      []teamtmpl.AgentDef `json:"agents"`
	Squads      []teamtmpl.SquadDef `json:"squads"`
}

// teamTemplateToSummary converts a loaded template into its list summary.
func teamTemplateToSummary(t teamtmpl.TeamTemplate) TeamTemplateSummaryResponse {
	skills := make([]TeamTemplateSkillSummaryResponse, 0, len(t.Skills))
	for _, s := range t.Skills {
		skills = append(skills, TeamTemplateSkillSummaryResponse{
			Name:        s.Name,
			Description: s.Description,
			SourceURL:   s.SourceURL,
		})
	}
	agents := make([]TeamTemplateAgentSummaryResponse, 0, len(t.Agents))
	for _, a := range t.Agents {
		agents = append(agents, TeamTemplateAgentSummaryResponse{
			Name:   a.Name,
			Model:  a.Model,
			Skills: a.Skills,
		})
	}
	squads := make([]TeamTemplateSquadSummaryResponse, 0, len(t.Squads))
	for _, sq := range t.Squads {
		squads = append(squads, TeamTemplateSquadSummaryResponse{
			Name:        sq.Name,
			Leader:      sq.Leader,
			MemberCount: 1 + len(sq.Members), // leader auto-joins
		})
	}
	return TeamTemplateSummaryResponse{
		Slug:        t.Slug,
		Name:        t.Name,
		Description: t.Description,
		Category:    t.Category,
		Icon:        t.Icon,
		Accent:      t.Accent,
		Skills:      skills,
		Agents:      agents,
		Squads:      squads,
	}
}

// teamTemplateToDetail converts a loaded template into its full detail,
// guaranteeing non-nil arrays.
func teamTemplateToDetail(t teamtmpl.TeamTemplate) TeamTemplateDetailResponse {
	skills := t.Skills
	if skills == nil {
		skills = []teamtmpl.SkillDef{}
	}
	agents := t.Agents
	if agents == nil {
		agents = []teamtmpl.AgentDef{}
	}
	squads := t.Squads
	if squads == nil {
		squads = []teamtmpl.SquadDef{}
	}
	return TeamTemplateDetailResponse{
		Slug:        t.Slug,
		Name:        t.Name,
		Description: t.Description,
		Category:    t.Category,
		Icon:        t.Icon,
		Accent:      t.Accent,
		Skills:      skills,
		Agents:      agents,
		Squads:      squads,
	}
}

// --- List + Get handlers ---

// ListTeamTemplates returns the summaries of every team template in the
// catalog. The payload is always a JSON array — an empty catalog yields `[]`.
func (h *Handler) ListTeamTemplates(w http.ResponseWriter, r *http.Request) {
	tmpls := teamTemplates.List()
	resp := make([]TeamTemplateSummaryResponse, 0, len(tmpls))
	for _, t := range tmpls {
		resp = append(resp, teamTemplateToSummary(t))
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetTeamTemplate returns the full template for the given slug.
func (h *Handler) GetTeamTemplate(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	tmpl, ok := teamTemplates.Get(slug)
	if !ok {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	writeJSON(w, http.StatusOK, teamTemplateToDetail(tmpl))
}

// --- Apply handler ---

// ApplyTeamTemplateRequest is the body of POST /api/team-templates/{slug}/apply.
type ApplyTeamTemplateRequest struct {
	// RuntimeID is the shared runtime every created agent binds to.
	RuntimeID string `json:"runtime_id"`
	// ModelOverrides maps agent name -> model. Keys must reference agents that
	// exist in the template. Absent/null = use the template's default model.
	ModelOverrides map[string]string `json:"model_overrides,omitempty"`
	// Visibility is the default visibility for agents that don't declare one
	// in the template. Defaults to "private". Same semantics as
	// CreateAgentFromTemplate: permission_mode, when present, is authoritative.
	Visibility     string  `json:"visibility,omitempty"`
	PermissionMode *string `json:"permission_mode,omitempty"`
}

// TeamTemplateResourceRef is one created/reused entry in the apply response.
type TeamTemplateResourceRef struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

// TeamTemplateResourceOutcome summarises what apply did for one resource kind.
// Created/Reused are always present ([] when empty, never null).
type TeamTemplateResourceOutcome struct {
	Created []TeamTemplateResourceRef `json:"created"`
	Reused  []TeamTemplateResourceRef `json:"reused"`
}

// ApplyTeamTemplateResponse is the 201 payload of a successful apply.
type ApplyTeamTemplateResponse struct {
	TemplateSlug string                      `json:"template_slug"`
	Skills       TeamTemplateResourceOutcome `json:"skills"`
	Agents       TeamTemplateResourceOutcome `json:"agents"`
	Squads       TeamTemplateResourceOutcome `json:"squads"`
}

// ApplyTeamTemplate materialises a whole team template into the current
// workspace atomically: skills -> agents -> squads, all inside one
// transaction. Remote skills are pre-fetched outside the transaction so a
// slow/broken source never pins a DB connection; any fetch failure aborts with
// 422 and zero side effects.
func (h *Handler) ApplyTeamTemplate(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)

	ownerID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req ApplyTeamTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RuntimeID == "" {
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}

	slug := chi.URLParam(r, "slug")
	tmpl, found := teamTemplates.Get(slug)
	if !found {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}

	// model_overrides may only reference agents declared in the template.
	if err := validateTeamTemplateModelOverrides(tmpl, req.ModelOverrides); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	runtimeUUID, ok := parseUUIDOrBadRequest(w, req.RuntimeID, "runtime_id")
	if !ok {
		return
	}

	// Runtime validation reproduces the gating done by CreateAgentFromTemplate
	// (handler/agent_template.go:210-225) — keep the two paths in sync. Done
	// before the fetch so we don't waste upstream calls on a 403-bound request.
	runtime, err := h.Queries.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          runtimeUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime_id")
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	if !canUseRuntimeForAgent(member, runtime) {
		writeError(w, http.StatusForbidden, "this runtime is private; only its owner or a workspace admin can create agents on it")
		return
	}
	creatorUUID := parseUUID(ownerID)

	// Pre-fetch stage (outside any tx): resolve every remote skill source up
	// front. Any failure -> 422 {error, failed_urls} with no side effects.
	importedBySkill, failedURLs := h.fetchTeamTemplateRemoteSkills(r, tmpl.Skills)
	if len(failedURLs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, fetchFailureResponse{
			Error:      "one or more skill sources are unavailable",
			FailedURLs: failedURLs,
		})
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin tx: "+err.Error())
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	resp := ApplyTeamTemplateResponse{
		TemplateSlug: tmpl.Slug,
		Skills:       TeamTemplateResourceOutcome{Created: []TeamTemplateResourceRef{}, Reused: []TeamTemplateResourceRef{}},
		Agents:       TeamTemplateResourceOutcome{Created: []TeamTemplateResourceRef{}, Reused: []TeamTemplateResourceRef{}},
		Squads:       TeamTemplateResourceOutcome{Created: []TeamTemplateResourceRef{}, Reused: []TeamTemplateResourceRef{}},
	}

	// ① Skills: find-or-create by template name.
	skillIDByName := make(map[string]pgtype.UUID, len(tmpl.Skills))
	for i, def := range tmpl.Skills {
		existing, err := qtx.GetSkillByWorkspaceAndName(r.Context(), db.GetSkillByWorkspaceAndNameParams{
			WorkspaceID: wsUUID,
			Name:        def.Name,
		})
		if err == nil {
			skillIDByName[def.Name] = existing.ID
			resp.Skills.Reused = append(resp.Skills.Reused, TeamTemplateResourceRef{Name: def.Name, ID: uuidToString(existing.ID)})
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("team-template apply: lookup skill failed",
				append(logger.RequestAttrs(r), "skill", def.Name, "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to look up skill: "+err.Error())
			return
		}

		content := def.Content
		description := def.Description
		origin := map[string]any{"type": "team_template", "template_slug": tmpl.Slug}
		var files []CreateSkillFileRequest
		if imp, ok := importedBySkill[i]; ok {
			content = imp.content
			if imp.description != "" {
				description = imp.description
			}
			origin["source_url"] = def.SourceURL
			if imp.origin != nil {
				for k, v := range imp.origin {
					if _, exists := origin[k]; !exists {
						origin[k] = v
					}
				}
			}
			for _, f := range imp.files {
				if !validateFilePath(f.path) {
					continue
				}
				files = append(files, CreateSkillFileRequest{Path: f.path, Content: f.content})
			}
		} else {
			for _, f := range def.Files {
				if !validateFilePath(f.Path) {
					continue
				}
				files = append(files, CreateSkillFileRequest{Path: f.Path, Content: f.Content})
			}
		}

		created, err := createSkillWithFilesInTx(r.Context(), qtx, skillCreateInput{
			WorkspaceID: wsUUID,
			CreatorID:   creatorUUID,
			Name:        def.Name,
			Description: description,
			Content:     content,
			Config:      origin,
			Files:       files,
		})
		if err != nil {
			slog.Error("team-template apply: failed to create skill",
				append(logger.RequestAttrs(r), "skill", def.Name, "error", err, "is_unique_violation", isUniqueViolation(err))...)
			writeError(w, http.StatusInternalServerError, "failed to create skill: "+err.Error())
			return
		}
		skillIDByName[def.Name] = parseUUID(created.ID)
		resp.Skills.Created = append(resp.Skills.Created, TeamTemplateResourceRef{Name: def.Name, ID: created.ID})
	}

	// ② Agents: find-or-create by name; reuse existing agents untouched.
	rc, _ := json.Marshal(map[string]any{})
	ce, _ := json.Marshal(map[string]string{})
	ca, _ := json.Marshal([]string{})
	agentIDByName := make(map[string]pgtype.UUID, len(tmpl.Agents))
	for _, def := range tmpl.Agents {
		existing, err := qtx.GetAgentByWorkspaceAndName(r.Context(), db.GetAgentByWorkspaceAndNameParams{
			WorkspaceID: wsUUID,
			Name:        def.Name,
		})
		if err == nil {
			agentIDByName[def.Name] = existing.ID
			resp.Agents.Reused = append(resp.Agents.Reused, TeamTemplateResourceRef{Name: def.Name, ID: uuidToString(existing.ID)})
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("team-template apply: lookup agent failed",
				append(logger.RequestAttrs(r), "agent", def.Name, "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to look up agent: "+err.Error())
			return
		}

		model := def.Model
		if override, ok := req.ModelOverrides[def.Name]; ok && override != "" {
			model = override
		}
		maxTasks := def.MaxConcurrentTasks
		if maxTasks == 0 {
			maxTasks = 6
		}

		// Permission resolution mirrors CreateAgentFromTemplate: request-level
		// permission_mode is authoritative when present; otherwise the agent's
		// own visibility (falling back to the request default, then "private")
		// is mapped.
		legacyVis := def.Visibility
		if legacyVis == "" {
			legacyVis = req.Visibility
		}
		if legacyVis == "" {
			legacyVis = "private"
		}
		perm, _, permErr := parsePermissionInput(wsUUID, req.PermissionMode, nil, req.PermissionMode != nil, false, &legacyVis)
		if permErr != nil {
			writeError(w, http.StatusBadRequest, permErr.Error())
			return
		}

		agent, err := qtx.CreateAgent(r.Context(), db.CreateAgentParams{
			WorkspaceID:              wsUUID,
			Name:                     def.Name,
			Description:              def.Description,
			Instructions:             def.Instructions,
			AvatarUrl:                newAgentAvatar(nil),
			RuntimeMode:              runtime.RuntimeMode,
			RuntimeConfig:            rc,
			RuntimeID:                runtime.ID,
			Visibility:               perm.legacyVisibility(),
			PermissionMode:           perm.mode,
			MaxConcurrentTasks:       maxTasks,
			OwnerID:                  creatorUUID,
			CustomEnv:                ce,
			CustomArgs:               ca,
			McpConfig:                nil,
			Model:                    pgtype.Text{String: model, Valid: model != ""},
			ThinkingLevel:            pgtype.Text{},
			ServiceTier:              pgtype.Text{},
			ComposioToolkitAllowlist: nil,
		})
		if err != nil {
			slog.Error("team-template apply: failed to create agent",
				append(logger.RequestAttrs(r), "agent", def.Name, "error", err, "is_unique_violation", isUniqueViolation(err))...)
			writeError(w, http.StatusInternalServerError, "failed to create agent: "+err.Error())
			return
		}
		agentIDByName[def.Name] = agent.ID

		// Attach referenced skills (ON CONFLICT DO NOTHING).
		for _, skillName := range def.Skills {
			skillID, ok := skillIDByName[skillName]
			if !ok {
				continue
			}
			if err := qtx.AddAgentSkill(r.Context(), db.AddAgentSkillParams{
				AgentID: agent.ID,
				SkillID: skillID,
			}); err != nil {
				slog.Error("team-template apply: failed to attach skill",
					append(logger.RequestAttrs(r), "agent", def.Name, "skill", skillName, "error", err)...)
				writeError(w, http.StatusInternalServerError, "failed to attach skill: "+err.Error())
				return
			}
		}
		// Persist the invocation allow-list inside the same tx as the agent row.
		if err := replaceInvocationTargetsWithQueries(r.Context(), qtx, agent.ID, creatorUUID, perm.targets); err != nil {
			slog.Error("team-template apply: persist invocation targets failed",
				append(logger.RequestAttrs(r), "agent", def.Name, "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to persist invocation targets: "+err.Error())
			return
		}

		resp.Agents.Created = append(resp.Agents.Created, TeamTemplateResourceRef{Name: def.Name, ID: uuidToString(agent.ID)})
	}

	// ③ Squads: find-or-create by name; leader auto-joins as role="leader".
	for _, def := range tmpl.Squads {
		existing, err := qtx.GetSquadByWorkspaceAndName(r.Context(), db.GetSquadByWorkspaceAndNameParams{
			WorkspaceID: wsUUID,
			Name:        def.Name,
		})
		if err == nil {
			resp.Squads.Reused = append(resp.Squads.Reused, TeamTemplateResourceRef{Name: def.Name, ID: uuidToString(existing.ID)})
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("team-template apply: lookup squad failed",
				append(logger.RequestAttrs(r), "squad", def.Name, "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to look up squad: "+err.Error())
			return
		}

		leaderID, ok := agentIDByName[def.Leader]
		if !ok {
			slog.Error("team-template apply: squad leader not materialised",
				append(logger.RequestAttrs(r), "squad", def.Name, "leader", def.Leader)...)
			writeError(w, http.StatusInternalServerError, "failed to resolve squad leader "+def.Leader)
			return
		}

		squad, err := qtx.CreateSquad(r.Context(), db.CreateSquadParams{
			WorkspaceID:            wsUUID,
			Name:                   def.Name,
			Description:            def.Description,
			LeaderID:               leaderID,
			CreatorID:              creatorUUID,
			AvatarUrl:              pgtype.Text{},
			UpgradeOnMemberMention: pgtype.Bool{Valid: true, Bool: true},
		})
		if err != nil {
			slog.Error("team-template apply: failed to create squad",
				append(logger.RequestAttrs(r), "squad", def.Name, "error", err, "is_unique_violation", isUniqueViolation(err))...)
			writeError(w, http.StatusInternalServerError, "failed to create squad: "+err.Error())
			return
		}

		// Persist squad instructions (CreateSquad itself has no instructions
		// column input; UpdateSquad carries it inside the same tx).
		if def.Instructions != "" {
			if _, err := qtx.UpdateSquad(r.Context(), db.UpdateSquadParams{
				ID:           squad.ID,
				Instructions: pgtype.Text{String: def.Instructions, Valid: true},
			}); err != nil {
				slog.Error("team-template apply: set squad instructions failed",
					append(logger.RequestAttrs(r), "squad", def.Name, "error", err)...)
				writeError(w, http.StatusInternalServerError, "failed to set squad instructions: "+err.Error())
				return
			}
		}

		// Leader auto-joins as role="leader" — do NOT re-add it from Members.
		if _, err := qtx.AddSquadMember(r.Context(), db.AddSquadMemberParams{
			SquadID:    squad.ID,
			MemberType: "agent",
			MemberID:   leaderID,
			Role:       "leader",
		}); err != nil {
			if !isUniqueViolation(err) {
				slog.Error("team-template apply: add squad leader failed",
					append(logger.RequestAttrs(r), "squad", def.Name, "error", err)...)
				writeError(w, http.StatusInternalServerError, "failed to add squad leader: "+err.Error())
				return
			}
		}
		for _, m := range def.Members {
			memberID, ok := agentIDByName[m.AgentName]
			if !ok {
				slog.Error("team-template apply: squad member not materialised",
					append(logger.RequestAttrs(r), "squad", def.Name, "member", m.AgentName)...)
				writeError(w, http.StatusInternalServerError, "failed to resolve squad member "+m.AgentName)
				return
			}
			if _, err := qtx.AddSquadMember(r.Context(), db.AddSquadMemberParams{
				SquadID:    squad.ID,
				MemberType: "agent",
				MemberID:   memberID,
				Role:       m.Role,
			}); err != nil {
				if !isUniqueViolation(err) {
					slog.Error("team-template apply: add squad member failed",
						append(logger.RequestAttrs(r), "squad", def.Name, "member", m.AgentName, "error", err)...)
					writeError(w, http.StatusInternalServerError, "failed to add squad member: "+err.Error())
					return
				}
			}
		}

		resp.Squads.Created = append(resp.Squads.Created, TeamTemplateResourceRef{Name: def.Name, ID: uuidToString(squad.ID)})
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("team-template apply: commit failed",
			append(logger.RequestAttrs(r), "template_slug", tmpl.Slug, "error", err)...)
		writeError(w, http.StatusInternalServerError, "commit failed: "+err.Error())
		return
	}

	slog.Info("team template applied",
		append(logger.RequestAttrs(r),
			"template_slug", tmpl.Slug,
			"skills_created", len(resp.Skills.Created),
			"skills_reused", len(resp.Skills.Reused),
			"agents_created", len(resp.Agents.Created),
			"agents_reused", len(resp.Agents.Reused),
			"squads_created", len(resp.Squads.Created),
			"squads_reused", len(resp.Squads.Reused),
		)...)

	writeJSON(w, http.StatusCreated, resp)
}

// validateTeamTemplateModelOverrides rejects overrides that reference agents
// not present in the template.
func validateTeamTemplateModelOverrides(tmpl teamtmpl.TeamTemplate, overrides map[string]string) error {
	known := make(map[string]struct{}, len(tmpl.Agents))
	for _, a := range tmpl.Agents {
		known[a.Name] = struct{}{}
	}
	for name := range overrides {
		if _, ok := known[name]; !ok {
			return fmt.Errorf("model_overrides references unknown agent: %s", name)
		}
	}
	return nil
}

// fetchTeamTemplateRemoteSkills resolves every remote SkillDef (SourceURL
// non-empty) in parallel, outside any transaction. Returns the imports keyed by
// the original skill index, plus the URLs that failed to fetch. The returned
// slice of failed URLs is non-empty iff at least one remote skill failed.
func (h *Handler) fetchTeamTemplateRemoteSkills(r *http.Request, skills []teamtmpl.SkillDef) (map[int]*importedSkill, []string) {
	refs := make([]agenttmpl.TemplateSkillRef, 0, len(skills))
	refIdx := make([]int, 0, len(skills))
	for i, s := range skills {
		if s.SourceURL == "" {
			continue
		}
		refs = append(refs, agenttmpl.TemplateSkillRef{SourceURL: s.SourceURL})
		refIdx = append(refIdx, i)
	}
	if len(refs) == 0 {
		return map[int]*importedSkill{}, nil
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	fetchCtx, cancel := context.WithTimeout(r.Context(), importFetchTimeout)
	defer cancel()

	imported, failed := fetchTemplateSkillsParallel(fetchCtx, httpClient, refs)
	byIdx := make(map[int]*importedSkill, len(imported))
	for j, imp := range imported {
		if imp == nil {
			continue
		}
		byIdx[refIdx[j]] = imp
	}
	return byIdx, failed
}
