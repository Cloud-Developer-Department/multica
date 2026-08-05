package resourcetmpl

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// known-field sets. Unknown-field detection does not rely on struct tags; it
// compares the keys actually present in the raw JSON against these explicit
// allow-lists. Because non-portable fields (runtime_id, owner_id, custom_env
// the value map, mcp_config the raw auth object, invocation_targets,
// composio_toolkit_allowlist, avatar_url, archived_*, timestamps, IDs, ...) are
// intentionally NOT in these sets, their presence surfaces as
// TEMPLATE_INVALID — that is how the "non-portable fields forbidden" rule is
// enforced at the type layer.
var (
	topLevelKnown = set("schema_version", "template_id", "kind", "metadata", "spec")
	metadataKnown = set("name", "description", "author", "version", "visibility",
		"tags", "source_workspace", "created_at")
	authorKnown = set("id", "display_name")
	specKnown   = set("agent", "squad")
	agentKnown  = set("name", "description", "instructions", "model", "thinking_level",
		"service_tier", "max_concurrent_tasks", "permission_mode", "public_to_workspace",
		"custom_args", "skills", "custom_env_keys", "mcp_servers")
	skillKnown     = set("name", "source_url", "enabled")
	envKeyKnown    = set("key", "required", "description")
	mcpServerKnown = set("name", "transport", "requires_auth", "config_skeleton")
	squadKnown     = set("name", "description", "instructions", "members_mode",
		"leader_ref", "members")
	memberKnown = set("ref", "role", "agent")
)

// semverRe matches SemVer 2.0 core: MAJOR.MINOR.PATCH with optional
// pre-release and build metadata. Used for metadata.version.
var semverRe = regexp.MustCompile(
	`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

// Warning code emitted when public_to is requested without the workspace-wide
// scope: in a fresh workspace the original allow-list cannot be ported, so the
// agent ends up effectively private. Non-blocking — surfaces intent to the user.
const WarningPublicToNoScope = "PUBLIC_TO_NO_SCOPE"

// Validate runs the full validation pipeline on a template JSON document and
// returns a Report collecting EVERY problem found. It never fails fast: all
// structural, version, enum, unknown-field, referential and secret checks run
// independently and their results are merged, so the frontend / CLI can render
// the complete list in one pass.
//
// A Report with no Errors means the template is structurally and semantically
// acceptable for import. Warnings may still be present.
func Validate(raw []byte) *Report {
	r := &Report{}

	// Lenient decode into the typed shape for semantic checks. If the bytes are
	// not valid JSON (or not an object), nothing else can run meaningfully.
	var tmpl Template
	if err := json.Unmarshal(raw, &tmpl); err != nil {
		r.AddError(CodeTemplateInvalid, "", "malformed JSON: "+err.Error())
		return r
	}

	// Each pass appends to the same report; order only affects display.
	root, _ := asMap(json.RawMessage(raw))
	collectUnknownFields(raw, r)
	validateVersion(tmpl.SchemaVersion, r)
	validateStructure(root, &tmpl, r)
	validateMetadata(tmpl.Metadata, r)
	switch {
	case tmpl.Kind == KindAgent && tmpl.Spec.Agent != nil:
		validateAgent(tmpl.Spec.Agent, "spec.agent", r)
	case tmpl.Kind == KindSquad && tmpl.Spec.Squad != nil:
		validateSquad(tmpl.Spec.Squad, r)
	}
	for _, e := range DetectSecrets(raw) {
		r.Errors = append(r.Errors, e)
	}

	return r
}

// validateVersion checks schema_version presence and major compatibility.
func validateVersion(v string, r *Report) {
	if strings.TrimSpace(v) == "" {
		r.AddError(CodeTemplateInvalid, "schema_version", "schema_version is required")
		return
	}
	if !SupportsVersion(v) {
		r.Errors = append(r.Errors, versionError(v))
	}
}

// validateStructure enforces the kind enum and the kind/spec consistency: the
// spec object must carry exactly one of {agent, squad} and that key must equal
// Kind. Unknown spec keys are reported separately by collectUnknownFields.
func validateStructure(root map[string]json.RawMessage, tmpl *Template, r *Report) {
	if tmpl.Kind != KindAgent && tmpl.Kind != KindSquad {
		r.AddError(CodeTemplateInvalid, "kind",
			fmt.Sprintf("kind %q must be %q or %q", tmpl.Kind, KindAgent, KindSquad))
	}

	specRaw, hasSpec := root["spec"]
	if !hasSpec {
		r.AddError(CodeTemplateInvalid, "spec", "spec is required")
		return
	}
	specMap, ok := asMap(specRaw)
	if !ok {
		r.AddError(CodeTemplateInvalid, "spec", "spec must be an object")
		return
	}

	hasAgent := keyExists(specMap, "agent")
	hasSquad := keyExists(specMap, "squad")
	known := 0
	if hasAgent {
		known++
	}
	if hasSquad {
		known++
	}
	switch {
	case known == 0:
		r.AddError(CodeTemplateInvalid, "spec",
			`spec must contain exactly one of "agent" or "squad"`)
	case known > 1:
		r.AddError(CodeTemplateInvalid, "spec",
			`spec must contain exactly one of "agent" or "squad" (got both)`)
	default:
		present := KindAgent
		if hasSquad {
			present = KindSquad
		}
		if tmpl.Kind == KindAgent || tmpl.Kind == KindSquad {
			if present != tmpl.Kind {
				r.AddError(CodeTemplateInvalid, "spec",
					fmt.Sprintf("kind/spec mismatch: kind=%q but spec contains %q", tmpl.Kind, present))
			}
		}
	}
}

// validateMetadata checks the required metadata fields and the v1 enum bounds
// (SemVer version, workspace-only visibility, RFC3339 created_at when set).
func validateMetadata(md Metadata, r *Report) {
	if strings.TrimSpace(md.Name) == "" {
		r.AddError(CodeTemplateInvalid, "metadata.name", "metadata.name is required")
	}
	switch {
	case md.Version == "":
		r.AddError(CodeTemplateInvalid, "metadata.version", "metadata.version is required")
	case !semverRe.MatchString(strings.TrimSpace(md.Version)):
		r.AddError(CodeTemplateInvalid, "metadata.version",
			fmt.Sprintf("metadata.version %q is not valid SemVer (MAJOR.MINOR.PATCH)", md.Version))
	}
	switch {
	case md.Visibility == "":
		r.AddError(CodeTemplateInvalid, "metadata.visibility", "metadata.visibility is required")
	case md.Visibility != VisibilityWorkspace:
		r.AddError(CodeTemplateInvalid, "metadata.visibility",
			fmt.Sprintf("metadata.visibility must be %q in this release", VisibilityWorkspace))
	}
	if md.CreatedAt != "" {
		if _, err := time.Parse(time.RFC3339, md.CreatedAt); err != nil {
			r.AddError(CodeTemplateInvalid, "metadata.created_at",
				fmt.Sprintf("metadata.created_at must be RFC3339, got %q", md.CreatedAt))
		}
	}
}

// validateAgent checks an AgentSpec subtree rooted at base (e.g. "spec.agent"
// or "spec.squad.members[1].agent").
func validateAgent(a *AgentSpec, base string, r *Report) {
	if strings.TrimSpace(a.Name) == "" {
		r.AddError(CodeTemplateInvalid, base+".name", "agent name is required")
	}
	if a.PermissionMode != "" && a.PermissionMode != PermissionPrivate && a.PermissionMode != PermissionPublicTo {
		r.AddError(CodeTemplateInvalid, base+".permission_mode",
			fmt.Sprintf("permission_mode %q must be %q or %q", a.PermissionMode, PermissionPrivate, PermissionPublicTo))
	}
	// public_to without the workspace-wide scope loses the original allow-list
	// in a fresh workspace and behaves like private. Warn, do not reject.
	if a.PermissionMode == PermissionPublicTo && !a.PublicToWorkspace {
		r.AddWarning(WarningPublicToNoScope, base,
			"permission_mode=public_to with public_to_workspace=false is effectively private in a fresh workspace")
	}
	for i := range a.Skills {
		if strings.TrimSpace(a.Skills[i].Name) == "" {
			r.AddError(CodeTemplateInvalid, fmt.Sprintf("%s.skills[%d].name", base, i),
				"skill name is required")
		}
	}
	for i := range a.MCPServers {
		mp := fmt.Sprintf("%s.mcp_servers[%d]", base, i)
		if strings.TrimSpace(a.MCPServers[i].Name) == "" {
			r.AddError(CodeTemplateInvalid, mp+".name", "mcp_server name is required")
		}
		if strings.TrimSpace(a.MCPServers[i].Transport) == "" {
			r.AddError(CodeTemplateInvalid, mp+".transport", "mcp_server transport is required")
		}
	}
}

// validateSquad checks a SquadSpec: members_mode enum, ref uniqueness,
// leader_ref resolution + role, and the embedded/references agent-presence
// rules. Embedded members recurse into validateAgent.
func validateSquad(s *SquadSpec, r *Report) {
	const base = "spec.squad"

	if strings.TrimSpace(s.Name) == "" {
		r.AddError(CodeTemplateInvalid, base+".name", "squad name is required")
	}
	if s.MembersMode != MembersEmbedded && s.MembersMode != MembersReferences {
		r.AddError(CodeTemplateInvalid, base+".members_mode",
			fmt.Sprintf("members_mode %q must be %q or %q", s.MembersMode, MembersEmbedded, MembersReferences))
	}
	if strings.TrimSpace(s.LeaderRef) == "" {
		r.AddError(CodeTemplateInvalid, base+".leader_ref", "leader_ref is required")
	}
	if len(s.Members) == 0 {
		r.AddError(CodeTemplateInvalid, base+".members", "squad must have at least one member")
		return
	}

	seen := make(map[string]int, len(s.Members))
	for i := range s.Members {
		mem := s.Members[i]
		mp := fmt.Sprintf("%s.members[%d]", base, i)

		if strings.TrimSpace(mem.Ref) == "" {
			r.AddError(CodeTemplateInvalid, mp+".ref", "member ref is required")
		} else if prev, exists := seen[mem.Ref]; exists {
			r.AddError(CodeTemplateInvalid, mp+".ref",
				fmt.Sprintf("duplicate member ref %q (first seen at members[%d])", mem.Ref, prev))
		} else {
			seen[mem.Ref] = i
		}

		if strings.TrimSpace(mem.Role) == "" {
			r.AddError(CodeTemplateInvalid, mp+".role", "member role is required")
		}

		switch s.MembersMode {
		case MembersEmbedded:
			if mem.Agent == nil {
				r.AddError(CodeTemplateInvalid, mp+".agent",
					"members_mode=embedded requires each member to embed an agent")
			} else {
				validateAgent(mem.Agent, mp+".agent", r)
			}
		case MembersReferences:
			if mem.Agent != nil {
				r.AddError(CodeTemplateInvalid, mp+".agent",
					"members_mode=references requires members[].agent to be omitted")
			}
		}
	}

	// leader_ref must resolve to a member whose role is leader. Only run when
	// the ref is set; the empty-ref error above already covers the absence.
	if strings.TrimSpace(s.LeaderRef) != "" {
		idx := -1
		for i := range s.Members {
			if s.Members[i].Ref == s.LeaderRef {
				idx = i
				break
			}
		}
		switch {
		case idx < 0:
			r.AddError(CodeTemplateInvalid, base+".leader_ref",
				fmt.Sprintf("leader_ref %q does not match any member ref", s.LeaderRef))
		case s.Members[idx].Role != RoleLeader:
			r.AddError(CodeTemplateInvalid, base+".leader_ref",
				fmt.Sprintf("leader_ref %q points to members[%d] whose role is %q, must be %q",
					s.LeaderRef, idx, s.Members[idx].Role, RoleLeader))
		}
	}
}

// collectUnknownFields walks the raw JSON and reports every key that is not in
// the relevant allow-list, with a precise dotted/[index] path. It recurses
// only into known containers, so an unknown branch is reported once (at its
// root key) rather than descending into arbitrary user payloads. The opaque
// mcp_servers[].config_skeleton object is intentionally not recursed: its keys
// are free-form structural hints; the secret scanner still inspects its values.
func collectUnknownFields(raw []byte, r *Report) {
	root, ok := asMap(raw)
	if !ok {
		return
	}
	checkUnknownKeys(root, topLevelKnown, "", r)

	if metaRaw, ok := root["metadata"]; ok {
		if m, ok := asMap(metaRaw); ok {
			checkUnknownKeys(m, metadataKnown, "metadata", r)
			if authorRaw, ok := m["author"]; ok {
				if am, ok := asMap(authorRaw); ok {
					checkUnknownKeys(am, authorKnown, "metadata.author", r)
				}
			}
		}
	}

	if specRaw, ok := root["spec"]; ok {
		specMap, ok := asMap(specRaw)
		if !ok {
			return
		}
		checkUnknownKeys(specMap, specKnown, "spec", r)
		if agentRaw, ok := specMap["agent"]; ok {
			collectAgentUnknown(agentRaw, "spec.agent", r)
		}
		if squadRaw, ok := specMap["squad"]; ok {
			collectSquadUnknown(squadRaw, "spec.squad", r)
		}
	}
}

// collectAgentUnknown checks unknown keys on an AgentSpec object and recurses
// into its typed sub-collections (skills, custom_env_keys, mcp_servers).
func collectAgentUnknown(raw json.RawMessage, base string, r *Report) {
	m, ok := asMap(raw)
	if !ok {
		return
	}
	checkUnknownKeys(m, agentKnown, base, r)
	collectSliceObjects(m["skills"], base+".skills", skillKnown, r)
	collectSliceObjects(m["custom_env_keys"], base+".custom_env_keys", envKeyKnown, r)
	if serversRaw, ok := m["mcp_servers"]; ok {
		arr, ok := asSlice(serversRaw)
		if !ok {
			return
		}
		for i, el := range arr {
			em, ok := asMap(el)
			if !ok {
				continue
			}
			mp := fmt.Sprintf("%s[%d]", base+".mcp_servers", i)
			checkUnknownKeys(em, mcpServerKnown, mp, r)
			// config_skeleton contents are intentionally free-form: do not
			// flag its inner keys. DetectSecrets still scans their values.
		}
	}
}

// collectSquadUnknown checks unknown keys on a SquadSpec object and recurses
// into members, validating each member's keys and any embedded agent.
func collectSquadUnknown(raw json.RawMessage, base string, r *Report) {
	m, ok := asMap(raw)
	if !ok {
		return
	}
	checkUnknownKeys(m, squadKnown, base, r)
	membersRaw, ok := m["members"]
	if !ok {
		return
	}
	arr, ok := asSlice(membersRaw)
	if !ok {
		return
	}
	for i, el := range arr {
		em, ok := asMap(el)
		if !ok {
			continue
		}
		mp := fmt.Sprintf("%s.members[%d]", base, i)
		checkUnknownKeys(em, memberKnown, mp, r)
		if agentRaw, ok := em["agent"]; ok {
			collectAgentUnknown(agentRaw, mp+".agent", r)
		}
	}
}

// collectSliceObjects checks unknown keys for each object element of a JSON
// array (used for skills / custom_env_keys). No-op for missing or non-array.
func collectSliceObjects(raw json.RawMessage, base string, known map[string]bool, r *Report) {
	arr, ok := asSlice(raw)
	if !ok {
		return
	}
	for i, el := range arr {
		em, ok := asMap(el)
		if !ok {
			continue
		}
		checkUnknownKeys(em, known, fmt.Sprintf("%s[%d]", base, i), r)
	}
}

// checkUnknownKeys reports every key of m not present in known, rooted at base.
func checkUnknownKeys(m map[string]json.RawMessage, known map[string]bool, base string, r *Report) {
	for k := range m {
		if known[k] {
			continue
		}
		path := k
		if base != "" {
			path = base + "." + k
		}
		r.AddError(CodeTemplateInvalid, path, fmt.Sprintf("unknown field %q", k))
	}
}

// asMap decodes raw into a generic JSON object map. Returns false for
// non-objects or malformed JSON.
func asMap(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, false
	}
	return m, true
}

// asSlice decodes raw into a generic JSON array. Returns false for non-arrays.
func asSlice(raw json.RawMessage) ([]json.RawMessage, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var s []json.RawMessage
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, false
	}
	return s, true
}

// keyExists is a presence check that reads clearer than the two-value map
// access at call sites.
func keyExists(m map[string]json.RawMessage, k string) bool {
	_, ok := m[k]
	return ok
}

// set builds a set (map[string]bool) from the given keys for membership tests.
func set(keys ...string) map[string]bool {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m
}
