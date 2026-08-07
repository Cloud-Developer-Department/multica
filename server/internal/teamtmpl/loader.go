package teamtmpl

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
)

//go:embed templates/*.json
var templateFS embed.FS

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Registry is the in-memory store of loaded team templates. It's read-only
// after construction — the only mutator is Load(), called once at server
// startup. Concurrent reads after that are safe without locking.
type Registry struct {
	bySlug map[string]TeamTemplate
	order  []string // slugs in deterministic load order, used by List()
}

// Load parses every *.json file under templates/ and returns a populated
// Registry. Any malformed template (bad JSON, missing required fields,
// slug/filename mismatch, dangling references) aborts startup — we'd rather
// fail loudly at boot than serve a half-broken apply flow.
func Load() (*Registry, error) {
	return loadFromFS(templateFS, "templates")
}

func loadFromFS(fsys fs.FS, dir string) (*Registry, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("teamtmpl: read templates dir: %w", err)
	}

	reg := &Registry{bySlug: make(map[string]TeamTemplate)}

	// Sort filenames so List() output is deterministic regardless of FS order.
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Files whose basename starts with "_" are placeholders (e.g. the
		// boot-time smoke template) and are not part of the shipped catalog.
		if strings.HasPrefix(name, "_") {
			continue
		}
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		path := dir + "/" + name
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return nil, fmt.Errorf("teamtmpl: read %s: %w", path, err)
		}

		var t TeamTemplate
		if err := json.Unmarshal(data, &t); err != nil {
			return nil, fmt.Errorf("teamtmpl: parse %s: %w", path, err)
		}

		if err := validate(t, name); err != nil {
			return nil, fmt.Errorf("teamtmpl: %s: %w", path, err)
		}

		if _, dup := reg.bySlug[t.Slug]; dup {
			return nil, fmt.Errorf("teamtmpl: duplicate slug %q (file %s)", t.Slug, path)
		}

		reg.bySlug[t.Slug] = t
		reg.order = append(reg.order, t.Slug)
	}

	return reg, nil
}

// validate enforces the invariants that the rest of the handler / UI assume.
// Cheap to run at boot — every check pays for itself the first time someone
// adds a malformed template in a PR.
func validate(t TeamTemplate, filename string) error {
	// Rule 1: slug non-empty, kebab-case, equals the filename basename.
	if t.Slug == "" {
		return fmt.Errorf("missing slug")
	}
	if !slugPattern.MatchString(t.Slug) {
		return fmt.Errorf("slug %q must be lowercase kebab-case (a-z, 0-9, -)", t.Slug)
	}
	// Slug must equal the filename basename so URL routing matches file
	// layout. Catches typos and lets `git mv` rename templates safely.
	if filename != t.Slug+".json" {
		return fmt.Errorf("slug %q does not match filename %q", t.Slug, filename)
	}

	// Rule 2: name non-empty.
	if strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("missing name")
	}

	// Rules 3-5: skill/agent/squad names unique within the template.
	if err := checkUniqueNames("skill", t.Skills, func(i int) string { return t.Skills[i].Name }); err != nil {
		return err
	}
	if err := checkUniqueNames("agent", t.Agents, func(i int) string { return t.Agents[i].Name }); err != nil {
		return err
	}
	if err := checkUniqueNames("squad", t.Squads, func(i int) string { return t.Squads[i].Name }); err != nil {
		return err
	}

	// Rule 6: each SkillDef has exactly one of Content / SourceURL.
	for i, s := range t.Skills {
		hasContent := strings.TrimSpace(s.Content) != ""
		hasSource := strings.TrimSpace(s.SourceURL) != ""
		if hasContent == hasSource {
			return fmt.Errorf("skill[%d] (%s): content and source_url are mutually exclusive; exactly one must be set", i, s.Name)
		}
	}

	// Rule 7: every agent skill reference must exist in Skills[].
	skillNames := make(map[string]struct{}, len(t.Skills))
	for _, s := range t.Skills {
		skillNames[s.Name] = struct{}{}
	}
	for i, a := range t.Agents {
		for _, ref := range a.Skills {
			if _, ok := skillNames[ref]; !ok {
				return fmt.Errorf("agent[%d] (%s): skill %q not found in skills", i, a.Name, ref)
			}
		}
	}

	// Rule 10: every agent must have non-empty instructions.
	for i, a := range t.Agents {
		if strings.TrimSpace(a.Instructions) == "" {
			return fmt.Errorf("agent[%d] (%s): missing instructions", i, a.Name)
		}
	}

	// Rules 8-9: squad leader and members must exist in Agents[].
	agentNames := make(map[string]struct{}, len(t.Agents))
	for _, a := range t.Agents {
		agentNames[a.Name] = struct{}{}
	}
	for i, sq := range t.Squads {
		if _, ok := agentNames[sq.Leader]; !ok {
			return fmt.Errorf("squad[%d] (%s): leader %q not found in agents", i, sq.Name, sq.Leader)
		}
		for j, m := range sq.Members {
			if _, ok := agentNames[m.AgentName]; !ok {
				return fmt.Errorf("squad[%d] (%s): member[%d] agent %q not found in agents", i, sq.Name, j, m.AgentName)
			}
		}
	}

	return nil
}

// checkUniqueNames reports a duplicate-name error for the given slice, or nil
// if every element's name (via getName) is distinct.
func checkUniqueNames[T any](kind string, items []T, getName func(int) string) error {
	seen := make(map[string]struct{}, len(items))
	for i := range items {
		name := getName(i)
		if _, dup := seen[name]; dup {
			return fmt.Errorf("duplicate %s name %q", kind, name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

// List returns all templates in deterministic load order.
func (r *Registry) List() []TeamTemplate {
	out := make([]TeamTemplate, 0, len(r.order))
	for _, slug := range r.order {
		out = append(out, r.bySlug[slug])
	}
	return out
}

// Get returns the template with the given slug, or false if not found.
func (r *Registry) Get(slug string) (TeamTemplate, bool) {
	t, ok := r.bySlug[slug]
	return t, ok
}
