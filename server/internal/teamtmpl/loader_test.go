package teamtmpl

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"
)

// validTemplate returns a template that satisfies every validation rule. Tests
// mutate one field at a time to isolate a single rule.
func validTemplate() TeamTemplate {
	return TeamTemplate{
		Slug:        "alpha",
		Name:        "Alpha",
		Description: "first team template",
		Category:    "Engineering",
		Skills: []SkillDef{
			{Name: "workflow", Description: "shared conventions", Content: "skill body"},
		},
		Agents: []AgentDef{
			{Name: "dev-agent", Description: "builder", Model: "deepseek-v4-flash", Instructions: "do the work", Skills: []string{"workflow"}},
		},
		Squads: []SquadDef{
			{Name: "squad-eng", Description: "engineering", Instructions: "ship it", Leader: "dev-agent", Members: []MemberRef{}},
		},
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal template: %v", err)
	}
	return data
}

// loadOne validates a single template under templates/alpha.json.
func loadOne(t *testing.T, tmpl TeamTemplate) error {
	t.Helper()
	fsys := fstest.MapFS{
		"templates/alpha.json": &fstest.MapFile{Data: mustJSON(t, tmpl)},
	}
	_, err := loadFromFS(fsys, "templates")
	return err
}

func TestLoad_RealTemplates(t *testing.T) {
	// Exercises the production go:embed path. A malformed template would fail
	// server boot, so this must stay green. At this stage only the underscore-
	// prefixed smoke placeholder exists (excluded from the catalog), so the
	// registry may be empty — real templates land in Task D.
	if _, err := Load(); err != nil {
		t.Fatalf("Load(): %v", err)
	}
}

func TestLoadFromFS_Valid(t *testing.T) {
	fsys := fstest.MapFS{
		"templates/alpha.json": &fstest.MapFile{Data: mustJSON(t, validTemplate())},
		"templates/beta.json": &fstest.MapFile{Data: mustJSON(t, TeamTemplate{
			Slug:        "beta",
			Name:        "Beta",
			Description: "second",
			Skills: []SkillDef{
				{Name: "fetch-me", SourceURL: "https://github.com/x/y/tree/main/skills/z"},
			},
			Agents: []AgentDef{
				{Name: "ops-agent", Instructions: "run ops"},
			},
			Squads: []SquadDef{},
		})},
	}

	reg, err := loadFromFS(fsys, "templates")
	if err != nil {
		t.Fatalf("loadFromFS: %v", err)
	}
	if got, want := len(reg.List()), 2; got != want {
		t.Fatalf("List() len = %d, want %d", got, want)
	}
	// List() must be deterministic (sorted by filename).
	if reg.List()[0].Slug != "alpha" {
		t.Errorf("List()[0].Slug = %q, want alpha", reg.List()[0].Slug)
	}
	if tmpl, ok := reg.Get("beta"); !ok {
		t.Error("Get(beta) = false, want true")
	} else if len(tmpl.Skills) != 1 {
		t.Errorf("beta skills len = %d, want 1", len(tmpl.Skills))
	}
	if _, ok := reg.Get("nope"); ok {
		t.Error("Get(nope) = true, want false")
	}
}

func TestLoadFromFS_SkipsUnderscorePlaceholders(t *testing.T) {
	// Files whose basename starts with "_" are placeholders (the smoke
	// template) and must not enter the catalog — a "__smoke" slug can't pass
	// kebab-case, so this is what lets the smoke file coexist with rule 1.
	fsys := fstest.MapFS{
		"templates/__smoke.json": &fstest.MapFile{Data: []byte(`{
			"slug":"__smoke","name":"Smoke","description":"placeholder",
			"skills":[],"agents":[],"squads":[]
		}`)},
	}
	reg, err := loadFromFS(fsys, "templates")
	if err != nil {
		t.Fatalf("loadFromFS with underscore placeholder: %v", err)
	}
	if len(reg.List()) != 0 {
		t.Errorf("List() len = %d, want 0 (placeholder excluded)", len(reg.List()))
	}
	if _, ok := reg.Get("__smoke"); ok {
		t.Error("Get(__smoke) = true, want false")
	}
}

func TestLoadFromFS_MalformedJSON(t *testing.T) {
	// Boot must abort on malformed JSON — the init() panic path.
	fsys := fstest.MapFS{
		"templates/alpha.json": &fstest.MapFile{Data: []byte(`{not json`)},
	}
	_, err := loadFromFS(fsys, "templates")
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("error = %v, want substring %q", err, "parse")
	}
}

func TestValidate_Rules(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*TeamTemplate)
		wantErr string
	}{
		{
			name:    "rule1 empty slug",
			mutate:  func(t *TeamTemplate) { t.Slug = "" },
			wantErr: "missing slug",
		},
		{
			name:    "rule1 bad slug",
			mutate:  func(t *TeamTemplate) { t.Slug = "Bad_Slug" },
			wantErr: "kebab-case",
		},
		{
			name:    "rule1 slug mismatches filename",
			mutate:  func(t *TeamTemplate) { t.Slug = "other" },
			wantErr: "does not match filename",
		},
		{
			name:    "rule2 missing name",
			mutate:  func(t *TeamTemplate) { t.Name = "" },
			wantErr: "missing name",
		},
		{
			name: "rule3 duplicate skill name",
			mutate: func(t *TeamTemplate) {
				t.Skills = append(t.Skills, SkillDef{Name: "workflow", Content: "dup"})
			},
			wantErr: "duplicate skill name",
		},
		{
			name: "rule4 duplicate agent name",
			mutate: func(t *TeamTemplate) {
				t.Agents = append(t.Agents, AgentDef{Name: "dev-agent", Instructions: "dup"})
			},
			wantErr: "duplicate agent name",
		},
		{
			name: "rule5 duplicate squad name",
			mutate: func(t *TeamTemplate) {
				t.Squads = append(t.Squads, SquadDef{Name: "squad-eng", Leader: "dev-agent"})
			},
			wantErr: "duplicate squad name",
		},
		{
			name: "rule6 skill has both content and source_url",
			mutate: func(t *TeamTemplate) {
				t.Skills[0].SourceURL = "https://github.com/x/y"
			},
			wantErr: "mutually exclusive",
		},
		{
			name: "rule6 skill has neither content nor source_url",
			mutate: func(t *TeamTemplate) {
				t.Skills[0].Content = ""
			},
			wantErr: "mutually exclusive",
		},
		{
			name: "rule7 dangling skill reference",
			mutate: func(t *TeamTemplate) {
				t.Agents[0].Skills = []string{"no-such-skill"}
			},
			wantErr: "not found in skills",
		},
		{
			name: "rule8 dangling squad leader",
			mutate: func(t *TeamTemplate) {
				t.Squads[0].Leader = "no-such-agent"
			},
			wantErr: "not found in agents",
		},
		{
			name: "rule9 dangling squad member",
			mutate: func(t *TeamTemplate) {
				t.Squads[0].Members = []MemberRef{{AgentName: "no-such-agent", Role: "member"}}
			},
			wantErr: "not found in agents",
		},
		{
			name: "rule10 empty instructions",
			mutate: func(t *TeamTemplate) {
				t.Agents[0].Instructions = ""
			},
			wantErr: "missing instructions",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpl := validTemplate()
			tc.mutate(&tmpl)
			err := loadOne(t, tmpl)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidate_ZeroSectionTemplates(t *testing.T) {
	// 0-skill / 0-agent / 0-squad templates are legitimate (mirrors the
	// agenttmpl 0-skill stance): a prompt-only org layout is valid.
	tmpl := TeamTemplate{
		Slug:        "prompt-only",
		Name:        "Prompt Only",
		Description: "no resources, just the org shell",
		Skills:      []SkillDef{},
		Agents:      []AgentDef{},
		Squads:      []SquadDef{},
	}
	fsys := fstest.MapFS{
		"templates/prompt-only.json": &fstest.MapFile{Data: mustJSON(t, tmpl)},
	}
	reg, err := loadFromFS(fsys, "templates")
	if err != nil {
		t.Fatalf("loadFromFS: %v", err)
	}
	got, ok := reg.Get("prompt-only")
	if !ok {
		t.Fatal("Get(prompt-only) = false, want true")
	}
	if len(got.Skills) != 0 || len(got.Agents) != 0 || len(got.Squads) != 0 {
		t.Errorf("expected empty sections, got skills=%d agents=%d squads=%d",
			len(got.Skills), len(got.Agents), len(got.Squads))
	}
}

func TestLoadFromFS_DuplicateSlug(t *testing.T) {
	// Two valid files declaring the same slug — caught by the registry, not
	// by validate(). Slugs are unique within the registry.
	tmpl := validTemplate()
	dup := validTemplate()
	dup.Slug = "alpha" // same slug, different content
	dup.Name = "Alpha Dupe"
	fsys := fstest.MapFS{
		"templates/alpha.json": &fstest.MapFile{Data: mustJSON(t, tmpl)},
		"templates/bravo.json": &fstest.MapFile{Data: mustJSON(t, dup)},
	}
	_, err := loadFromFS(fsys, "templates")
	if err == nil || !strings.Contains(err.Error(), "duplicate slug") {
		// The bravo.json file declares slug "alpha", so filename mismatch may
		// fire first depending on validate() ordering — both are errors.
		if err == nil || !strings.Contains(err.Error(), "does not match filename") {
			t.Errorf("expected duplicate slug or filename mismatch, got %v", err)
		}
	}
}
