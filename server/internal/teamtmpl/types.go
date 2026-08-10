// Package teamtmpl loads and serves the curated team templates that power the
// "Apply team template" flow. A team template packages a whole organization —
// multiple agents, squads, and shared skills — into one static JSON file that
// gets materialised atomically into a workspace. Templates are static JSON
// files embedded at build time (see loader.go).
//
// The three sections mirror the product's three resource types:
//
//   - Skills: named skill definitions. Each is either embedded (Content) or
//     fetched from a remote location at apply time (SourceURL) — never both.
//   - Agents: named agent definitions whose Skills field references skills by
//     name. Instructions is the full prompt.
//   - Squads: named squad definitions whose Leader/Members reference agents by
//     name (Leader auto-joins the squad).
//
// Templates are intentionally repo-only: their content is part of the product
// and changes go through normal PR review. No runtime mutation, no admin UI.
package teamtmpl

// TeamTemplate is the structured representation of a `team template` JSON file
// loaded from server/internal/teamtmpl/templates/<slug>.json.
type TeamTemplate struct {
	// Slug uniquely identifies a template within the catalog. Must equal the
	// JSON file's basename so URLs like /api/team-templates/{slug} resolve
	// deterministically. Allowed characters: lowercase letters, digits, "-".
	Slug string `json:"slug"`

	// Name is the human-readable title shown in the picker.
	Name string `json:"name"`

	// Description is a one-line summary.
	Description string `json:"description"`

	// Category groups templates in the picker UI. Empty allowed.
	Category string `json:"category,omitempty"`

	// Icon is a lucide-react icon name (e.g. "Users"). Empty falls back to a
	// generic icon on the frontend.
	Icon string `json:"icon,omitempty"`

	// Accent picks the semantic color token used to tint the icon badge. Must
	// be a Multica design-system token name (e.g. "primary"), never a hardcoded
	// color.
	Accent string `json:"accent,omitempty"`

	// Skills lists the skill definitions this template materialises. Order is
	// preserved in responses.
	Skills []SkillDef `json:"skills"`

	// Agents lists the agent definitions this template materialises.
	Agents []AgentDef `json:"agents"`

	// Squads lists the squad definitions this template materialises.
	Squads []SquadDef `json:"squads"`
}

// SkillDef supports two mutually-exclusive sources (loader-validated):
//
//   - embedded: Content is non-empty → the skill is created directly in the
//     workspace from the given SKILL.md body.
//   - remote: SourceURL is non-empty → the skill is fetched at apply time via
//     the existing ImportSkill fetch chain (ClawHub / skills.sh / GitHub).
//
// Exactly one of Content / SourceURL must be non-empty.
type SkillDef struct {
	// Name is the skill's name (unique within the template, and used as the
	// workspace-level uniqueness key at apply time).
	Name string `json:"name"`

	// Description is a one-line summary shown in the picker.
	Description string `json:"description,omitempty"`

	// Content is the embedded SKILL.md body. Mutually exclusive with SourceURL.
	Content string `json:"content,omitempty"`

	// SourceURL is the remote skill location. Mutually exclusive with Content.
	SourceURL string `json:"source_url,omitempty"`

	// Files is optional auxiliary files for an embedded skill (path+content).
	Files []SkillFile `json:"files,omitempty"`
}

// SkillFile is an auxiliary file carried by an embedded SkillDef.
type SkillFile struct {
	// Path is the file path relative to the skill directory.
	Path string `json:"path"`

	// Content is the file's verbatim content.
	Content string `json:"content"`
}

// AgentDef describes one agent to materialise. Skills references SkillDef
// names by name.
type AgentDef struct {
	// Name is the agent's name (unique within the template, and used as the
	// workspace-level uniqueness key at apply time).
	Name string `json:"name"`

	// Description is a one-line summary.
	Description string `json:"description,omitempty"`

	// Model is the template's default model; can be overridden per-request via
	// model_overrides. Empty lets the platform pick the default.
	Model string `json:"model,omitempty"`

	// Instructions is the verbatim text written into the created agent's
	// `agent.instructions` column. Keep it plain markdown.
	Instructions string `json:"instructions"`

	// Skills lists skill names (referencing Skills[].Name) to attach to the
	// agent at apply time.
	Skills []string `json:"skills,omitempty"`

	// Visibility is "workspace" or "private"; empty defaults to private.
	Visibility string `json:"visibility,omitempty"`

	// MaxConcurrentTasks caps parallel task concurrency; empty defaults to 6.
	MaxConcurrentTasks int32 `json:"max_concurrent_tasks,omitempty"`
}

// SquadDef describes one squad to materialise. Leader and Members reference
// agent names. The leader is automatically added as a squad member.
type SquadDef struct {
	// Name is the squad's name (unique within the template, and used as the
	// workspace-level uniqueness key at apply time).
	Name string `json:"name"`

	// Description is a one-line summary.
	Description string `json:"description,omitempty"`

	// Instructions is the squad-level instructions text.
	Instructions string `json:"instructions,omitempty"`

	// Leader is an agent name that must exist in Agents[].
	Leader string `json:"leader"`

	// Members lists additional members (excluding the leader, who auto-joins).
	Members []MemberRef `json:"members,omitempty"`
}

// MemberRef references one agent as a squad member.
type MemberRef struct {
	// AgentName must exist in Agents[].
	AgentName string `json:"agent_name"`

	// Role is free-form text, e.g. "member".
	Role string `json:"role"`
}
