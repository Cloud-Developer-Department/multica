package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

const teamTemplateSlug = "equipment-department-1-3-9"

// teamTemplateTestRuntimeID returns the integration fixture runtime, which the
// apply endpoint requires (and the fixture's workspace owner is allowed to use).
func teamTemplateTestRuntimeID(t *testing.T) string {
	t.Helper()

	var runtimeID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT id FROM agent_runtime WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1`,
		testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("failed to load integration test runtime: %v", err)
	}
	return runtimeID
}

// TestTeamTemplatesListThroughRouter covers GET /api/team-templates: a 200 with
// a JSON array whose single shipped template carries the expected summary shape.
func TestTeamTemplatesListThroughRouter(t *testing.T) {
	resp := authRequest(t, "GET", "/api/team-templates", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ListTeamTemplates: expected 200, got %d", resp.StatusCode)
	}
	body := respBody(t, resp)
	var templates []map[string]any
	if err := json.Unmarshal([]byte(body), &templates); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(templates) != 1 {
		t.Fatalf("expected exactly 1 team template, got %d", len(templates))
	}
	tmpl := templates[0]
	if tmpl["slug"] != teamTemplateSlug {
		t.Fatalf("expected slug %q, got %v", teamTemplateSlug, tmpl["slug"])
	}
	skills, _ := tmpl["skills"].([]any)
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill in summary, got %d", len(skills))
	}
	agents, _ := tmpl["agents"].([]any)
	if len(agents) != 10 {
		t.Fatalf("expected 10 agents in summary, got %d", len(agents))
	}
	squads, _ := tmpl["squads"].([]any)
	if len(squads) != 3 {
		t.Fatalf("expected 3 squads in summary, got %d", len(squads))
	}
	// The list payload is a summary: it must not leak full instructions.
	if strings.Contains(body, `"instructions"`) {
		t.Fatal("list payload must not include instructions")
	}
}

// TestTeamTemplateGetThroughRouter covers GET /api/team-templates/{slug}: a 200
// with the full template for a known slug and a 404 for an unknown one.
func TestTeamTemplateGetThroughRouter(t *testing.T) {
	resp := authRequest(t, "GET", "/api/team-templates/"+teamTemplateSlug, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GetTeamTemplate: expected 200, got %d", resp.StatusCode)
	}
	body := respBody(t, resp)
	var tmpl map[string]any
	if err := json.Unmarshal([]byte(body), &tmpl); err != nil {
		t.Fatalf("decode detail response: %v", err)
	}
	if tmpl["slug"] != teamTemplateSlug {
		t.Fatalf("expected slug %q, got %v", teamTemplateSlug, tmpl["slug"])
	}
	agents, _ := tmpl["agents"].([]any)
	if len(agents) != 10 {
		t.Fatalf("expected 10 agents in detail, got %d", len(agents))
	}
	// Detail payload is full: agents carry instructions.
	if !strings.Contains(body, `"instructions"`) {
		t.Fatal("detail payload must include instructions")
	}

	resp = authRequest(t, "GET", "/api/team-templates/does-not-exist", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown slug: expected 404, got %d", resp.StatusCode)
	}
}

// TestTeamTemplateApplyThroughRouter covers POST /api/team-templates/{slug}/apply
// through the full router: first apply creates every resource, a repeat apply
// reuses them all, and a missing runtime_id is rejected with 400.
func TestTeamTemplateApplyThroughRouter(t *testing.T) {
	runtimeID := teamTemplateTestRuntimeID(t)
	body := map[string]any{"runtime_id": runtimeID}

	// First apply -> 201 with everything created.
	resp := authRequest(t, "POST", "/api/team-templates/"+teamTemplateSlug+"/apply", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first apply: expected 201, got %d: %s", resp.StatusCode, respBody(t, resp))
	}
	first := decodeApplyThroughRouter(t, resp)
	if n := len(first.Skills.Created); n != 1 {
		t.Fatalf("first apply: created skills = %d, want 1", n)
	}
	if n := len(first.Agents.Created); n != 10 {
		t.Fatalf("first apply: created agents = %d, want 10", n)
	}
	if n := len(first.Squads.Created); n != 3 {
		t.Fatalf("first apply: created squads = %d, want 3", n)
	}

	// Repeat apply -> 201 with everything reused and nothing created.
	resp = authRequest(t, "POST", "/api/team-templates/"+teamTemplateSlug+"/apply", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("repeat apply: expected 201, got %d: %s", resp.StatusCode, respBody(t, resp))
	}
	second := decodeApplyThroughRouter(t, resp)
	if n := len(second.Skills.Reused); n != 1 {
		t.Fatalf("repeat apply: reused skills = %d, want 1", n)
	}
	if n := len(second.Agents.Reused); n != 10 {
		t.Fatalf("repeat apply: reused agents = %d, want 10", n)
	}
	if n := len(second.Squads.Reused); n != 3 {
		t.Fatalf("repeat apply: reused squads = %d, want 3", n)
	}
	for _, outcome := range []teamTemplateResourceOutcome{second.Skills, second.Agents, second.Squads} {
		if len(outcome.Created) != 0 {
			t.Fatalf("repeat apply must create nothing, got created %+v", outcome.Created)
		}
	}
}

// TestTeamTemplateApplyMissingRuntimeIDThroughRouter covers the 400 path when
// the required runtime_id is absent.
func TestTeamTemplateApplyMissingRuntimeIDThroughRouter(t *testing.T) {
	resp := authRequest(t, "POST", "/api/team-templates/"+teamTemplateSlug+"/apply", map[string]any{})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing runtime_id: expected 400, got %d", resp.StatusCode)
	}
}

// TestTeamTemplateApplyUnknownSlugThroughRouter covers the 404 path when the
// template does not exist.
func TestTeamTemplateApplyUnknownSlugThroughRouter(t *testing.T) {
	resp := authRequest(t, "POST", "/api/team-templates/nope/apply", map[string]any{
		"runtime_id": teamTemplateTestRuntimeID(t),
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown slug apply: expected 404, got %d", resp.StatusCode)
	}
}

type teamTemplateResourceOutcome struct {
	Created []map[string]string `json:"created"`
	Reused  []map[string]string `json:"reused"`
}

type teamTemplateApplyResponse struct {
	TemplateSlug string                      `json:"template_slug"`
	Skills       teamTemplateResourceOutcome `json:"skills"`
	Agents       teamTemplateResourceOutcome `json:"agents"`
	Squads       teamTemplateResourceOutcome `json:"squads"`
}

func decodeApplyThroughRouter(t *testing.T, resp *http.Response) teamTemplateApplyResponse {
	t.Helper()
	defer resp.Body.Close()
	var parsed teamTemplateApplyResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode apply response: %v", err)
	}
	return parsed
}

// respBody drains the response body into a string. Callers must not call
// readJSON afterwards (it would fail on an empty body).
func respBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	buf := new(strings.Builder)
	if _, err := io.Copy(buf, resp.Body); err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return buf.String()
}
