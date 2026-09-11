package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/resourcetmpl"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// marketplace.go implements the F-523 (CLO-617) marketplace capability: the
// agent/squad marketplace where workspace members publish portable resource
// templates for teammates to browse and one-click import.
//
// Endpoints (all under /api/marketplace):
//
//	POST /api/marketplace/listings              — publish a listing
//	GET  /api/marketplace/listings              — browse/search (no template)
//	GET  /api/marketplace/listings/{id}         — detail (includes template)
//	POST /api/marketplace/listings/{id}/archive — archive/restore
//	GET  /api/marketplace/listings/{id}/download — download template file
//	POST /api/marketplace/listings/{id}/downloads — report a download (deduped)
//
// The listing template payload IS the portable resourcetmpl.Template — the
// marketplace introduces no second template protocol (PRD §0/§2.1). Import
// reuses the existing /api/templates/{validate,apply} endpoints unchanged.

// marketplaceCategories is the server-side category whitelist (PRD §2.4).
// New categories require a code/migration change.
var marketplaceCategories = map[string]bool{
	"content-creation":     true,
	"dev-programming":      true,
	"data-analysis":        true,
	"ai-agent":             true,
	"knowledge-management": true,
	"business-ops":         true,
	"education":            true,
	"professional":         true,
	"it-ops-security":      true,
	"life-service":         true,
	"other":                true,
}

// sortedMarketplaceCategories is the stable ordering for UI rendering.
func sortedMarketplaceCategories() []string {
	out := make([]string, 0, len(marketplaceCategories))
	for c := range marketplaceCategories {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// semverRe is a deliberately loose SemVer check (major.minor.patch, optional
// pre-release/build). It accepts "1.0.0", "1.0", "1", "v1.2.3" and rejects
// free-form strings that would confuse the version column.
var semverRe = regexp.MustCompile(`^v?\d+\.\d+(\.\d+)?([-+][0-9A-Za-z.-]+)?$`)

const (
	// marketplaceMaxTitleLength bounds the listing title (also the UNIQUE
	// (source_workspace_id, title) key).
	marketplaceMaxTitleLength = 120
	marketplaceMaxSummaryLen  = 2000
	marketplaceMaxTags        = 20
	marketplaceMaxTagLength   = 64
	// marketplaceDefaultPageSize / marketplaceMaxPageSize bound browse
	// pagination (PRD §4.2).
	marketplaceDefaultPageSize = 20
	marketplaceMaxPageSize     = 100
)

// ---------------------------------------------------------------------------
// Wire types
// ---------------------------------------------------------------------------

// PublishMarketplaceListingRequest is the POST /api/marketplace/listings body.
// Either ResourceID (server-side export from an existing resource) or
// Template (an already-exported template document) must be provided; when both
// are present ResourceID wins (PRD §4.1).
type PublishMarketplaceListingRequest struct {
	Kind       string                    `json:"kind"`
	ResourceID string                    `json:"resource_id,omitempty"`
	Template   json.RawMessage           `json:"template,omitempty"`
	Metadata   MarketplacePublishMetadata `json:"metadata"`
}

// MarketplacePublishMetadata carries the marketplace display fields. On
// publish they are the source of truth and are written back into the template
// metadata (PRD §2.6).
type MarketplacePublishMetadata struct {
	Title    string   `json:"title"`
	Summary  string   `json:"summary,omitempty"`
	Category string   `json:"category"`
	Tags     []string `json:"tags,omitempty"`
	Version  string   `json:"version,omitempty"`
}

// MarketplaceListing is the wire shape of one listing. Template is only
// populated on detail/download responses — the browse list never carries it
// (PRD §4.2 / R5).
type MarketplaceListing struct {
	ID                string          `json:"id"`
	Kind              string          `json:"kind"`
	Title             string          `json:"title"`
	Summary           string          `json:"summary"`
	Category          string          `json:"category"`
	Tags              []string        `json:"tags"`
	Version           string          `json:"version"`
	AuthorID          string          `json:"author_id"`
	AuthorDisplayName string          `json:"author_display_name"`
	SourceWorkspaceID string          `json:"source_workspace_id"`
	TemplateID        string          `json:"template_id"`
	Status            string          `json:"status"`
	Downloads         int64           `json:"downloads"`
	Installs          int64           `json:"installs"`
	CreatedAt         string          `json:"created_at"`
	UpdatedAt         string          `json:"updated_at"`
	// Template is the full resourcetmpl.Template document (detail only).
	Template json.RawMessage `json:"template,omitempty"`
}

// ListMarketplaceListingsResponse is the GET /api/marketplace/listings body.
type ListMarketplaceListingsResponse struct {
	Items    []MarketplaceListing `json:"items"`
	Total    int64                `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
}

// ArchiveMarketplaceListingRequest is the archive/restore body.
type ArchiveMarketplaceListingRequest struct {
	Restore bool `json:"restore"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func marketplaceListingFromRow(row db.ListMarketplaceListingsRow) MarketplaceListing {
	return MarketplaceListing{
		ID:                uuidToString(row.ID),
		Kind:              row.Kind,
		Title:             row.Title,
		Summary:           row.Summary,
		Category:          row.Category,
		Tags:              row.Tags,
		Version:           row.Version,
		AuthorID:          uuidToString(row.AuthorID),
		AuthorDisplayName: row.AuthorDisplayName,
		SourceWorkspaceID: uuidToString(row.SourceWorkspaceID),
		TemplateID:        row.TemplateID,
		Status:            row.Status,
		Downloads:         row.Downloads,
		Installs:          row.Installs,
		CreatedAt:         timestampToString(row.CreatedAt),
		UpdatedAt:         timestampToString(row.UpdatedAt),
	}
}

// loadMarketplaceListing loads a listing by id (any status).
func (h *Handler) loadMarketplaceListing(ctx context.Context, id string) (db.MarketplaceListing, error) {
	idUUID, err := util.ParseUUID(id)
	if err != nil {
		return db.MarketplaceListing{}, pgx.ErrNoRows
	}
	return h.Queries.GetMarketplaceListing(ctx, idUUID)
}

// marketplaceListingVisible reports whether the caller may see this listing.
// Published listings are visible to every workspace member; archived ones only
// to the publisher or a workspace owner/admin (PRD §4.3).
func marketplaceListingVisible(l db.MarketplaceListing, member db.Member) bool {
	if l.Status == "published" {
		return true
	}
	if member.Role == "owner" || member.Role == "admin" {
		return true
	}
	return uuidToString(l.AuthorID) == uuidToString(member.UserID)
}

// marketplaceListingManageable reports whether the caller may archive/restore:
// the publisher or a workspace owner/admin (PRD §4.4).
func marketplaceListingManageable(l db.MarketplaceListing, member db.Member) bool {
	if member.Role == "owner" || member.Role == "admin" {
		return true
	}
	return uuidToString(l.AuthorID) == uuidToString(member.UserID)
}

// validateMarketplaceMetadata validates the publish metadata fields and
// returns a human-readable error message (or "").
func validateMarketplaceMetadata(m MarketplacePublishMetadata) string {
	title := strings.TrimSpace(m.Title)
	if title == "" {
		return "metadata.title is required"
	}
	if len(title) > marketplaceMaxTitleLength {
		return fmt.Sprintf("metadata.title must be at most %d characters", marketplaceMaxTitleLength)
	}
	if m.Category != "" && !marketplaceCategories[m.Category] {
		return fmt.Sprintf("metadata.category %q is not a recognised category", m.Category)
	}
	if len(m.Tags) > marketplaceMaxTags {
		return fmt.Sprintf("metadata.tags must have at most %d entries", marketplaceMaxTags)
	}
	for _, tag := range m.Tags {
		if len(tag) > marketplaceMaxTagLength {
			return fmt.Sprintf("metadata.tags entry %q exceeds %d characters", tag, marketplaceMaxTagLength)
		}
	}
	if m.Version != "" && !semverRe.MatchString(m.Version) {
		return "metadata.version must be a valid semantic version (e.g. 1.0.0)"
	}
	if len(m.Summary) > marketplaceMaxSummaryLen {
		return fmt.Sprintf("metadata.summary must be at most %d characters", marketplaceMaxSummaryLen)
	}
	return ""
}

// buildResourceTemplateForPublish produces the template for a listing. When
// ResourceID is set it runs the same server-side export path as
// POST /api/templates/export (same permission checks, same redaction, same
// deterministic template_id); otherwise the caller-supplied template document
// is validated. The resulting document is always run through
// resourcetmpl.Validate + DetectSecrets before being stored (PRD §2.2 / R1).
func (h *Handler) buildResourceTemplateForPublish(
	ctx context.Context,
	wsUUID pgtype.UUID,
	member db.Member,
	userID string,
	kind string,
	resourceID string,
	tmplRaw json.RawMessage,
) (resourcetmpl.Template, []resourcetmpl.Warning, error) {
	if resourceID != "" {
		// Server-side export path: identical to /api/templates/export. We
		// reuse the full export logic (permissions, spec building, redaction)
		// so a published listing is byte-for-byte the same portable template
		// a teammate could export themselves.
		return h.exportResourceTemplateInternal(ctx, wsUUID, member, userID, kind, resourceID)
	}
	// Direct template path: the document is validated but the export
	// permission model does not apply (nothing is being read from the
	// workspace). Structural validation + secret scanning still apply.
	var tmpl resourcetmpl.Template
	if err := json.Unmarshal(tmplRaw, &tmpl); err != nil {
		return tmpl, nil, fmt.Errorf("template is not a valid resource template: %v", err)
	}
	if tmpl.Kind != kind {
		return tmpl, nil, fmt.Errorf("template.kind %q does not match kind %q", tmpl.Kind, kind)
	}
	return tmpl, nil, nil
}

// exportResourceTemplateInternal is the export logic shared by the export
// endpoint and the marketplace publish path. It mirrors ExportResourceTemplate
// exactly: permission checks, spec building, metadata defaults, and the
// finished-template secret scan. The audit-log write stays in the HTTP
// handlers (marketplace records its own audit action).
func (h *Handler) exportResourceTemplateInternal(
	ctx context.Context,
	wsUUID pgtype.UUID,
	member db.Member,
	userID string,
	kind string,
	resourceID string,
) (resourcetmpl.Template, []resourcetmpl.Warning, error) {
	membersMode := resourcetmpl.MembersEmbedded

	var tmpl resourcetmpl.Template
	var warnings []resourcetmpl.Warning

	switch kind {
	case resourcetmpl.KindAgent:
		agentRow, err := h.loadAgentByIDOrName(ctx, wsUUID, resourceID)
		if err != nil {
			return tmpl, nil, errors.New("resource not found")
		}
		if !canViewAgentSecrets(agentRow, userID, member.Role) {
			return tmpl, nil, errors.New("you do not have permission to export this resource")
		}
		spec, err := h.buildAgentSpec(ctx, agentRow)
		if err != nil {
			return tmpl, nil, err
		}
		tmpl = resourcetmpl.Template{
			SchemaVersion: resourcetmpl.SchemaVersion,
			TemplateID:    deterministicTemplateID(uuidToString(wsUUID), kind, uuidToString(agentRow.ID)),
			Kind:          resourcetmpl.KindAgent,
			Spec:          resourcetmpl.Spec{Agent: &spec},
		}
	case resourcetmpl.KindSquad:
		squad, err := h.loadSquadByIDOrName(ctx, wsUUID, resourceID)
		if err != nil {
			return tmpl, nil, errors.New("resource not found")
		}
		spec, missing, warns, err := h.buildSquadSpec(ctx, wsUUID, squad, membersMode)
		if err != nil {
			return tmpl, nil, err
		}
		var unreadable []string
		for _, dep := range missing {
			if !canViewAgentSecrets(dep.agent, userID, member.Role) {
				unreadable = append(unreadable, dep.name)
			}
		}
		if len(unreadable) > 0 {
			sort.Strings(unreadable)
			return tmpl, nil, fmt.Errorf("you do not have permission to export this squad; unreadable members: %s", strings.Join(unreadable, ", "))
		}
		warnings = warns
		tmpl = resourcetmpl.Template{
			SchemaVersion: resourcetmpl.SchemaVersion,
			TemplateID:    deterministicTemplateID(uuidToString(wsUUID), kind, uuidToString(squad.ID)),
			Kind:          resourcetmpl.KindSquad,
			Spec:          resourcetmpl.Spec{Squad: &spec},
		}
	default:
		return tmpl, nil, fmt.Errorf("kind must be %q or %q", resourcetmpl.KindAgent, resourcetmpl.KindSquad)
	}

	// Metadata defaults (identical to the export endpoint).
	authorDisplayName := userID
	if user, err := h.Queries.GetUser(ctx, parseUUID(userID)); err == nil {
		authorDisplayName = user.Name
	}
	tmpl.Metadata = resourcetmpl.Metadata{
		Name:            tmplName(tmpl),
		Description:     templateDescription(tmpl),
		Author:          resourcetmpl.Author{ID: userID, DisplayName: authorDisplayName},
		Version:         "1.0.0",
		Visibility:      resourcetmpl.VisibilityWorkspace,
		SourceWorkspace: uuidToString(wsUUID),
		CreatedAt:       time.Now().UTC().Format(time.RFC3339),
	}

	// Belt-and-suspenders: run the secret scanner over the finished template.
	raw, err := json.Marshal(tmpl)
	if err != nil {
		return tmpl, nil, err
	}
	if secrets := resourcetmpl.DetectSecrets(raw); len(secrets) > 0 {
		return tmpl, nil, fmt.Errorf("template contains plaintext secrets: %s", secrets[0].Message)
	}
	return tmpl, warnings, nil
}

// templateDescription mirrors the export endpoint's description default.
func templateDescription(tmpl resourcetmpl.Template) string {
	if tmpl.Spec.Agent != nil {
		return tmpl.Spec.Agent.Description
	}
	if tmpl.Spec.Squad != nil {
		return tmpl.Spec.Squad.Description
	}
	return ""
}

// ---------------------------------------------------------------------------
// Publish
// ---------------------------------------------------------------------------

// PublishMarketplaceListing handles POST /api/marketplace/listings.
func (h *Handler) PublishMarketplaceListing(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	// Agent-actor tokens may never publish (they carry task credentials, not
	// a workspace member identity), mirroring the export gate.
	actorType, _ := h.resolveActor(r, userID, workspaceID)
	if actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents may not publish marketplace listings")
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	var req PublishMarketplaceListingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Kind != resourcetmpl.KindAgent && req.Kind != resourcetmpl.KindSquad {
		writeError(w, http.StatusBadRequest, `kind must be "agent" or "squad"`)
		return
	}
	if req.ResourceID == "" && len(req.Template) == 0 {
		writeError(w, http.StatusBadRequest, "either resource_id or template is required")
		return
	}
	if msg := validateMarketplaceMetadata(req.Metadata); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	// Build + validate the template (shared with the export endpoint).
	tmpl, warnings, err := h.buildResourceTemplateForPublish(r.Context(), wsUUID, member, userID, req.Kind, req.ResourceID, req.Template)
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case strings.Contains(err.Error(), "resource not found"):
			status = http.StatusNotFound
		case strings.Contains(err.Error(), "permission"):
			status = http.StatusForbidden
		}
		writeError(w, status, err.Error())
		return
	}
	// Structural validation must pass (direct-template uploads bypass the
	// export handler's guarantees).
	raw, err := json.Marshal(tmpl)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode template")
		return
	}
	report := resourcetmpl.Validate(raw)
	if report.HasErrors() {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":  "template is not valid",
			"errors": report.Errors,
		})
		return
	}
	// The secret scanner is the marketplace's own backstop (PRD R1): the
	// direct-template path never went through the export handler's scan.
	if secrets := resourcetmpl.DetectSecrets(raw); len(secrets) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":  CodeSecretDetectedHTTP,
			"message": "template contains plaintext secrets: " + secrets[0].Message,
		})
		return
	}

	// Apply the publish metadata to the stored document (PRD §2.6).
	title := strings.TrimSpace(req.Metadata.Title)
	summary := req.Metadata.Summary
	if summary == "" {
		summary = templateDescription(tmpl)
	}
	version := req.Metadata.Version
	if version == "" {
		version = tmpl.Metadata.Version
	}
	if version == "" {
		version = "1.0.0"
	}
	category := req.Metadata.Category
	if category == "" {
		category = "other"
	}
	tags := req.Metadata.Tags
	if tags == nil {
		tags = []string{}
	}
	tmpl.Metadata.Name = title
	tmpl.Metadata.Description = summary
	tmpl.Metadata.Version = version
	tmpl.Metadata.Tags = tags
	storedTemplate, err := json.Marshal(tmpl)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode template")
		return
	}

	authorDisplayName := userID
	if user, err := h.Queries.GetUser(r.Context(), parseUUID(userID)); err == nil {
		authorDisplayName = user.Name
	}

	// Listing + stats are created in one transaction (PRD §4.1).
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	created, err := qtx.CreateMarketplaceListing(r.Context(), db.CreateMarketplaceListingParams{
		Kind:              req.Kind,
		Title:             title,
		Summary:           summary,
		Category:          category,
		Tags:              tags,
		Version:           version,
		AuthorID:          parseUUID(userID),
		AuthorDisplayName: authorDisplayName,
		SourceWorkspaceID: wsUUID,
		TemplateID:        tmpl.TemplateID,
		Template:          storedTemplate,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a listing with this title already exists in this workspace")
			return
		}
		slog.Error("marketplace publish: insert listing failed",
			append(logger.RequestAttrs(r), "title", title, "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create listing")
		return
	}
	if err := qtx.CreateMarketplaceStats(r.Context(), created.ID); err != nil {
		slog.Error("marketplace publish: create stats failed",
			append(logger.RequestAttrs(r), "listing_id", uuidToString(created.ID), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create listing")
		return
	}

	auditDetails, _ := json.Marshal(map[string]any{
		"listing_id":  uuidToString(created.ID),
		"kind":        req.Kind,
		"template_id": tmpl.TemplateID,
		"version":     version,
		"title":       title,
		"warn_count":  len(warnings),
	})
	if _, err := qtx.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: wsUUID,
		IssueID:     pgtype.UUID{},
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     parseUUID(userID),
		Action:      "marketplace_listing_published",
		Details:     auditDetails,
	}); err != nil {
		slog.Warn("marketplace publish: activity_log write failed",
			append(logger.RequestAttrs(r), "error", err)...)
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("marketplace publish: commit failed",
			append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create listing")
		return
	}

	writeJSON(w, http.StatusOK, MarketplaceListing{
		ID:                uuidToString(created.ID),
		Kind:              created.Kind,
		Title:             created.Title,
		Summary:           created.Summary,
		Category:          created.Category,
		Tags:              created.Tags,
		Version:           created.Version,
		AuthorID:          uuidToString(created.AuthorID),
		AuthorDisplayName: created.AuthorDisplayName,
		SourceWorkspaceID: uuidToString(created.SourceWorkspaceID),
		TemplateID:        created.TemplateID,
		Status:            created.Status,
		Downloads:         0,
		Installs:          0,
		CreatedAt:         timestampToString(created.CreatedAt),
		UpdatedAt:         timestampToString(created.UpdatedAt),
	})
}

// ---------------------------------------------------------------------------
// Browse / list
// ---------------------------------------------------------------------------

// ListMarketplaceListings handles GET /api/marketplace/listings.
func (h *Handler) ListMarketplaceListings(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	q := r.URL.Query()
	kind := strings.TrimSpace(q.Get("kind"))
	category := strings.TrimSpace(q.Get("category"))
	search := strings.TrimSpace(q.Get("q"))
	sortKey := q.Get("sort")
	if sortKey == "" {
		sortKey = "latest"
	}
	if sortKey != "name" && sortKey != "downloads" && sortKey != "latest" {
		writeError(w, http.StatusBadRequest, `sort must be "latest", "downloads" or "name"`)
		return
	}
	page, err := positiveQueryInt(q.Get("page"), 1)
	if err != nil {
		writeError(w, http.StatusBadRequest, "page must be a positive integer")
		return
	}
	pageSize, err := positiveQueryInt(q.Get("page_size"), marketplaceDefaultPageSize)
	if err != nil {
		writeError(w, http.StatusBadRequest, "page_size must be a positive integer")
		return
	}
	if pageSize > marketplaceMaxPageSize {
		pageSize = marketplaceMaxPageSize
	}

	offset := (page - 1) * pageSize
	rows, err := h.Queries.ListMarketplaceListings(r.Context(), db.ListMarketplaceListingsParams{
		WorkspaceID: wsUUID,
		Kind:        kind,
		Category:    category,
		Search:      search,
		Sort:        sortKey,
		Limit:       int32(pageSize),
		Offset:      int32(offset),
	})
	if err != nil {
		slog.Error("marketplace list: query failed",
			append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list listings")
		return
	}
	total, err := h.Queries.CountMarketplaceListings(r.Context(), db.CountMarketplaceListingsParams{
		WorkspaceID: wsUUID,
		Kind:        kind,
		Category:    category,
		Search:      search,
	})
	if err != nil {
		slog.Error("marketplace list: count failed",
			append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list listings")
		return
	}

	items := make([]MarketplaceListing, 0, len(rows))
	for _, row := range rows {
		items = append(items, marketplaceListingFromRow(row))
	}
	writeJSON(w, http.StatusOK, ListMarketplaceListingsResponse{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// ---------------------------------------------------------------------------
// Detail
// ---------------------------------------------------------------------------

// GetMarketplaceListing handles GET /api/marketplace/listings/{id}.
func (h *Handler) GetMarketplaceListing(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	listing, err := h.loadMarketplaceListing(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "listing not found")
		return
	}
	// MVP is single-workspace: a listing from another workspace is invisible.
	if uuidToString(listing.SourceWorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "listing not found")
		return
	}
	if !marketplaceListingVisible(listing, member) {
		writeError(w, http.StatusNotFound, "listing not found")
		return
	}

	stats, err := h.Queries.GetMarketplaceStats(r.Context(), listing.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		slog.Error("marketplace detail: stats lookup failed",
			append(logger.RequestAttrs(r), "listing_id", uuidToString(listing.ID), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load listing")
		return
	}

	out := MarketplaceListing{
		ID:                uuidToString(listing.ID),
		Kind:              listing.Kind,
		Title:             listing.Title,
		Summary:           listing.Summary,
		Category:          listing.Category,
		Tags:              listing.Tags,
		Version:           listing.Version,
		AuthorID:          uuidToString(listing.AuthorID),
		AuthorDisplayName: listing.AuthorDisplayName,
		SourceWorkspaceID: uuidToString(listing.SourceWorkspaceID),
		TemplateID:        listing.TemplateID,
		Status:            listing.Status,
		Downloads:         stats.Downloads,
		Installs:          stats.Installs,
		CreatedAt:         timestampToString(listing.CreatedAt),
		UpdatedAt:         timestampToString(listing.UpdatedAt),
		Template:          listing.Template,
	}
	writeJSON(w, http.StatusOK, map[string]any{"listing": out})
}

// ---------------------------------------------------------------------------
// Archive / restore
// ---------------------------------------------------------------------------

// ArchiveMarketplaceListing handles POST /api/marketplace/listings/{id}/archive.
func (h *Handler) ArchiveMarketplaceListing(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	listing, err := h.loadMarketplaceListing(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "listing not found")
		return
	}
	if uuidToString(listing.SourceWorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "listing not found")
		return
	}
	if !marketplaceListingManageable(listing, member) {
		writeError(w, http.StatusForbidden, "you do not have permission to archive this listing")
		return
	}

	var req ArchiveMarketplaceListingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	status := "published"
	if !req.Restore {
		status = "archived"
	}

	updated, err := h.Queries.UpdateMarketplaceListingStatus(r.Context(), db.UpdateMarketplaceListingStatusParams{
		ID:     listing.ID,
		Status: status,
	})
	if err != nil {
		slog.Error("marketplace archive: update failed",
			append(logger.RequestAttrs(r), "listing_id", uuidToString(listing.ID), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update listing")
		return
	}

	action := "marketplace_listing_archived"
	if req.Restore {
		action = "marketplace_listing_restored"
	}
	details, _ := json.Marshal(map[string]any{"listing_id": uuidToString(updated.ID)})
	if _, err := h.Queries.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: member.WorkspaceID,
		IssueID:     pgtype.UUID{},
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     member.UserID,
		Action:      action,
		Details:     details,
	}); err != nil {
		slog.Warn("marketplace archive: activity_log write failed",
			append(logger.RequestAttrs(r), "error", err)...)
	}

	writeJSON(w, http.StatusOK, MarketplaceListing{
		ID:                uuidToString(updated.ID),
		Kind:              updated.Kind,
		Title:             updated.Title,
		Summary:           updated.Summary,
		Category:          updated.Category,
		Tags:              updated.Tags,
		Version:           updated.Version,
		AuthorID:          uuidToString(updated.AuthorID),
		AuthorDisplayName: updated.AuthorDisplayName,
		SourceWorkspaceID: uuidToString(updated.SourceWorkspaceID),
		TemplateID:        updated.TemplateID,
		Status:            updated.Status,
		CreatedAt:         timestampToString(updated.CreatedAt),
		UpdatedAt:         timestampToString(updated.UpdatedAt),
	})
}

// ---------------------------------------------------------------------------
// Download + download counting
// ---------------------------------------------------------------------------

// DownloadMarketplaceListingTemplate handles GET
// /api/marketplace/listings/{id}/download: returns the stored template as an
// application/json attachment. The download counter is reported separately via
// POST .../downloads (PRD §4.5 / §4.6), so this stays read-only.
func (h *Handler) DownloadMarketplaceListingTemplate(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}

	listing, err := h.loadMarketplaceListing(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "listing not found")
		return
	}
	if uuidToString(listing.SourceWorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "listing not found")
		return
	}
	if !marketplaceListingVisible(listing, member) {
		writeError(w, http.StatusNotFound, "listing not found")
		return
	}

	filename := fmt.Sprintf("%s-v%s.json", safeFilename(listing.Title), listing.Version)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(listing.Template)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(listing.Template)
}

// ReportMarketplaceDownload handles POST /api/marketplace/listings/{id}/downloads.
// Counting is deduped per (listing, workspace, member) (PRD §4.6 / R2): the
// first report for a member inserts a dedup row and increments the counter;
// later reports are no-ops. The dedup insert and the counter increment run in
// one transaction so a unique-violation race cannot double-count.
func (h *Handler) ReportMarketplaceDownload(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	listing, err := h.loadMarketplaceListing(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "listing not found")
		return
	}
	if uuidToString(listing.SourceWorkspaceID) != workspaceID {
		writeError(w, http.StatusNotFound, "listing not found")
		return
	}
	if listing.Status != "published" {
		writeError(w, http.StatusNotFound, "listing not found")
		return
	}

	var req struct {
		Count int64 `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Count != 1 {
		// The PRD contract is count:1 per report; anything else is a
		// malformed client (we never batch downloads in the MVP UI).
		writeError(w, http.StatusBadRequest, "count must be 1")
		return
	}

	// Dedup + increment in one transaction. The dedup insert uses
	// ON CONFLICT DO NOTHING RETURNING, so a concurrent report from the same
	// member returns "no row" instead of a 23505 abort — the transaction is
	// never poisoned, so the subsequent read/increment below stays valid.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	insertedID, err := qtx.InsertMarketplaceDownloadDedup(r.Context(), db.InsertMarketplaceDownloadDedupParams{
		ListingID:   listing.ID,
		WorkspaceID: wsUUID,
		MemberID:    member.UserID,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		slog.Error("marketplace download report: dedup insert failed",
			append(logger.RequestAttrs(r), "listing_id", uuidToString(listing.ID), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to record download")
		return
	}

	// pgx.ErrNoRows → ON CONFLICT DO NOTHING took effect: this member already
	// counted. Return the current count unchanged (the tx holds nothing to
	// commit, but rolling back is also a no-op — no poisoned state to recover
	// from).
	if errors.Is(err, pgx.ErrNoRows) || !insertedID.Valid {
		stats, err := qtx.GetMarketplaceStats(r.Context(), listing.ID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("marketplace download report: stats lookup failed",
				append(logger.RequestAttrs(r), "listing_id", uuidToString(listing.ID), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to record download")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"downloads": stats.Downloads})
		return
	}

	downloads, err := qtx.IncrementMarketplaceDownloads(r.Context(), listing.ID)
	if err != nil {
		slog.Error("marketplace download report: increment failed",
			append(logger.RequestAttrs(r), "listing_id", uuidToString(listing.ID), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to record download")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("marketplace download report: commit failed",
			append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to record download")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"downloads": downloads})
}

// safeFilename strips characters that would break a Content-Disposition header.
func safeFilename(title string) string {
	var b strings.Builder
	for _, r := range title {
		if r == '"' || r == '\\' || r == '\n' || r == '\r' || r == '/' {
			b.WriteRune('-')
			continue
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return "listing"
	}
	return b.String()
}

// positiveQueryInt parses a positive integer query parameter with a default.
func positiveQueryInt(raw string, def int) (int, error) {
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, errors.New("not a positive integer")
	}
	return n, nil
}
