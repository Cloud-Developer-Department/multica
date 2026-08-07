package resourcetmpl

import (
	"encoding/json"
	"reflect"
	"testing"
)

// --- Happy paths --------------------------------------------------------

func TestValidate_ValidAgent(t *testing.T) {
	r := Validate(marshal(t, validAgentMap()))
	if r.HasErrors() {
		t.Fatalf("valid agent template must have no errors, got: %v", r.Errors)
	}
}

func TestValidate_ValidSquadEmbedded(t *testing.T) {
	r := Validate(marshal(t, validSquadMap()))
	if r.HasErrors() {
		t.Fatalf("valid embedded squad must have no errors, got: %v", r.Errors)
	}
}

func TestValidate_ValidSquadReferences(t *testing.T) {
	m := validSquadMap()
	squad := m["spec"].(map[string]any)["squad"].(map[string]any)
	squad["members_mode"] = "references"
	squad["members"] = []any{
		map[string]any{"ref": "architect", "role": "leader"},
		map[string]any{"ref": "dev", "role": "core-dev"},
	}
	r := Validate(marshal(t, m))
	if r.HasErrors() {
		t.Fatalf("valid references squad must have no errors, got: %v", r.Errors)
	}
}

// --- Round trip (struct -> JSON -> struct, and Validate on the JSON) -----

func TestRoundTrip_Agent(t *testing.T) {
	tmpl := Template{
		SchemaVersion: SchemaVersion,
		TemplateID:    "11111111-1111-1111-1111-111111111111",
		Kind:          KindAgent,
		Metadata: Metadata{
			Name:       "Architect",
			Version:    "1.0.0",
			Visibility: VisibilityWorkspace,
			Author:     Author{ID: "22222222-2222-2222-2222-222222222222", DisplayName: "Xu Yi"},
		},
		Spec: Spec{Agent: &AgentSpec{
			Name:               "architect-agent",
			Model:              "",
			MaxConcurrentTasks: 6,
			PermissionMode:     PermissionPrivate,
			Skills:             []SkillRef{{Name: "s", SourceURL: "https://github.com/x/y", Enabled: true}},
			CustomEnvKeys:      []CustomEnvKey{{Key: "API_KEY", Required: true}},
			MCPServers:         []MCPServer{{Name: "fs", Transport: "stdio"}},
		}},
	}
	raw, err := json.Marshal(tmpl)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if r := Validate(raw); r.HasErrors() {
		t.Fatalf("marshaled template must validate, got: %v", r.Errors)
	}
	var back Template
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(tmpl, back) {
		t.Fatalf("round trip lost data:\n got=%#v\nwant=%#v", back, tmpl)
	}
}

// --- Unknown fields / non-portable fields -------------------------------

func TestValidate_UnknownTopLevelField(t *testing.T) {
	m := validAgentMap()
	m["foobar"] = "x"
	r := Validate(marshal(t, m))
	if !hasCode(r, CodeTemplateInvalid) || !hasPath(r, "foobar") {
		t.Fatalf("expected TEMPLATE_INVALID at top-level 'foobar', got %v", r.Errors)
	}
}

func TestValidate_NonPortableFieldRejected(t *testing.T) {
	// runtime_id is a machine-local field that must never appear in a template.
	// Because it is not a declared AgentSpec field, it surfaces as unknown.
	m := validAgentMap()
	setPath(t, m, "spec.agent.runtime_id", "rt-123")
	r := Validate(marshal(t, m))
	if !hasCode(r, CodeTemplateInvalid) || !hasPath(r, "spec.agent.runtime_id") {
		t.Fatalf("expected non-portable runtime_id rejected, got %v", r.Errors)
	}
}

func TestValidate_CustomEnvValueMapRejected(t *testing.T) {
	// The live custom_env value map is non-portable; only custom_env_keys may
	// appear. Its presence is rejected as an unknown field.
	m := validAgentMap()
	setPath(t, m, "spec.agent.custom_env", map[string]any{"API_KEY": "sk-leaked"})
	r := Validate(marshal(t, m))
	if !hasPath(r, "spec.agent.custom_env") {
		t.Fatalf("expected custom_env rejected as non-portable, got %v", r.Errors)
	}
}

func TestValidate_UnknownMetadataField(t *testing.T) {
	m := validAgentMap()
	setPath(t, m, "metadata.owner_id", "u")
	r := Validate(marshal(t, m))
	if !hasPath(r, "metadata.owner_id") {
		t.Fatalf("expected metadata.owner_id rejected, got %v", r.Errors)
	}
}

// metadata.readme carries the companion README.md filename (PMO UX ruling 2:
// JSON has no comments, so prose lives in a sibling file). It is a known
// optional field and must not be reported as unknown.
func TestValidate_ReadmeAccepted(t *testing.T) {
	m := validAgentMap()
	setPath(t, m, "metadata.readme", "README.md")
	r := Validate(marshal(t, m))
	if r.HasErrors() {
		t.Fatalf("metadata.readme should be accepted, got %v", r.Errors)
	}
	if hasPath(r, "metadata.readme") {
		t.Fatalf("metadata.readme should not be flagged, got %v", r.Warnings)
	}
}

func TestValidate_UnknownNestedAgentField(t *testing.T) {
	m := validAgentMap()
	setPath(t, m, "spec.agent.avatar_url", "https://x.png")
	r := Validate(marshal(t, m))
	if !hasPath(r, "spec.agent.avatar_url") {
		t.Fatalf("expected avatar_url rejected, got %v", r.Errors)
	}
}

// --- kind / spec consistency -------------------------------------------

func TestValidate_KindSpecMismatch(t *testing.T) {
	m := validAgentMap() // kind=agent, spec.agent present
	// Replace spec payload with squad while keeping kind=agent.
	m["spec"] = map[string]any{"squad": map[string]any{
		"name": "x", "members_mode": "references", "leader_ref": "a",
		"members": []any{map[string]any{"ref": "a", "role": "leader"}},
	}}
	r := Validate(marshal(t, m))
	if !hasPath(r, "spec") {
		t.Fatalf("expected kind/spec mismatch error at spec, got %v", r.Errors)
	}
}

func TestValidate_KindEnumInvalid(t *testing.T) {
	m := validAgentMap()
	m["kind"] = "team"
	r := Validate(marshal(t, m))
	if !hasPath(r, "kind") {
		t.Fatalf("expected kind enum error, got %v", r.Errors)
	}
}

func TestValidate_SpecMissing(t *testing.T) {
	m := validAgentMap()
	delete(m, "spec")
	r := Validate(marshal(t, m))
	if !hasPath(r, "spec") {
		t.Fatalf("expected spec required error, got %v", r.Errors)
	}
}

// --- version ------------------------------------------------------------

func TestValidate_SchemaVersionUnsupported(t *testing.T) {
	m := validAgentMap()
	m["schema_version"] = "2.0"
	r := Validate(marshal(t, m))
	if !hasCode(r, CodeTemplateVersionUnsupported) {
		t.Fatalf("expected TEMPLATE_VERSION_UNSUPPORTED, got %v", r.Errors)
	}
}

func TestValidate_SchemaVersionMissing(t *testing.T) {
	m := validAgentMap()
	delete(m, "schema_version")
	r := Validate(marshal(t, m))
	if !hasPath(r, "schema_version") {
		t.Fatalf("expected schema_version required error, got %v", r.Errors)
	}
}

// --- metadata enum / semver --------------------------------------------

func TestValidate_InvalidSemVer(t *testing.T) {
	m := validAgentMap()
	setPath(t, m, "metadata.version", "1.0")
	r := Validate(marshal(t, m))
	if !hasPath(r, "metadata.version") {
		t.Fatalf("expected invalid SemVer error, got %v", r.Errors)
	}
}

func TestValidate_VisibilityNotWorkspace(t *testing.T) {
	m := validAgentMap()
	setPath(t, m, "metadata.visibility", "public")
	r := Validate(marshal(t, m))
	if !hasPath(r, "metadata.visibility") {
		t.Fatalf("expected visibility error, got %v", r.Errors)
	}
}

// --- squad referential integrity ---------------------------------------

func TestValidate_LeaderRefNoMember(t *testing.T) {
	m := validSquadMap()
	m["spec"].(map[string]any)["squad"].(map[string]any)["leader_ref"] = "ghost"
	r := Validate(marshal(t, m))
	if !hasPath(r, "spec.squad.leader_ref") {
		t.Fatalf("expected leader_ref resolution error, got %v", r.Errors)
	}
}

func TestValidate_LeaderRefRoleNotLeader(t *testing.T) {
	m := validSquadMap()
	squad := m["spec"].(map[string]any)["squad"].(map[string]any)
	// Point leader_ref at the 'dev' member, whose role is core-dev.
	squad["leader_ref"] = "dev"
	r := Validate(marshal(t, m))
	if !hasPath(r, "spec.squad.leader_ref") {
		t.Fatalf("expected leader role!=leader error, got %v", r.Errors)
	}
}

func TestValidate_DuplicateMemberRef(t *testing.T) {
	m := validSquadMap()
	squad := m["spec"].(map[string]any)["squad"].(map[string]any)
	squad["members"] = []any{
		map[string]any{"ref": "dup", "role": "leader", "agent": agentSpecMap("a")},
		map[string]any{"ref": "dup", "role": "core-dev", "agent": agentSpecMap("b")},
	}
	squad["leader_ref"] = "dup"
	r := Validate(marshal(t, m))
	if !hasPath(r, "spec.squad.members[1].ref") {
		t.Fatalf("expected duplicate ref error, got %v", r.Errors)
	}
}

func TestValidate_EmbeddedMissingAgent(t *testing.T) {
	m := validSquadMap()
	squad := m["spec"].(map[string]any)["squad"].(map[string]any)
	squad["members"] = []any{
		map[string]any{"ref": "architect", "role": "leader"}, // no agent
	}
	r := Validate(marshal(t, m))
	if !hasPath(r, "spec.squad.members[0].agent") {
		t.Fatalf("expected embedded-missing-agent error, got %v", r.Errors)
	}
}

func TestValidate_ReferencesExtraAgent(t *testing.T) {
	m := validSquadMap()
	squad := m["spec"].(map[string]any)["squad"].(map[string]any)
	squad["members_mode"] = "references"
	// references mode but member carries an embedded agent -> error.
	r := Validate(marshal(t, m))
	if !hasPath(r, "spec.squad.members[0].agent") {
		t.Fatalf("expected references-extra-agent error, got %v", r.Errors)
	}
}

func TestValidate_MembersModeInvalid(t *testing.T) {
	m := validSquadMap()
	m["spec"].(map[string]any)["squad"].(map[string]any)["members_mode"] = "hybrid"
	r := Validate(marshal(t, m))
	if !hasPath(r, "spec.squad.members_mode") {
		t.Fatalf("expected members_mode error, got %v", r.Errors)
	}
}

// --- secrets ------------------------------------------------------------

func TestValidate_PlaintextSecretRejected(t *testing.T) {
	m := validAgentMap()
	// Plant a hardcoded token in an MCP config_skeleton.
	setPath(t, m, "spec.agent.mcp_servers", []any{
		map[string]any{
			"name": "fs", "transport": "stdio", "requires_auth": true,
			"config_skeleton": map[string]any{"headers": map[string]any{
				"Authorization": "Bearer abc.def.ghi",
			}},
		},
	})
	r := Validate(marshal(t, m))
	if !hasCode(r, CodeSecretDetected) {
		t.Fatalf("expected SECRET_DETECTED, got %v", r.Errors)
	}
}

// --- multi-error collection --------------------------------------------

func TestValidate_MultipleErrorsCollected(t *testing.T) {
	m := validAgentMap()
	// Stack several independent problems.
	m["schema_version"] = "3.0"                     // version unsupported
	setPath(t, m, "metadata.version", "not-semver") // bad semver
	setPath(t, m, "metadata.visibility", "public")  // bad visibility
	m["kind"] = "team"                              // bad kind enum
	m["ghost"] = "x"                                // unknown top-level

	r := Validate(marshal(t, m))
	// Expect at least one error per problem category, all in a single report.
	want := []ErrorCode{
		CodeTemplateVersionUnsupported, // schema_version 3.0
		CodeTemplateInvalid,            // covers version(semver), visibility, kind, ghost
	}
	for _, c := range want {
		if !hasCode(r, c) {
			t.Errorf("expected error code %s in collected report, got %v", c, r.Errors)
		}
	}
	// And the specific paths must all be present (not fail-fast on the first).
	for _, p := range []string{"metadata.version", "metadata.visibility", "kind", "ghost"} {
		if !hasPath(r, p) {
			t.Errorf("expected collected error at %s, got %v", p, r.Errors)
		}
	}
}

// --- malformed JSON -----------------------------------------------------

func TestValidate_MalformedJSON(t *testing.T) {
	r := Validate([]byte(`{not json`))
	if !hasCode(r, CodeTemplateInvalid) || !r.HasErrors() {
		t.Fatalf("expected single TEMPLATE_INVALID for malformed JSON, got %v", r.Errors)
	}
}

// --- warning path -------------------------------------------------------

func TestValidate_PublicToNoScopeWarning(t *testing.T) {
	m := validAgentMap()
	setPath(t, m, "spec.agent.permission_mode", PermissionPublicTo)
	setPath(t, m, "spec.agent.public_to_workspace", false)
	r := Validate(marshal(t, m))
	if r.HasErrors() {
		t.Fatalf("public_to-without-scope must be valid, got errors: %v", r.Errors)
	}
	if !hasWarning(r, WarningPublicToNoScope) {
		t.Fatalf("expected PUBLIC_TO_NO_SCOPE warning, got %v", r.Warnings)
	}
}
