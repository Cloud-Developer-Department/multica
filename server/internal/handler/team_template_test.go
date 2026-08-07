package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/teamtmpl"
)

// --- helpers ---

// installTeamTemplateRegistry swaps the package-level catalog for the given
// templates and restores the original on test cleanup.
func installTeamTemplateRegistry(t *testing.T, templates ...teamtmpl.TeamTemplate) {
	t.Helper()
	reg, err := teamtmpl.NewRegistry(templates...)
	if err != nil {
		t.Fatalf("build team template registry: %v", err)
	}
	orig := teamTemplates
	teamTemplates = reg
	t.Cleanup(func() { teamTemplates = orig })
}

// teamTemplateFixture returns a valid template whose every resource name is
// scoped by `id` so tests never collide with each other or the seeded fixture.
func teamTemplateFixture(id string) teamtmpl.TeamTemplate {
	skillName := "tmpl-skill-" + id
	agentA := "tmpl-agent-" + id + "-a"
	agentB := "tmpl-agent-" + id + "-b"
	return teamtmpl.TeamTemplate{
		Slug:        "tmpl-" + id,
		Name:        "Team template " + id,
		Description: "fixture template",
		Category:    "Engineering",
		Icon:        "Users",
		Accent:      "primary",
		Skills: []teamtmpl.SkillDef{
			{
				Name:        skillName,
				Description: "shared workflow skill",
				Content:     "# Fixture workflow\nbody " + id,
			},
		},
		Agents: []teamtmpl.AgentDef{
			{
				Name:               agentA,
				Description:        "agent a",
				Model:              "gpt-4o",
				Instructions:       "You are agent a.",
				Skills:             []string{skillName},
				Visibility:         "workspace",
				MaxConcurrentTasks: 6,
			},
			{
				Name:               agentB,
				Description:        "agent b",
				Model:              "deepseek-v4-flash",
				Instructions:       "You are agent b.",
				Skills:             []string{skillName},
				Visibility:         "private",
				MaxConcurrentTasks: 0,
			},
		},
		Squads: []teamtmpl.SquadDef{
			{
				Name:         "tmpl-squad-" + id,
				Description:  "engineering squad",
				Instructions: "squad instructions",
				Leader:       agentA,
				Members: []teamtmpl.MemberRef{
					{AgentName: agentB, Role: "member"},
				},
			},
		},
	}
}

// applyTeamTemplateRequest builds a POST apply request with the shared test
// identity/workspace headers and the slug in the chi route context.
func applyTeamTemplateRequest(slug string, body any) *http.Request {
	req := newRequest(http.MethodPost, "/api/team-templates/"+slug+"/apply", body)
	return withURLParam(req, "slug", slug)
}

// decodeApplyResponse parses the apply response body.
func decodeApplyResponse(t *testing.T, rec *httptest.ResponseRecorder) ApplyTeamTemplateResponse {
	t.Helper()
	var resp ApplyTeamTemplateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode apply response %q: %v", rec.Body.String(), err)
	}
	return resp
}

// cleanupTeamTemplateApply deletes the rows created by a successful apply.
func cleanupTeamTemplateApply(t *testing.T, resp ApplyTeamTemplateResponse) {
	t.Helper()
	ctx := context.Background()
	for _, s := range resp.Squads.Created {
		testPool.Exec(ctx, `DELETE FROM squad_member WHERE squad_id = $1`, s.ID)
		testPool.Exec(ctx, `DELETE FROM squad WHERE id = $1`, s.ID)
	}
	for _, a := range resp.Agents.Created {
		testPool.Exec(ctx, `DELETE FROM agent_invocation_target WHERE agent_id = $1`, a.ID)
		testPool.Exec(ctx, `DELETE FROM agent_skill WHERE agent_id = $1`, a.ID)
		testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, a.ID)
	}
	for _, sk := range resp.Skills.Created {
		testPool.Exec(ctx, `DELETE FROM skill_file WHERE skill_id = $1`, sk.ID)
		testPool.Exec(ctx, `DELETE FROM skill WHERE id = $1`, sk.ID)
	}
}

// agentModelByName reads an agent's model from the test workspace.
func agentModelByName(t *testing.T, name string) (string, error) {
	t.Helper()
	var model string
	err := testPool.QueryRow(context.Background(),
		`SELECT COALESCE(model, '') FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, name).Scan(&model)
	return model, err
}

// --- list + get ---

func TestListTeamTemplatesEmptyIsEmptyArray(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	installTeamTemplateRegistry(t)

	w := httptest.NewRecorder()
	testHandler.ListTeamTemplates(w, newRequest(http.MethodGet, "/api/team-templates", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListTeamTemplates: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := strings.TrimSpace(w.Body.String()); got != "[]" {
		t.Fatalf("ListTeamTemplates with empty catalog: expected [], got %s", got)
	}
}

func TestListTeamTemplatesReturnsSummaries(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	installTeamTemplateRegistry(t, teamTemplateFixture("summary"))

	w := httptest.NewRecorder()
	testHandler.ListTeamTemplates(w, newRequest(http.MethodGet, "/api/team-templates", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListTeamTemplates: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got []TeamTemplateSummaryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 template, got %d", len(got))
	}
	tmpl := got[0]
	if tmpl.Slug != "tmpl-summary" || tmpl.Name != "Team template summary" {
		t.Fatalf("unexpected summary slug/name: %+v", tmpl)
	}
	if len(tmpl.Skills) != 1 || tmpl.Skills[0].Name != "tmpl-skill-summary" {
		t.Fatalf("summary skills wrong: %+v", tmpl.Skills)
	}
	if len(tmpl.Agents) != 2 {
		t.Fatalf("summary agents wrong: %+v", tmpl.Agents)
	}
	if len(tmpl.Squads) != 1 || tmpl.Squads[0].MemberCount != 2 { // leader + 1 member
		t.Fatalf("summary squads wrong: %+v", tmpl.Squads)
	}
	// summary must omit skill content / instructions entirely
	if strings.Contains(w.Body.String(), `"content"`) || strings.Contains(w.Body.String(), `"instructions"`) {
		t.Fatalf("summary must omit content/instructions, got %s", w.Body.String())
	}
	if tmpl.Agents[0].Skills == nil {
		t.Fatalf("summary agent skills must be an array (non-null), got nil")
	}
}

func TestGetTeamTemplateReturnsDetail(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	installTeamTemplateRegistry(t, teamTemplateFixture("detail"))

	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/team-templates/tmpl-detail", nil)
	req = withURLParam(req, "slug", "tmpl-detail")
	testHandler.GetTeamTemplate(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetTeamTemplate: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got TeamTemplateDetailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if got.Slug != "tmpl-detail" {
		t.Fatalf("detail slug = %q", got.Slug)
	}
	if len(got.Skills) != 1 || !strings.Contains(got.Skills[0].Content, "detail") {
		t.Fatalf("detail skills must carry content: %+v", got.Skills)
	}
	if got.Agents[0].Instructions == "" || got.Agents[0].Visibility == "" {
		t.Fatalf("detail agents must carry instructions/visibility: %+v", got.Agents)
	}
	if got.Squads[0].Instructions != "squad instructions" {
		t.Fatalf("detail squads must carry instructions: %+v", got.Squads)
	}
	if got.Skills == nil || got.Agents == nil || got.Squads == nil {
		t.Fatalf("detail arrays must be non-null: %+v", got)
	}
}

func TestGetTeamTemplateUnknownSlug404(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	installTeamTemplateRegistry(t, teamTemplateFixture("get-404"))

	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/team-templates/nope", nil)
	req = withURLParam(req, "slug", "nope")
	testHandler.GetTeamTemplate(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GetTeamTemplate unknown slug: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// --- apply ---

func TestApplyTeamTemplateFirstApplyCreatesAll(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	id := "first"
	installTeamTemplateRegistry(t, teamTemplateFixture(id))
	runtimeID := handlerTestRuntimeID(t)

	w := httptest.NewRecorder()
	testHandler.ApplyTeamTemplate(w, applyTeamTemplateRequest("tmpl-"+id, map[string]any{
		"runtime_id": runtimeID,
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("first apply: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeApplyResponse(t, w)
	t.Cleanup(func() { cleanupTeamTemplateApply(t, resp) })

	if resp.TemplateSlug != "tmpl-"+id {
		t.Fatalf("template_slug = %q", resp.TemplateSlug)
	}
	if len(resp.Skills.Created) != 1 || len(resp.Skills.Reused) != 0 {
		t.Fatalf("first apply skills = created %d reused %d, want 1/0", len(resp.Skills.Created), len(resp.Skills.Reused))
	}
	if len(resp.Agents.Created) != 2 || len(resp.Agents.Reused) != 0 {
		t.Fatalf("first apply agents = created %d reused %d, want 2/0", len(resp.Agents.Created), len(resp.Agents.Reused))
	}
	if len(resp.Squads.Created) != 1 || len(resp.Squads.Reused) != 0 {
		t.Fatalf("first apply squads = created %d reused %d, want 1/0", len(resp.Squads.Created), len(resp.Squads.Reused))
	}
	// arrays must be non-null even when empty
	if resp.Skills.Reused == nil || resp.Agents.Reused == nil || resp.Squads.Reused == nil {
		t.Fatalf("apply response empty arrays must be non-null: %+v", resp)
	}
	// verify embedded skill content landed
	var content string
	if err := testPool.QueryRow(context.Background(),
		`SELECT content FROM skill WHERE id = $1`, resp.Skills.Created[0].ID).Scan(&content); err != nil {
		t.Fatalf("load created skill: %v", err)
	}
	if !strings.Contains(content, id) {
		t.Fatalf("created skill content = %q, want fixture body %q", content, id)
	}
	// verify squad leader + member rows exist
	var leaderCount int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM squad_member WHERE squad_id = $1 AND role = 'leader'`, resp.Squads.Created[0].ID).Scan(&leaderCount); err != nil {
		t.Fatalf("count squad leader: %v", err)
	}
	if leaderCount != 1 {
		t.Fatalf("squad leader rows = %d, want 1", leaderCount)
	}
}

func TestApplyTeamTemplateRepeatApplyReusesAll(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	id := "repeat"
	installTeamTemplateRegistry(t, teamTemplateFixture(id))
	runtimeID := handlerTestRuntimeID(t)

	body := map[string]any{"runtime_id": runtimeID}

	w := httptest.NewRecorder()
	testHandler.ApplyTeamTemplate(w, applyTeamTemplateRequest("tmpl-"+id, body))
	if w.Code != http.StatusCreated {
		t.Fatalf("first apply: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	first := decodeApplyResponse(t, w)
	t.Cleanup(func() { cleanupTeamTemplateApply(t, first) })

	w = httptest.NewRecorder()
	testHandler.ApplyTeamTemplate(w, applyTeamTemplateRequest("tmpl-"+id, body))
	if w.Code != http.StatusCreated {
		t.Fatalf("repeat apply: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	second := decodeApplyResponse(t, w)

	if len(second.Skills.Created) != 0 || len(second.Skills.Reused) != 1 {
		t.Fatalf("repeat apply skills = created %d reused %d, want 0/1", len(second.Skills.Created), len(second.Skills.Reused))
	}
	if len(second.Agents.Created) != 0 || len(second.Agents.Reused) != 2 {
		t.Fatalf("repeat apply agents = created %d reused %d, want 0/2", len(second.Agents.Created), len(second.Agents.Reused))
	}
	if len(second.Squads.Created) != 0 || len(second.Squads.Reused) != 1 {
		t.Fatalf("repeat apply squads = created %d reused %d, want 0/1", len(second.Squads.Created), len(second.Squads.Reused))
	}
	// reused IDs must point at the first apply's rows
	if second.Skills.Reused[0].ID != first.Skills.Created[0].ID {
		t.Fatalf("reused skill id = %s, want %s", second.Skills.Reused[0].ID, first.Skills.Created[0].ID)
	}
	if second.Agents.Reused[0].ID != first.Agents.Created[0].ID {
		t.Fatalf("reused agent id = %s, want %s", second.Agents.Reused[0].ID, first.Agents.Created[0].ID)
	}
	if second.Squads.Reused[0].ID != first.Squads.Created[0].ID {
		t.Fatalf("reused squad id = %s, want %s", second.Squads.Reused[0].ID, first.Squads.Created[0].ID)
	}
}

func TestApplyTeamTemplateRemoteSkillBranch(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	id := "remote"
	importURL := withMockClawHubImport(t, "review-helper")

	tmpl := teamTemplateFixture(id)
	tmpl.Skills = []teamtmpl.SkillDef{
		{Name: "tmpl-remote-" + id, Description: "remote skill", SourceURL: importURL},
	}
	for i := range tmpl.Agents {
		tmpl.Agents[i].Skills = []string{"tmpl-remote-" + id}
	}
	installTeamTemplateRegistry(t, tmpl)

	w := httptest.NewRecorder()
	testHandler.ApplyTeamTemplate(w, applyTeamTemplateRequest("tmpl-"+id, map[string]any{
		"runtime_id": handlerTestRuntimeID(t),
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("remote skill apply: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeApplyResponse(t, w)
	t.Cleanup(func() { cleanupTeamTemplateApply(t, resp) })

	if len(resp.Skills.Created) != 1 {
		t.Fatalf("remote skill created = %d, want 1", len(resp.Skills.Created))
	}
	var content string
	if err := testPool.QueryRow(context.Background(),
		`SELECT content FROM skill WHERE id = $1`, resp.Skills.Created[0].ID).Scan(&content); err != nil {
		t.Fatalf("load remote skill: %v", err)
	}
	if strings.TrimSpace(content) != "# Imported" {
		t.Fatalf("remote skill content = %q, want fetched body", content)
	}
}

func TestApplyTeamTemplateBadSourceURLReturns422WithNoResidue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	id := "badurl"
	tmpl := teamTemplateFixture(id)
	tmpl.Skills = []teamtmpl.SkillDef{
		{Name: "tmpl-bad-" + id, SourceURL: "https://example.invalid/does/not/exist"},
	}
	for i := range tmpl.Agents {
		tmpl.Agents[i].Skills = []string{"tmpl-bad-" + id}
	}
	installTeamTemplateRegistry(t, tmpl)

	w := httptest.NewRecorder()
	testHandler.ApplyTeamTemplate(w, applyTeamTemplateRequest("tmpl-"+id, map[string]any{
		"runtime_id": handlerTestRuntimeID(t),
	}))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad source_url: expected 422, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Error      string   `json:"error"`
		FailedURLs []string `json:"failed_urls"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode 422 body: %v", err)
	}
	if body.Error == "" || len(body.FailedURLs) == 0 {
		t.Fatalf("422 body missing error/failed_urls: %s", w.Body.String())
	}

	// zero residue: no skill/agent/squad created for the template names
	for _, table := range []string{"skill", "agent", "squad"} {
		var count int
		query := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE workspace_id = $1 AND name LIKE 'tmpl-bad-%%'`, table)
		if err := testPool.QueryRow(context.Background(), query, testWorkspaceID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("bad source_url left %d %s row(s); expected 0 residue", count, table)
		}
	}
}

// failSquadCreateTxStarter wraps the real transaction starter and returns a tx
// whose QueryRow fails when it sees the squad INSERT — simulating a mid-apply
// DB failure after skills and agents were already written inside the tx.
type failSquadCreateTxStarter struct {
	inner txStarter
}

func (s failSquadCreateTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &failSquadCreateTx{Tx: tx}, nil
}

type failSquadCreateTx struct {
	pgx.Tx
}

type errorRow struct{ err error }

func (r errorRow) Scan(dest ...any) error { return r.err }

func (tx *failSquadCreateTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "INSERT INTO squad") {
		return errorRow{err: errors.New("injected squad create failure")}
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func TestApplyTeamTemplateTxFailureRollsBackEverything(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	id := "txfail"
	installTeamTemplateRegistry(t, teamTemplateFixture(id))
	runtimeID := handlerTestRuntimeID(t)

	failing := *testHandler
	failing.TxStarter = failSquadCreateTxStarter{inner: testHandler.TxStarter}

	w := httptest.NewRecorder()
	failing.ApplyTeamTemplate(w, applyTeamTemplateRequest("tmpl-"+id, map[string]any{
		"runtime_id": runtimeID,
	}))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("tx failure: expected 500, got %d: %s", w.Code, w.Body.String())
	}

	// full rollback: no skill/agent/squad rows for the template names
	for _, table := range []string{"skill", "agent", "squad"} {
		var count int
		query := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE workspace_id = $1 AND name LIKE 'tmpl-%s-%%'`, table, id)
		if err := testPool.QueryRow(context.Background(), query, testWorkspaceID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("tx failure left %d %s row(s); expected full rollback", count, table)
		}
	}
}

func TestApplyTeamTemplateModelOverrides(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	id := "model"
	installTeamTemplateRegistry(t, teamTemplateFixture(id))

	agentA := "tmpl-agent-" + id + "-a"
	agentB := "tmpl-agent-" + id + "-b"

	w := httptest.NewRecorder()
	testHandler.ApplyTeamTemplate(w, applyTeamTemplateRequest("tmpl-"+id, map[string]any{
		"runtime_id": handlerTestRuntimeID(t),
		"model_overrides": map[string]string{
			agentA: "claude-3-5-sonnet",
		},
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("model override apply: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeApplyResponse(t, w)
	t.Cleanup(func() { cleanupTeamTemplateApply(t, resp) })

	modelA, err := agentModelByName(t, agentA)
	if err != nil {
		t.Fatalf("load agent A model: %v", err)
	}
	if modelA != "claude-3-5-sonnet" {
		t.Fatalf("agent A model = %q, want overridden claude-3-5-sonnet", modelA)
	}
	modelB, err := agentModelByName(t, agentB)
	if err != nil {
		t.Fatalf("load agent B model: %v", err)
	}
	if modelB != "deepseek-v4-flash" {
		t.Fatalf("agent B model = %q, want template default deepseek-v4-flash", modelB)
	}
}

func TestApplyTeamTemplateValidationErrors(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	id := "valid"
	installTeamTemplateRegistry(t, teamTemplateFixture(id))
	runtimeID := handlerTestRuntimeID(t)

	// 404: unknown slug
	w := httptest.NewRecorder()
	testHandler.ApplyTeamTemplate(w, applyTeamTemplateRequest("tmpl-missing", map[string]any{
		"runtime_id": runtimeID,
	}))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown slug: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	// 400: missing runtime_id
	w = httptest.NewRecorder()
	testHandler.ApplyTeamTemplate(w, applyTeamTemplateRequest("tmpl-"+id, map[string]any{}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing runtime_id: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "runtime_id is required") {
		t.Fatalf("missing runtime_id body = %s", w.Body.String())
	}

	// 400: model_overrides references unknown agent
	w = httptest.NewRecorder()
	testHandler.ApplyTeamTemplate(w, applyTeamTemplateRequest("tmpl-"+id, map[string]any{
		"runtime_id": runtimeID,
		"model_overrides": map[string]string{
			"ghost-agent": "claude-3-5-sonnet",
		},
	}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown override: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "model_overrides references unknown agent: ghost-agent") {
		t.Fatalf("unknown override body = %s", w.Body.String())
	}

	// 400: invalid runtime_id (not a UUID)
	w = httptest.NewRecorder()
	testHandler.ApplyTeamTemplate(w, applyTeamTemplateRequest("tmpl-"+id, map[string]any{
		"runtime_id": "not-a-uuid",
	}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid runtime_id: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// 400: invalid request body
	w = httptest.NewRecorder()
	req := applyTeamTemplateRequest("tmpl-"+id, nil)
	req.Body = http.NoBody
	testHandler.ApplyTeamTemplate(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid body: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// --- summary member-count helper is covered above; guard the embedded/summary
// model defaults ---

func TestApplyTeamTemplateDefaultMaxConcurrentTasks(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	id := "maxtasks"
	installTeamTemplateRegistry(t, teamTemplateFixture(id))

	w := httptest.NewRecorder()
	testHandler.ApplyTeamTemplate(w, applyTeamTemplateRequest("tmpl-"+id, map[string]any{
		"runtime_id": handlerTestRuntimeID(t),
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("max_concurrent_tasks apply: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeApplyResponse(t, w)
	t.Cleanup(func() { cleanupTeamTemplateApply(t, resp) })

	var maxTasks int32
	if err := testPool.QueryRow(context.Background(),
		`SELECT max_concurrent_tasks FROM agent WHERE id = $1`, resp.Agents.Created[1].ID).Scan(&maxTasks); err != nil {
		t.Fatalf("load max_concurrent_tasks: %v", err)
	}
	if maxTasks != 6 {
		t.Fatalf("default max_concurrent_tasks = %d, want 6", maxTasks)
	}
}
