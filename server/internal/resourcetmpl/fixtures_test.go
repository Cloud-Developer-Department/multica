package resourcetmpl

import (
	"encoding/json"
	"strings"
	"testing"
)

// metaMap returns a valid metadata sub-document shared by every fixture.
func metaMap() map[string]any {
	return map[string]any{
		"name":        "Architect",
		"description": "architect agent",
		"author": map[string]any{
			"id":           "22222222-2222-2222-2222-222222222222",
			"display_name": "Xu Yi",
		},
		"version":          "1.0.0",
		"visibility":       "workspace",
		"tags":             []any{"eng"},
		"source_workspace": "33333333-3333-3333-3333-333333333333",
		"created_at":       "2026-08-05T14:00:00Z",
	}
}

// agentSpecMap returns a valid AgentSpec sub-document.
func agentSpecMap(name string) map[string]any {
	return map[string]any{
		"name":                 name,
		"description":          "does architecture",
		"instructions":         "design systems",
		"model":                "",
		"max_concurrent_tasks": 6,
		"permission_mode":      "private",
		"custom_args":          []any{"--foo"},
		"skills": []any{
			map[string]any{"name": "multica-squads", "source_url": "https://github.com/x/y", "enabled": true},
		},
		"custom_env_keys": []any{
			map[string]any{"key": "API_KEY", "required": true, "description": "provider key"},
		},
		"mcp_servers": []any{
			map[string]any{
				"name":            "fs",
				"transport":       "stdio",
				"requires_auth":   false,
				"config_skeleton": map[string]any{"command": "npx", "args": []any{"-y", "fs-mcp"}},
			},
		},
	}
}

// validAgentMap returns a complete, valid agent template as a generic map so
// tests can mutate one field and re-marshal without touching the rest.
func validAgentMap() map[string]any {
	return map[string]any{
		"schema_version": "1.0",
		"template_id":    "11111111-1111-1111-1111-111111111111",
		"kind":           "agent",
		"metadata":       metaMap(),
		"spec":           map[string]any{"agent": agentSpecMap("architect-agent")},
	}
}

// validSquadMap returns a complete, valid embedded squad template.
func validSquadMap() map[string]any {
	return map[string]any{
		"schema_version": "1.0",
		"kind":           "squad",
		"metadata":       metaMap(),
		"spec": map[string]any{"squad": map[string]any{
			"name":         "core-team",
			"description":  "core engineering team",
			"members_mode": "embedded",
			"leader_ref":   "architect",
			"members": []any{
				map[string]any{"ref": "architect", "role": "leader", "agent": agentSpecMap("architect-agent")},
				map[string]any{"ref": "dev", "role": "core-dev", "agent": agentSpecMap("dev-agent")},
			},
		}},
	}
}

// marshal encodes v to JSON, failing the test on error.
func marshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// setPath sets val at a dotted path of nested map[string]any (no array
// indices). Pass nil for val to delete the leaf key. Intermediate segments
// must already be objects. Used to perturb a single field in a fixture.
func setPath(t *testing.T, root map[string]any, path string, val any) {
	t.Helper()
	parts := strings.Split(path, ".")
	cur := root
	for _, p := range parts[:len(parts)-1] {
		next, ok := cur[p].(map[string]any)
		if !ok {
			t.Fatalf("setPath %q: segment %q is not an object", path, p)
		}
		cur = next
	}
	last := parts[len(parts)-1]
	if val == nil {
		delete(cur, last)
		return
	}
	cur[last] = val
}

// hasCode reports whether the report contains an error with the given code.
func hasCode(r *Report, c ErrorCode) bool {
	for _, e := range r.Errors {
		if e.Code == c {
			return true
		}
	}
	return false
}

// hasPath reports whether the report contains an error whose path matches.
func hasPath(r *Report, p string) bool {
	for _, e := range r.Errors {
		if e.Path == p {
			return true
		}
	}
	return false
}

// hasWarning reports whether the report contains a warning with the given code.
func hasWarning(r *Report, code string) bool {
	for _, w := range r.Warnings {
		if w.Code == code {
			return true
		}
	}
	return false
}
