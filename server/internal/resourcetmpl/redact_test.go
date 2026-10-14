package resourcetmpl

import (
	"strings"
	"testing"
)

func TestDetectSecrets_KeyNameMatch(t *testing.T) {
	// A credential-named key with any string value is flagged, even a benign
	// one — the location itself is sensitive.
	raw := marshal(t, map[string]any{"spec": map[string]any{"agent": map[string]any{
		"mcp_servers": []any{
			map[string]any{"config_skeleton": map[string]any{"api_key": "redacted"}},
		},
	}}})
	errs := DetectSecrets(raw)
	if !hasCode(&Report{Errors: errs}, CodeSecretDetected) {
		t.Fatalf("expected SECRET_DETECTED for api_key, got %v", errs)
	}
	wantPath := "spec.agent.mcp_servers[0].config_skeleton.api_key"
	if !pathIn(errs, wantPath) {
		t.Errorf("expected path %s, got %v", wantPath, errs)
	}
}

func pathIn(errs []Error, p string) bool {
	for _, e := range errs {
		if e.Path == p {
			return true
		}
	}
	return false
}

func TestDetectSecrets_ValueShapes(t *testing.T) {
	// Each value shape is credential-like on its own, under a benign key.
	values := []string{
		"sk-" + strings.Repeat("a", 20),
		"ghp_" + strings.Repeat("b", 20),
		"github_pat_" + strings.Repeat("c", 20),
		"AKIA" + "ABCDEFGHIJKLMNOP",
		"xoxb-" + strings.Repeat("d", 12),
		"Bearer abc123",
		"Basic " + strings.Repeat("Z", 12) + "==",
		"-----BEGIN PRIVATE KEY-----\nMIIE...",
		"postgres://multica:supersecret@localhost:5432/db",
	}
	for _, v := range values {
		raw := marshal(t, map[string]any{"spec": map[string]any{"agent": map[string]any{"instructions": v}}})
		errs := DetectSecrets(raw)
		if !hasCode(&Report{Errors: errs}, CodeSecretDetected) {
			t.Errorf("value %q: expected SECRET_DETECTED, got %v", v, errs)
		}
	}
}

func TestDetectSecrets_NoFalsePositives(t *testing.T) {
	// Benign content that mentions credential terms but is not itself a
	// credential must not be flagged.
	benign := map[string]any{
		"spec": map[string]any{"agent": map[string]any{
			"name":         "github-token-bot",
			"description":  "manages tokens", // 'token' as a word in prose
			"instructions": "Call the API with an Authorization header that the runtime injects.",
			"skills": []any{
				map[string]any{"name": "auth-helper", "source_url": "https://github.com/org/repo"},
			},
		}},
	}
	errs := DetectSecrets(marshal(t, benign))
	if len(errs) != 0 {
		t.Errorf("expected no secrets for benign content, got %v", errs)
	}
}

func TestDetectSecrets_ConfigSkeletonHeader(t *testing.T) {
	// A hardcoded Authorization header inside an MCP config_skeleton is the
	// canonical "secret slipped past the downgrade" case.
	raw := marshal(t, map[string]any{"spec": map[string]any{"agent": map[string]any{
		"mcp_servers": []any{
			map[string]any{"config_skeleton": map[string]any{
				"headers": map[string]any{"Authorization": "Bearer abc.def.ghi"},
			}},
		},
	}}})
	errs := DetectSecrets(raw)
	if !hasCode(&Report{Errors: errs}, CodeSecretDetected) {
		t.Fatalf("expected SECRET_DETECTED for Authorization header, got %v", errs)
	}
}

func TestDetectSecrets_CustomEnvKeyNameNotFlagged(t *testing.T) {
	// custom_env_keys legitimately stores key NAMES like "API_KEY"; the value
	// under field "key" is a name, not a credential. Must not false-positive.
	errs := DetectSecrets(marshal(t, validAgentMap()))
	if len(errs) != 0 {
		t.Errorf("valid template with custom_env_keys must not trigger secrets, got %v", errs)
	}
}

func TestDetectSecrets_MalformedJSON(t *testing.T) {
	if errs := DetectSecrets([]byte(`{not json`)); errs != nil {
		t.Errorf("expected nil for malformed JSON, got %v", errs)
	}
}
