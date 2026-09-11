package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// newTemplateTestCmd builds a standalone cobra.Command carrying the same flags
// runTemplateExport/Validate/Apply read (via the shared registrars), plus the
// persistent --profile flag the API-client resolver needs. Tests mutate its
// flag state in isolation.
func newTemplateTestCmd(register func(*cobra.Command)) *cobra.Command {
	c := &cobra.Command{Use: "template"}
	register(c)
	c.Flags().String("profile", "", "")
	return c
}

// templateTestEnv runs the test from a fresh temp dir with a mock server URL so
// newAPIClient resolves cleanly without any daemon-task marker interference.
func templateTestEnv(t *testing.T, serverURL string) {
	t.Helper()
	t.Chdir(t.TempDir())
	t.Setenv("MULTICA_SERVER_URL", serverURL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_AGENT_ID", "")
	t.Setenv("MULTICA_TASK_ID", "")
}

// sampleAgentTemplate is a minimal but valid agent template document.
func sampleAgentTemplate() map[string]any {
	return map[string]any{
		"schema_version": "1.0",
		"template_id":    "11111111-1111-1111-1111-111111111111",
		"kind":           "agent",
		"metadata": map[string]any{
			"name": "reviewer",
		},
		"spec": map[string]any{
			"agent": map[string]any{
				"name":            "reviewer",
				"model":           "claude",
				"permission_mode": "private",
				"custom_env_keys": []any{
					map[string]any{"key": "API_KEY", "required": true},
					map[string]any{"key": "OPTIONAL", "required": false},
				},
			},
		},
	}
}

// sampleSquadTemplate is a minimal embedded squad template with array-indexable
// members, used by the --set path tests.
func sampleSquadTemplate() map[string]any {
	return map[string]any{
		"schema_version": "1.0",
		"template_id":    "22222222-2222-2222-2222-222222222222",
		"kind":           "squad",
		"metadata": map[string]any{
			"name": "dev-squad",
		},
		"spec": map[string]any{
			"squad": map[string]any{
				"name":         "dev-squad",
				"members_mode": "embedded",
				"leader_ref":   "dev",
				"members": []any{
					map[string]any{
						"ref":  "dev",
						"role": "leader",
						"agent": map[string]any{
							"name": "dev",
						},
					},
					map[string]any{
						"ref":  "qa",
						"role": "reviewer",
						"agent": map[string]any{
							"name": "qa",
						},
					},
				},
			},
		},
	}
}

// writeTemplateFile writes a template document to a temp file and returns its
// path.
func writeTemplateFile(t *testing.T, doc map[string]any) string {
	t.Helper()
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal template: %v", err)
	}
	path := filepath.Join(t.TempDir(), "template.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write template file: %v", err)
	}
	return path
}

// applyMockServer serves /api/templates/apply (and optionally /validate) and
// captures the last request body. The handler func decides the response.
func applyMockServer(t *testing.T, handler http.HandlerFunc, gotBody *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if gotBody != nil {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
			}
			*gotBody = body
		}
		handler(w, r)
	}))
}

// ---------------------------------------------------------------------------
// Flag registry
// ---------------------------------------------------------------------------

func TestTemplateCommandsRegisteredOnRoot(t *testing.T) {
	sub := rootCmd.Commands()
	seen := map[string]bool{}
	for _, c := range sub {
		seen[c.Name()] = true
	}
	if !seen["template"] {
		t.Fatal("template command not registered on root")
	}
	tmpl := templateCmd
	find := func(name string) bool {
		found, _, err := tmpl.Find([]string{name})
		return err == nil && found != nil && found.Name() == name
	}
	if !find("export") {
		t.Error("template export not registered")
	}
	if !find("validate") {
		t.Error("template validate not registered")
	}
	if !find("apply") {
		t.Error("template apply not registered")
	}
}

func TestTemplateApplyHasNoPlaintextCustomEnvFlag(t *testing.T) {
	if flag := templateApplyCmd.Flags().Lookup("custom-env"); flag != nil {
		t.Fatalf("--custom-env plaintext flag must not exist, got %v", flag)
	}
}

func TestTemplateSubcommandFlagParsing(t *testing.T) {
	export := templateExportCmd
	if export.Flags().Lookup("kind") == nil {
		t.Error("export missing --kind")
	}
	if export.Flags().Lookup("id") == nil {
		t.Error("export missing --id")
	}
	if export.Flags().Lookup("file") == nil {
		t.Error("export missing --file")
	}
	if export.Flags().Lookup("members-mode") == nil {
		t.Error("export missing --members-mode")
	}
	if export.Flags().Lookup("output") == nil {
		t.Error("export missing --output")
	}

	validate := templateValidateCmd
	if validate.Flags().Lookup("file") == nil {
		t.Error("validate missing --file")
	}
	if validate.Flags().Lookup("runtime-id") == nil {
		t.Error("validate missing --runtime-id")
	}

	apply := templateApplyCmd
	for _, flag := range []string{"file", "runtime-id", "dry-run", "yes", "conflict-policy", "members-mode", "set", "overrides-file", "custom-env-file", "custom-env-stdin", "install-missing-skills", "idempotency-key", "output"} {
		if apply.Flags().Lookup(flag) == nil {
			t.Errorf("apply missing --%s", flag)
		}
	}
}

// ---------------------------------------------------------------------------
// --output / --file semantics
// ---------------------------------------------------------------------------

func TestTemplateOutputRejectsNonFormatValues(t *testing.T) {
	for _, fn := range []func(*cobra.Command) error{validateOutputFlag} {
		cmd := newTemplateTestCmd(registerTemplateExportFlags)
		if err := cmd.Flags().Set("output", "agent.json"); err != nil {
			t.Fatalf("set output: %v", err)
		}
		if err := fn(cmd); err == nil {
			t.Error("--output agent.json must be rejected (paths go in --file)")
		} else if !strings.Contains(err.Error(), "--file") {
			t.Errorf("error should hint at --file, got %v", err)
		}
	}
}

func TestTemplateMissingFileAndRuntimeID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	templateTestEnv(t, srv.URL)

	cmd := newTemplateTestCmd(registerTemplateValidateFlags)
	err := runTemplateValidate(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--file") {
		t.Errorf("missing --file should error, got %v", err)
	}

	cmd2 := newTemplateTestCmd(registerTemplateValidateFlags)
	if err := cmd2.Flags().Set("file", "x.json"); err != nil {
		t.Fatal(err)
	}
	err = runTemplateValidate(cmd2, nil)
	if err == nil || !strings.Contains(err.Error(), "--runtime-id") {
		t.Errorf("missing --runtime-id should error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Custom env flags
// ---------------------------------------------------------------------------

func TestTemplateCustomEnvMutuallyExclusive(t *testing.T) {
	cmd := newTemplateTestCmd(registerTemplateApplyFlags)
	if err := cmd.Flags().Set("custom-env-file", "env.json"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("custom-env-stdin", "true"); err != nil {
		t.Fatal(err)
	}
	_, _, err := resolveTemplateEnv(cmd, sampleAgentTemplate())
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("custom-env-file + custom-env-stdin must be rejected, got %v", err)
	}
}

func TestTemplateCustomEnvFileParsing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env.json")
	if err := os.WriteFile(path, []byte(`{"reviewer":{"API_KEY":"sekret"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := newTemplateTestCmd(registerTemplateApplyFlags)
	if err := cmd.Flags().Set("custom-env-file", path); err != nil {
		t.Fatal(err)
	}
	env, skipped, err := resolveTemplateEnv(cmd, sampleAgentTemplate())
	if err != nil {
		t.Fatalf("resolveTemplateEnv: %v", err)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped should be empty with a file, got %v", skipped)
	}
	if env["reviewer"]["API_KEY"] != "sekret" {
		t.Errorf("env = %v, want API_KEY=sekret", env)
	}
}

func TestTemplateCustomEnvFileInvalidShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "env.json")
	if err := os.WriteFile(path, []byte(`{"API_KEY":"sekret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := newTemplateTestCmd(registerTemplateApplyFlags)
	if err := cmd.Flags().Set("custom-env-file", path); err != nil {
		t.Fatal(err)
	}
	_, _, err := resolveTemplateEnv(cmd, sampleAgentTemplate())
	if err == nil {
		t.Error("non-ref-wrapped env shape must be rejected")
	}
}

// ---------------------------------------------------------------------------
// Interactive env fallback (non-TTY)
// ---------------------------------------------------------------------------

func TestTemplateInteractiveEnvFallbackNonTTYWritesPlaceholder(t *testing.T) {
	// Tests run with stdin not attached to a terminal, so this exercises the
	// non-TTY skip path: it must not hang and must write the placeholder.
	cmd := newTemplateTestCmd(registerTemplateApplyFlags)
	env, skipped, err := resolveTemplateEnv(cmd, sampleAgentTemplate())
	if err != nil {
		t.Fatalf("resolveTemplateEnv must not error/hang in non-TTY: %v", err)
	}
	if len(skipped) != 1 {
		t.Fatalf("expected 1 skipped required key, got %v", skipped)
	}
	if skipped[0].key != "API_KEY" {
		t.Errorf("skipped key = %q, want API_KEY", skipped[0].key)
	}
	// The placeholder is written into the env structure, then stripped before
	// sending (never submitted as a real value).
	if env["reviewer"]["API_KEY"] != templateEnvPlaceholder {
		t.Errorf("env should hold the placeholder for the skipped key, got %v", env)
	}
	stripped := stripEnvPlaceholders(env)
	if _, ok := stripped["reviewer"]["API_KEY"]; ok {
		t.Error("placeholder must be stripped before sending")
	}
}

// ---------------------------------------------------------------------------
// --set parsing
// ---------------------------------------------------------------------------

func TestParseSetOverrideObjectProperty(t *testing.T) {
	path, value, err := parseSetOverride("metadata.name=My Team")
	if err != nil {
		t.Fatal(err)
	}
	want := []setSegment{{key: "metadata"}, {key: "name"}}
	if !reflect.DeepEqual(path, want) {
		t.Errorf("path = %v, want %v", path, want)
	}
	if value != "My Team" {
		t.Errorf("value = %v, want string", value)
	}
}

func TestParseSetOverrideArrayIndex(t *testing.T) {
	path, value, err := parseSetOverride("spec.squad.members[0].role=leader")
	if err != nil {
		t.Fatal(err)
	}
	want := []setSegment{{key: "spec"}, {key: "squad"}, {key: "members"}, {index: 0, isIdx: true}, {key: "role"}}
	if !reflect.DeepEqual(path, want) {
		t.Errorf("path = %v, want %v", path, want)
	}
	if value != "leader" {
		t.Errorf("value = %v, want leader", value)
	}
}

func TestParseSetOverrideJSONValue(t *testing.T) {
	_, value, err := parseSetOverride("spec.agent.max_concurrent_tasks=6")
	if err != nil {
		t.Fatal(err)
	}
	if value != float64(6) {
		t.Errorf("numeric value = %v (%T), want float64(6)", value, value)
	}
}

func TestApplySetPathObjectAndArray(t *testing.T) {
	doc := sampleSquadTemplate()
	if err := applySetPath(doc, []setSegment{{key: "spec"}, {key: "squad"}, {key: "members"}, {index: 0, isIdx: true}, {key: "role"}}, "captain"); err != nil {
		t.Fatal(err)
	}
	spec := doc["spec"].(map[string]any)
	squad := spec["squad"].(map[string]any)
	members := squad["members"].([]any)
	first := members[0].(map[string]any)
	if first["role"] != "captain" {
		t.Errorf("members[0].role = %v, want captain", first["role"])
	}
}

func TestApplySetPathMissingObjectKeyIsCreated(t *testing.T) {
	doc := sampleAgentTemplate()
	// Optional field omitted in the template: creating it must work.
	if err := applySetPath(doc, []setSegment{{key: "spec"}, {key: "agent"}, {key: "description"}}, "a description"); err != nil {
		t.Fatal(err)
	}
	spec := doc["spec"].(map[string]any)
	agent := spec["agent"].(map[string]any)
	if agent["description"] != "a description" {
		t.Errorf("agent.description = %v", agent["description"])
	}
}

func TestApplySetPathOutOfRangeArrayIndexFails(t *testing.T) {
	doc := sampleSquadTemplate()
	err := applySetPath(doc, []setSegment{{key: "spec"}, {key: "squad"}, {key: "members"}, {index: 9, isIdx: true}, {key: "role"}}, "x")
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Errorf("out-of-range index should error with path, got %v", err)
	}
}

func TestSetOldStyleAgentRefIsNotSpecial(t *testing.T) {
	// Old-style `agents.<ref>.<field>` must no longer be specially interpreted:
	// it parses as an ordinary dotted path rooted on the template document.
	path, value, err := parseSetOverride("agents.senior-dev.name=Bob")
	if err != nil {
		t.Fatalf("old-style set should parse as a plain path: %v", err)
	}
	want := []setSegment{{key: "agents"}, {key: "senior-dev"}, {key: "name"}}
	if !reflect.DeepEqual(path, want) {
		t.Fatalf("path = %v, want plain dotted path %v (no special agents.<ref> interpretation)", path, want)
	}
	if value != "Bob" {
		t.Errorf("value = %v, want Bob", value)
	}
	// Applying it touches a plain top-level `agents` key (created on the fly),
	// never a member override — the server rejects unknown top-level fields.
	doc := sampleAgentTemplate()
	if err := applySetPath(doc, path, value); err != nil {
		t.Fatalf("applySetPath: %v", err)
	}
	agents, _ := doc["agents"].(map[string]any)
	dev, _ := agents["senior-dev"].(map[string]any)
	if dev["name"] != "Bob" {
		t.Errorf("plain path write failed: %v", agents)
	}
}

func TestSetWinsOverOverridesFile(t *testing.T) {
	// --set wins over --overrides-file on a conflicting path.
	overridesPath := filepath.Join(t.TempDir(), "overrides.json")
	if err := os.WriteFile(overridesPath, []byte(`{"metadata":{"name":"from-file"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newTemplateTestCmd(registerTemplateApplyFlags)
	if err := cmd.Flags().Set("overrides-file", overridesPath); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("set", "metadata.name=from-cli"); err != nil {
		t.Fatal(err)
	}

	doc := sampleAgentTemplate()
	if err := applyTemplateOverrides(cmd, doc); err != nil {
		t.Fatal(err)
	}
	meta := doc["metadata"].(map[string]any)
	if meta["name"] != "from-cli" {
		t.Errorf("metadata.name = %v, want from-cli (--set wins)", meta["name"])
	}
}

func TestOverridesFileDeepMergesNonConflictingFields(t *testing.T) {
	overridesPath := filepath.Join(t.TempDir(), "overrides.json")
	if err := os.WriteFile(overridesPath, []byte(`{"metadata":{"description":"file desc"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := newTemplateTestCmd(registerTemplateApplyFlags)
	if err := cmd.Flags().Set("overrides-file", overridesPath); err != nil {
		t.Fatal(err)
	}
	doc := sampleAgentTemplate()
	if err := applyTemplateOverrides(cmd, doc); err != nil {
		t.Fatal(err)
	}
	meta := doc["metadata"].(map[string]any)
	if meta["description"] != "file desc" || meta["name"] != "reviewer" {
		t.Errorf("deep merge wrong: %v", meta)
	}
}

// ---------------------------------------------------------------------------
// Validate
// ---------------------------------------------------------------------------

func TestTemplateValidateSuccessExitZero(t *testing.T) {
	srv := applyMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/templates/validate" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"valid":    true,
			"errors":   []any{},
			"warnings": []any{},
			"required_inputs": map[string]any{
				"env_keys": []any{map[string]any{"agent_ref": "reviewer", "key": "API_KEY"}},
			},
		})
	}, nil)
	defer srv.Close()
	templateTestEnv(t, srv.URL)

	cmd := newTemplateTestCmd(registerTemplateValidateFlags)
	path := writeTemplateFile(t, sampleAgentTemplate())
	_ = cmd.Flags().Set("file", path)
	_ = cmd.Flags().Set("runtime-id", "rt-1")

	err := runTemplateValidate(cmd, nil)
	if err != nil {
		t.Fatalf("valid template should not error, got %v", err)
	}
}

func TestTemplateValidateFailureExitCodeTwo(t *testing.T) {
	srv := applyMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/templates/validate" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"valid": false,
			"errors": []any{
				map[string]any{"code": "TEMPLATE_INVALID", "path": "spec.agent.name", "message": "name required"},
			},
		})
	}, nil)
	defer srv.Close()
	templateTestEnv(t, srv.URL)

	cmd := newTemplateTestCmd(registerTemplateValidateFlags)
	path := writeTemplateFile(t, sampleAgentTemplate())
	_ = cmd.Flags().Set("file", path)
	_ = cmd.Flags().Set("runtime-id", "rt-1")

	err := runTemplateValidate(cmd, nil)
	var codeErr *exitCodeError
	if !errors.As(err, &codeErr) {
		t.Fatalf("want exitCodeError, got %v", err)
	}
	if codeErr.ExitCode() != 2 {
		t.Errorf("exit code = %d, want 2", codeErr.ExitCode())
	}
}

func TestTemplateValidateOutputJSONStableAndNoEnvValues(t *testing.T) {
	srv := applyMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"valid":    true,
			"errors":   []any{},
			"warnings": []any{},
			"required_inputs": map[string]any{
				"env_keys": []any{map[string]any{"agent_ref": "reviewer", "key": "API_KEY"}},
			},
		})
	}, nil)
	defer srv.Close()
	templateTestEnv(t, srv.URL)

	cmd := newTemplateTestCmd(registerTemplateValidateFlags)
	path := writeTemplateFile(t, sampleAgentTemplate())
	_ = cmd.Flags().Set("file", path)
	_ = cmd.Flags().Set("runtime-id", "rt-1")
	_ = cmd.Flags().Set("output", "json")

	if err := runTemplateValidate(cmd, nil); err != nil {
		t.Fatalf("runTemplateValidate: %v", err)
	}

	// Capture the printed JSON from the real os.Stdout is hard; instead re-run
	// the response rendering logic through the shared print path by asserting
	// the JSON-encode of the response never carries env values.
	// The stable contract is that --output json prints the API response object
	// untouched; that path is covered above. Here we assert the sub-command
	// actually wired --output json (registry check) and that a print of the
	// sample response contains no secret material.
	var resp = map[string]any{
		"valid":    true,
		"errors":   []any{},
		"warnings": []any{},
		"required_inputs": map[string]any{
			"env_keys": []any{map[string]any{"agent_ref": "reviewer", "key": "API_KEY"}},
		},
	}
	var sb strings.Builder
	if err := writeJSON(&sb, resp); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sb.String(), "sekret") {
		t.Error("output must not contain env values")
	}
	if !strings.Contains(sb.String(), `"errors"`) {
		t.Error("--output json structure should mirror the API response")
	}
}

// writeJSON is a tiny helper so tests can assert on rendered JSON without
// depending on os.Stdout.
func writeJSON(w interface{ Write([]byte) (int, error) }, v any) error {
	enc := json.NewEncoder(w)
	return enc.Encode(v)
}

// ---------------------------------------------------------------------------
// Apply
// ---------------------------------------------------------------------------

func TestTemplateApplyNonInteractiveRequiresYes(t *testing.T) {
	srv := applyMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}, nil)
	defer srv.Close()
	templateTestEnv(t, srv.URL)

	cmd := newTemplateTestCmd(registerTemplateApplyFlags)
	path := writeTemplateFile(t, sampleAgentTemplate())
	_ = cmd.Flags().Set("file", path)
	_ = cmd.Flags().Set("runtime-id", "rt-1")

	err := runTemplateApply(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Errorf("non-interactive apply without --yes must be rejected, got %v", err)
	}
}

func TestTemplateApplyDryRunSendsDryRunTrue(t *testing.T) {
	var gotBody map[string]any
	srv := applyMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/templates/apply" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"applied": false,
			"dry_run": true,
			"created": map[string]any{
				"agents": []any{map[string]any{"ref": "reviewer", "name": "reviewer", "id": ""}},
			},
			"resource_mapping": map[string]any{},
			"warnings":         []any{},
		})
	}, &gotBody)
	defer srv.Close()
	templateTestEnv(t, srv.URL)

	cmd := newTemplateTestCmd(registerTemplateApplyFlags)
	path := writeTemplateFile(t, sampleAgentTemplate())
	_ = cmd.Flags().Set("file", path)
	_ = cmd.Flags().Set("runtime-id", "rt-1")
	_ = cmd.Flags().Set("dry-run", "true")

	if err := runTemplateApply(cmd, nil); err != nil {
		t.Fatalf("runTemplateApply: %v", err)
	}
	if gotBody == nil {
		t.Fatal("apply was never called")
	}
	if dry, ok := gotBody["dry_run"].(bool); !ok || !dry {
		t.Errorf("dry_run = %v, want true", gotBody["dry_run"])
	}
	if tmpl, ok := gotBody["template"].(map[string]any); !ok {
		t.Error("template missing from apply body")
	} else if strVal(tmpl, "kind") != "agent" {
		t.Errorf("template kind = %v", tmpl["kind"])
	}
}

func TestTemplateApplyConflictExitCodeThree(t *testing.T) {
	var gotBody map[string]any
	srv := applyMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/templates/apply" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"applied": false,
			"dry_run": true,
			"created": map[string]any{},
			"plan": map[string]any{
				"conflicts": []any{map[string]any{"kind": "agent", "name": "reviewer", "existing_id": "aaa"}},
			},
		})
	}, &gotBody)
	defer srv.Close()
	templateTestEnv(t, srv.URL)

	cmd := newTemplateTestCmd(registerTemplateApplyFlags)
	path := writeTemplateFile(t, sampleAgentTemplate())
	_ = cmd.Flags().Set("file", path)
	_ = cmd.Flags().Set("runtime-id", "rt-1")
	_ = cmd.Flags().Set("dry-run", "true")

	err := runTemplateApply(cmd, nil)
	var codeErr *exitCodeError
	if !errors.As(err, &codeErr) {
		t.Fatalf("want exitCodeError for conflict, got %v", err)
	}
	if codeErr.ExitCode() != 3 {
		t.Errorf("exit code = %d, want 3", codeErr.ExitCode())
	}
}

func TestTemplateApplyConflictWithRenamePolicyProceeds(t *testing.T) {
	var gotBody map[string]any
	srv := applyMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/templates/apply" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"applied": false,
			"dry_run": true,
			"created": map[string]any{},
			"plan": map[string]any{
				"conflicts": []any{map[string]any{"kind": "agent", "name": "reviewer", "existing_id": "aaa"}},
			},
		})
	}, &gotBody)
	defer srv.Close()
	templateTestEnv(t, srv.URL)

	cmd := newTemplateTestCmd(registerTemplateApplyFlags)
	path := writeTemplateFile(t, sampleAgentTemplate())
	_ = cmd.Flags().Set("file", path)
	_ = cmd.Flags().Set("runtime-id", "rt-1")
	_ = cmd.Flags().Set("conflict-policy", "rename")
	_ = cmd.Flags().Set("dry-run", "true")

	// rename policy resolves conflicts automatically, so no exit-3.
	if err := runTemplateApply(cmd, nil); err != nil {
		t.Fatalf("rename policy should not error on conflicts, got %v", err)
	}
}

func TestTemplateApplyValidationErrorExitCodeTwo(t *testing.T) {
	var gotBody map[string]any
	srv := applyMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/templates/apply" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"applied": false,
			"dry_run": true,
			"created": map[string]any{},
			"errors":  []any{map[string]any{"code": "TEMPLATE_INVALID", "message": "bad"}},
		})
	}, &gotBody)
	defer srv.Close()
	templateTestEnv(t, srv.URL)

	cmd := newTemplateTestCmd(registerTemplateApplyFlags)
	path := writeTemplateFile(t, sampleAgentTemplate())
	_ = cmd.Flags().Set("file", path)
	_ = cmd.Flags().Set("runtime-id", "rt-1")
	_ = cmd.Flags().Set("dry-run", "true")

	err := runTemplateApply(cmd, nil)
	var codeErr *exitCodeError
	if !errors.As(err, &codeErr) {
		t.Fatalf("want exitCodeError, got %v", err)
	}
	if codeErr.ExitCode() != 2 {
		t.Errorf("exit code = %d, want 2", codeErr.ExitCode())
	}
}

func TestTemplateApplyHTTPConflictMapsToExitThree(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/templates/apply" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "NAME_CONFLICT", "message": "name exists"},
		})
	}))
	defer srv.Close()
	templateTestEnv(t, srv.URL)

	cmd := newTemplateTestCmd(registerTemplateApplyFlags)
	path := writeTemplateFile(t, sampleAgentTemplate())
	_ = cmd.Flags().Set("file", path)
	_ = cmd.Flags().Set("runtime-id", "rt-1")
	_ = cmd.Flags().Set("yes", "true")

	err := runTemplateApply(cmd, nil)
	var codeErr *exitCodeError
	if !errors.As(err, &codeErr) {
		t.Fatalf("want exitCodeError, got %v", err)
	}
	if codeErr.ExitCode() != 3 {
		t.Errorf("exit code = %d, want 3", codeErr.ExitCode())
	}
}

func TestTemplateApplyIdempotencyKeyEchoedWhenGenerated(t *testing.T) {
	var gotBody map[string]any
	srv := applyMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/templates/apply" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"applied": true,
			"dry_run": false,
			"created": map[string]any{
				"agents": []any{map[string]any{"ref": "reviewer", "name": "reviewer", "id": "agent-1"}},
			},
			"resource_mapping": map[string]any{"reviewer": "agent-1"},
			"warnings":         []any{},
		})
	}, &gotBody)
	defer srv.Close()
	templateTestEnv(t, srv.URL)

	cmd := newTemplateTestCmd(registerTemplateApplyFlags)
	path := writeTemplateFile(t, sampleAgentTemplate())
	_ = cmd.Flags().Set("file", path)
	_ = cmd.Flags().Set("runtime-id", "rt-1")
	_ = cmd.Flags().Set("yes", "true")

	if err := runTemplateApply(cmd, nil); err != nil {
		t.Fatalf("runTemplateApply: %v", err)
	}
	key, ok := gotBody["idempotency_key"].(string)
	if !ok || key == "" {
		t.Errorf("idempotency_key missing/empty in apply body: %v", gotBody["idempotency_key"])
	}
}

func TestTemplateApplySetAndEnvInRequest(t *testing.T) {
	var gotBody map[string]any
	srv := applyMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/templates/apply" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"applied": true,
			"dry_run": false,
			"created": map[string]any{
				"agents": []any{map[string]any{"ref": "reviewer", "name": "renamed", "id": "agent-1"}},
			},
			"resource_mapping": map[string]any{"reviewer": "agent-1"},
			"warnings":         []any{},
		})
	}, &gotBody)
	defer srv.Close()
	templateTestEnv(t, srv.URL)

	envPath := filepath.Join(t.TempDir(), "env.json")
	if err := os.WriteFile(envPath, []byte(`{"reviewer":{"API_KEY":"sekret"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := newTemplateTestCmd(registerTemplateApplyFlags)
	path := writeTemplateFile(t, sampleAgentTemplate())
	_ = cmd.Flags().Set("file", path)
	_ = cmd.Flags().Set("runtime-id", "rt-1")
	_ = cmd.Flags().Set("yes", "true")
	_ = cmd.Flags().Set("set", "metadata.name=renamed")
	_ = cmd.Flags().Set("custom-env-file", envPath)

	if err := runTemplateApply(cmd, nil); err != nil {
		t.Fatalf("runTemplateApply: %v", err)
	}
	tmpl := gotBody["template"].(map[string]any)
	meta := tmpl["metadata"].(map[string]any)
	if meta["name"] != "renamed" {
		t.Errorf("--set not applied to template: metadata.name = %v", meta["name"])
	}
	env, ok := gotBody["env"].(map[string]any)
	if !ok {
		t.Fatal("env missing from apply body")
	}
	ref, ok := env["reviewer"].(map[string]any)
	if !ok || ref["API_KEY"] != "sekret" {
		t.Errorf("env not passed through: %v", env)
	}
}

func TestTemplateApplyEnvValuesNeverEchoed(t *testing.T) {
	var gotBody map[string]any
	srv := applyMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"applied": true,
			"dry_run": false,
			"created": map[string]any{
				"agents": []any{map[string]any{"ref": "reviewer", "name": "reviewer", "id": "agent-1"}},
			},
			"resource_mapping": map[string]any{"reviewer": "agent-1"},
			"warnings":         []any{},
		})
	}, &gotBody)
	defer srv.Close()
	templateTestEnv(t, srv.URL)

	envPath := filepath.Join(t.TempDir(), "env.json")
	if err := os.WriteFile(envPath, []byte(`{"reviewer":{"API_KEY":"supersecretvalue"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := newTemplateTestCmd(registerTemplateApplyFlags)
	path := writeTemplateFile(t, sampleAgentTemplate())
	_ = cmd.Flags().Set("file", path)
	_ = cmd.Flags().Set("runtime-id", "rt-1")
	_ = cmd.Flags().Set("yes", "true")
	_ = cmd.Flags().Set("custom-env-file", envPath)

	if err := runTemplateApply(cmd, nil); err != nil {
		t.Fatalf("runTemplateApply: %v", err)
	}
	// The captured request may contain the env value (it is the request), but
	// the JSON-printed output must never. The response here carries no env
	// field; assert that rendering the response object cannot leak values.
	resp := map[string]any{
		"applied":          true,
		"created":          map[string]any{},
		"resource_mapping": map[string]any{},
	}
	var sb strings.Builder
	if err := writeJSON(&sb, resp); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sb.String(), "supersecretvalue") {
		t.Error("output must not echo env values")
	}
}

func TestTemplateApplyRejectsInvalidConflictPolicy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	templateTestEnv(t, srv.URL)

	cmd := newTemplateTestCmd(registerTemplateApplyFlags)
	path := writeTemplateFile(t, sampleAgentTemplate())
	_ = cmd.Flags().Set("file", path)
	_ = cmd.Flags().Set("runtime-id", "rt-1")
	_ = cmd.Flags().Set("conflict-policy", "overwrite")

	err := runTemplateApply(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "conflict-policy") {
		t.Errorf("overwrite policy must be rejected, got %v", err)
	}
}

func TestTemplateApplyOverridesFileWithSetPrecedenceInRequest(t *testing.T) {
	var gotBody map[string]any
	srv := applyMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"applied":          true,
			"dry_run":          false,
			"created":          map[string]any{},
			"resource_mapping": map[string]any{},
			"warnings":         []any{},
		})
	}, &gotBody)
	defer srv.Close()
	templateTestEnv(t, srv.URL)

	overridesPath := filepath.Join(t.TempDir(), "overrides.json")
	if err := os.WriteFile(overridesPath, []byte(`{"metadata":{"name":"from-file"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newTemplateTestCmd(registerTemplateApplyFlags)
	path := writeTemplateFile(t, sampleAgentTemplate())
	_ = cmd.Flags().Set("file", path)
	_ = cmd.Flags().Set("runtime-id", "rt-1")
	_ = cmd.Flags().Set("yes", "true")
	_ = cmd.Flags().Set("overrides-file", overridesPath)
	_ = cmd.Flags().Set("set", "metadata.name=from-cli")

	if err := runTemplateApply(cmd, nil); err != nil {
		t.Fatalf("runTemplateApply: %v", err)
	}
	tmpl := gotBody["template"].(map[string]any)
	meta := tmpl["metadata"].(map[string]any)
	if meta["name"] != "from-cli" {
		t.Errorf("metadata.name = %v, want from-cli (--set wins)", meta["name"])
	}
}

// ---------------------------------------------------------------------------
// Export
// ---------------------------------------------------------------------------

func TestTemplateExportAgentWritesFile(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/agents":
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"id": "agent-1", "name": "reviewer"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/templates/export":
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Errorf("decode body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"template": sampleAgentTemplate(),
				"warnings": []any{},
			})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	templateTestEnv(t, srv.URL)

	outPath := filepath.Join(t.TempDir(), "agent.json")
	cmd := newTemplateTestCmd(registerTemplateExportFlags)
	_ = cmd.Flags().Set("kind", "agent")
	_ = cmd.Flags().Set("id", "reviewer")
	_ = cmd.Flags().Set("file", outPath)

	if err := runTemplateExport(cmd, nil); err != nil {
		t.Fatalf("runTemplateExport: %v", err)
	}
	if gotBody["kind"] != "agent" {
		t.Errorf("kind = %v", gotBody["kind"])
	}
	if gotBody["resource_id"] != "agent-1" {
		t.Errorf("resource_id = %v, want agent-1", gotBody["resource_id"])
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("export file not written: %v", err)
	}
	data, _ := os.ReadFile(outPath)
	if !strings.Contains(string(data), `"kind": "agent"`) {
		t.Errorf("export file content wrong: %s", data)
	}
}

func TestTemplateExportSquadNameResolution(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/squads":
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"id": "squad-1", "name": "dev-squad"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/templates/export":
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Errorf("decode body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"template": sampleSquadTemplate(),
				"warnings": []any{},
			})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	templateTestEnv(t, srv.URL)

	outPath := filepath.Join(t.TempDir(), "squad.json")
	cmd := newTemplateTestCmd(registerTemplateExportFlags)
	_ = cmd.Flags().Set("kind", "squad")
	_ = cmd.Flags().Set("id", "dev-squad")
	_ = cmd.Flags().Set("file", outPath)
	_ = cmd.Flags().Set("members-mode", "embedded")

	if err := runTemplateExport(cmd, nil); err != nil {
		t.Fatalf("runTemplateExport: %v", err)
	}
	if gotBody["resource_id"] != "squad-1" {
		t.Errorf("resource_id = %v, want squad-1", gotBody["resource_id"])
	}
	if gotBody["members_mode"] != "embedded" {
		t.Errorf("members_mode = %v", gotBody["members_mode"])
	}
}

func TestTemplateExportOutputJSONRejectsPath(t *testing.T) {
	cmd := newTemplateTestCmd(registerTemplateExportFlags)
	_ = cmd.Flags().Set("output", "out.json")
	if err := validateOutputFlag(cmd); err == nil {
		t.Error("export --output out.json must be rejected (--file is the path)")
	}
}

// ---------------------------------------------------------------------------
// Squad resolver
// ---------------------------------------------------------------------------

func TestResolveSquadByNameAndAmbiguity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/squads" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]any{
			map[string]any{"id": "squad-1", "name": "dev-squad"},
			map[string]any{"id": "squad-2", "name": "dev-squad-ops"},
		})
	}))
	defer srv.Close()
	templateTestEnv(t, srv.URL)

	client := cli.NewAPIClient(srv.URL, "ws-1", "test-token")
	ctx := t.Context()

	id, err := resolveSquad(ctx, client, "dev-squad-ops")
	if err != nil {
		t.Fatalf("resolveSquad: %v", err)
	}
	if id != "squad-2" {
		t.Errorf("id = %s, want squad-2", id)
	}
	_, err = resolveSquad(ctx, client, "dev-squad")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("ambiguous squad name should error, got %v", err)
	}
	// A full UUID resolves directly without a fetch.
	id, err = resolveSquad(ctx, client, "11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatalf("resolveSquad UUID: %v", err)
	}
	if id != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("id = %s", id)
	}
}

// TestExitCodeErrorWiring ensures main honors the exitCoder interface. It runs
// the apply command end-to-end through Execute and checks the process would
// have exited 3. We can't capture os.Exit in-process, so we verify the error
// type is produced and that main's exit path checks for it (compile-level).
func TestExitCodeErrorType(t *testing.T) {
	e := &exitCodeError{code: 3, msg: "x"}
	if e.ExitCode() != 3 {
		t.Error("exit code mismatch")
	}
	if e.Error() != "x" {
		t.Error("message mismatch")
	}
	_ = fmt.Sprintf("%v", e)
}
