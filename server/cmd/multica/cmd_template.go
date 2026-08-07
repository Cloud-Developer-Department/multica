package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/multica-ai/multica/server/internal/cli"
)

// templateCmd is the parent of the resource-template commands. Templates are
// portable, file-based agent/squad definitions (see the design doc
// docs/plans/2026-08-05-001-feat-agent-squad-template-design.md) that can be
// exported from one workspace and applied in another. The CLI is the phase-1
// interaction surface; there is no Web UI yet.
var templateCmd = &cobra.Command{
	Use:   "template",
	Short: "Work with resource templates (export / validate / apply)",
}

// templateEnvPlaceholder is the marker value written for an env key the user
// skipped during interactive credential input. It is never sent to the server
// as a real value; skipped keys are reported so the user can fill them later
// with `multica agent env`.
const templateEnvPlaceholder = "{{PLACEHOLDER}}"

// exitCodeError lets a command force a specific process exit code (2 for a
// validation failure, 3 for a conflict that needs a decision) that the generic
// cli.ExitCodeFor mapping cannot express. main detects it via the ExitCode
// interface and exits with that code instead of running cli.ExitCodeFor.
type exitCodeError struct {
	code int
	msg  string
}

func (e *exitCodeError) Error() string { return e.msg }
func (e *exitCodeError) ExitCode() int { return e.code }

// setSegment is one step of a --set dotted path. Exactly one of key or index
// is meaningful: index is used for the `members[0]` array syntax.
type setSegment struct {
	key   string
	index int
	isIdx bool
}

// envKeyRef identifies one environment variable declared by a template,
// addressed by the apply-time agent ref wrapper (see custom_env file format).
type envKeyRef struct {
	agentRef string
	key      string
	required bool
}

func init() {
	templateCmd.AddCommand(templateExportCmd)
	templateCmd.AddCommand(templateValidateCmd)
	templateCmd.AddCommand(templateApplyCmd)
	registerTemplateExportFlags(templateExportCmd)
	registerTemplateValidateFlags(templateValidateCmd)
	registerTemplateApplyFlags(templateApplyCmd)
}

// registerTemplateExportFlags registers every flag runTemplateExport reads. It
// is shared between init() and the tests so both stay in lockstep.
func registerTemplateExportFlags(cmd *cobra.Command) {
	cmd.Flags().String("kind", "", "Resource kind to export: agent or squad (required)")
	cmd.Flags().String("id", "", "Agent or squad name (or full UUID) to export (required)")
	cmd.Flags().String("file", "", "Path of the template JSON file to write (required)")
	cmd.Flags().String("members-mode", "embedded", "Squad member representation: embedded or references")
	cmd.Flags().String("version", "", "Template metadata version (SemVer; default 1.0.0)")
	cmd.Flags().String("description", "", "Template metadata description")
	cmd.Flags().StringSlice("tag", nil, "Template metadata tag (repeatable)")
	cmd.Flags().String("output", "table", "Output format: table or json")
}

// registerTemplateValidateFlags registers every flag runTemplateValidate
// reads. It is shared between init() and the tests so both stay in lockstep.
func registerTemplateValidateFlags(cmd *cobra.Command) {
	cmd.Flags().String("file", "", "Path of the template JSON file to validate (required)")
	cmd.Flags().String("runtime-id", "", "Target runtime ID; model/thinking_level/service_tier are re-checked against it (required)")
	cmd.Flags().String("members-mode", "", "Override the template's member representation (embedded or references)")
	cmd.Flags().String("output", "table", "Output format: table or json")
}

// registerTemplateApplyFlags registers every flag runTemplateApply reads. It
// is shared between init() and the tests so both stay in lockstep.
//
// Note the deliberate absence of a plaintext --custom-env flag: template
// scenarios routinely end up pasted into chat logs, issue comments and
// terminal scrollback, so credentials are only accepted through
// --custom-env-file (recommended, e.g. chmod 0600) or --custom-env-stdin.
func registerTemplateApplyFlags(cmd *cobra.Command) {
	cmd.Flags().String("file", "", "Path of the template JSON file to apply (required)")
	cmd.Flags().String("runtime-id", "", "Target runtime ID (required)")
	cmd.Flags().Bool("dry-run", false, "Show the creation plan without writing anything")
	cmd.Flags().Bool("yes", false, "Skip the interactive confirmation (required for non-interactive runs)")
	cmd.Flags().String("conflict-policy", "fail", "Name-conflict strategy: fail, rename, or skip")
	cmd.Flags().String("members-mode", "", "Override the template's member representation (embedded or references)")
	cmd.Flags().StringArray("set", nil, "Override a value by template-document dotted path, e.g. metadata.name=My Team or spec.squad.members[0].role=leader (repeatable)")
	cmd.Flags().String("overrides-file", "", "Read batch overrides from a JSON file (a partial template document, deep-merged; --set wins on conflicts)")
	cmd.Flags().String("custom-env-file", "", "Read agent custom_env values from a JSON file keyed by agent ref (recommended: chmod 0600). Mutually exclusive with --custom-env-stdin.")
	cmd.Flags().Bool("custom-env-stdin", false, "Read agent custom_env values as JSON from stdin. Mutually exclusive with --custom-env-file.")
	cmd.Flags().Bool("install-missing-skills", false, "Install skills reported as missing by validate (only URLs already present in the template spec are sent)")
	cmd.Flags().String("idempotency-key", "", "Idempotency key; reuse the same key to retry a timed-out apply without duplicating resources. Generated and echoed when omitted.")
	cmd.Flags().String("output", "table", "Output format: table or json")
}

// ---------------------------------------------------------------------------
// export
// ---------------------------------------------------------------------------

var templateExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export an agent or squad as a portable template file",
	Args:  cobra.NoArgs,
	RunE:  runTemplateExport,
}

func runTemplateExport(cmd *cobra.Command, _ []string) error {
	if err := validateOutputFlag(cmd); err != nil {
		return err
	}
	kind, _ := cmd.Flags().GetString("kind")
	if kind != "agent" && kind != "squad" {
		return fmt.Errorf("--kind must be 'agent' or 'squad'")
	}
	id, _ := cmd.Flags().GetString("id")
	if id == "" {
		return fmt.Errorf("--id is required (agent or squad name, or a full UUID)")
	}
	filePath, _ := cmd.Flags().GetString("file")
	if filePath == "" {
		return fmt.Errorf("--file is required (path of the template JSON file to write)")
	}
	membersMode, _ := cmd.Flags().GetString("members-mode")
	if err := validateMembersMode(membersMode, false); err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	resourceID := id
	if !uuidRegexp.MatchString(id) {
		if kind == "agent" {
			resourceID, err = resolveAgent(ctx, client, id)
		} else {
			resourceID, err = resolveSquad(ctx, client, id)
		}
		if err != nil {
			return fmt.Errorf("resolve %s: %w", kind, err)
		}
	}

	body := map[string]any{
		"kind":         kind,
		"resource_id":  resourceID,
		"members_mode": membersMode,
	}
	metadata := map[string]any{}
	if cmd.Flags().Changed("version") {
		metadata["version"], _ = cmd.Flags().GetString("version")
	}
	if cmd.Flags().Changed("description") {
		metadata["description"], _ = cmd.Flags().GetString("description")
	}
	if tags, _ := cmd.Flags().GetStringSlice("tag"); len(tags) > 0 {
		metadata["tags"] = tags
	}
	if len(metadata) > 0 {
		body["metadata"] = metadata
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/templates/export", body, &result); err != nil {
		return fmt.Errorf("export %s template: %w", kind, err)
	}

	tmpl, ok := result["template"].(map[string]any)
	if !ok {
		return fmt.Errorf("export response is missing the template document")
	}

	content, err := json.MarshalIndent(tmpl, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal template: %w", err)
	}
	content = append(content, '\n')
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		return fmt.Errorf("write template file: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Template exported to %s\n", filePath)
	fmt.Printf("  template_id:  %s\n", strVal(tmpl, "template_id"))
	fmt.Printf("  schema:       %s\n", strVal(tmpl, "schema_version"))
	fmt.Printf("  kind:         %s\n", strVal(tmpl, "kind"))
	printTemplateWarnings(result)
	return nil
}

// ---------------------------------------------------------------------------
// validate
// ---------------------------------------------------------------------------

var templateValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate a template file against the target runtime",
	Args:  cobra.NoArgs,
	RunE:  runTemplateValidate,
}

func runTemplateValidate(cmd *cobra.Command, _ []string) error {
	if err := validateOutputFlag(cmd); err != nil {
		return err
	}
	filePath, _ := cmd.Flags().GetString("file")
	if filePath == "" {
		return fmt.Errorf("--file is required (path of the template JSON file)")
	}
	runtimeID, _ := cmd.Flags().GetString("runtime-id")
	if runtimeID == "" {
		return fmt.Errorf("--runtime-id is required: model/thinking_level/service_tier are re-checked against the target runtime")
	}
	if cmd.Flags().Changed("members-mode") {
		membersMode, _ := cmd.Flags().GetString("members-mode")
		if err := validateMembersMode(membersMode, false); err != nil {
			return err
		}
	}

	tmpl, err := readTemplateFile(filePath)
	if err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{
		"template":          tmpl,
		"target_runtime_id": runtimeID,
	}
	if cmd.Flags().Changed("members-mode") {
		body["members_mode"], _ = cmd.Flags().GetString("members-mode")
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/templates/validate", body, &result); err != nil {
		return handleTemplateHTTPError(err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		if err := cli.PrintJSON(os.Stdout, result); err != nil {
			return err
		}
	} else {
		printValidateTable(result)
	}

	return templateResponseExitError(result, "fail")
}

// ---------------------------------------------------------------------------
// apply
// ---------------------------------------------------------------------------

var templateApplyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply a template to create agents/squads in the target workspace",
	Args:  cobra.NoArgs,
	RunE:  runTemplateApply,
}

func runTemplateApply(cmd *cobra.Command, _ []string) error {
	if err := validateOutputFlag(cmd); err != nil {
		return err
	}
	filePath, _ := cmd.Flags().GetString("file")
	if filePath == "" {
		return fmt.Errorf("--file is required (path of the template JSON file)")
	}
	runtimeID, _ := cmd.Flags().GetString("runtime-id")
	if runtimeID == "" {
		return fmt.Errorf("--runtime-id is required: model/thinking_level/service_tier are re-checked against the target runtime")
	}
	conflictPolicy, _ := cmd.Flags().GetString("conflict-policy")
	switch conflictPolicy {
	case "fail", "rename", "skip":
	default:
		return fmt.Errorf("--conflict-policy must be one of: fail, rename, skip")
	}
	if cmd.Flags().Changed("members-mode") {
		membersMode, _ := cmd.Flags().GetString("members-mode")
		if err := validateMembersMode(membersMode, false); err != nil {
			return err
		}
	}

	tmpl, err := readTemplateFile(filePath)
	if err != nil {
		return err
	}

	// Apply overrides to a copy of the template document: --overrides-file is
	// deep-merged first, then --set wins on any conflicting path (the command
	// line is "closer" to intent than a file). The server receives the fully
	// resolved template.
	if err := applyTemplateOverrides(cmd, tmpl); err != nil {
		return err
	}

	env, skippedEnv, err := resolveTemplateEnv(cmd, tmpl)
	if err != nil {
		return err
	}
	// Placeholders from skipped keys must never be sent as real values.
	env = stripEnvPlaceholders(env)

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	idempotencyKey, _ := cmd.Flags().GetString("idempotency-key")
	if idempotencyKey == "" {
		idempotencyKey = newIdempotencyKey()
		fmt.Fprintf(os.Stderr, "Idempotency key: %s (reuse it to retry safely after a timeout)\n", idempotencyKey)
	}

	installSkills := []string{}
	if install, _ := cmd.Flags().GetBool("install-missing-skills"); install {
		installSkills, err = missingSkillURLs(ctx, client, tmpl, runtimeID)
		if err != nil {
			return err
		}
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	yes, _ := cmd.Flags().GetBool("yes")
	stdinIsTTY := term.IsTerminal(int(os.Stdin.Fd()))

	if !dryRun && !yes && !stdinIsTTY {
		return fmt.Errorf("non-interactive apply requires --yes (or use --dry-run to preview the plan)")
	}

	body := map[string]any{
		"template":               tmpl,
		"target_runtime_id":      runtimeID,
		"conflict_policy":        conflictPolicy,
		"idempotency_key":        idempotencyKey,
		"env":                    env,
		"install_missing_skills": installSkills,
		"dry_run":                dryRun,
	}
	if cmd.Flags().Changed("members-mode") {
		body["members_mode"], _ = cmd.Flags().GetString("members-mode")
	}

	// Interactive runs (no --yes, TTY): preview with dry_run=true, surface
	// conflicts and required inputs, then confirm before writing anything.
	if !dryRun && !yes {
		preview := cloneBody(body)
		preview["dry_run"] = true
		var planResult map[string]any
		if err := client.PostJSON(ctx, "/api/templates/apply", preview, &planResult); err != nil {
			return handleTemplateHTTPError(err)
		}
		printApplyPlan(planResult)
		if err := printSkippedEnv(skippedEnv); err != nil {
			return err
		}
		if codeErr := templateResponseExitError(planResult, conflictPolicy); codeErr != nil {
			return codeErr
		}
		ok, err := confirmApply(cmd)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(os.Stdout, "Apply aborted.")
			return nil
		}
	} else {
		// --yes (or --dry-run): no confirmation needed. --dry-run still prints
		// the plan below from the response.
		if err := printSkippedEnv(skippedEnv); err != nil {
			return err
		}
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/templates/apply", body, &result); err != nil {
		return handleTemplateHTTPError(err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		if err := cli.PrintJSON(os.Stdout, result); err != nil {
			return err
		}
	} else {
		if dryRun {
			printApplyPlan(result)
		} else {
			printApplyResultTable(result)
		}
	}

	return templateResponseExitError(result, conflictPolicy)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// validateOutputFlag enforces the PMO ruling that --output only controls
// format (json|table) and never accepts a file path.
func validateOutputFlag(cmd *cobra.Command) error {
	output, _ := cmd.Flags().GetString("output")
	switch output {
	case "json", "table":
		return nil
	default:
		return fmt.Errorf("--output must be 'json' or 'table', got %q (file paths are set with --file)", output)
	}
}

// validateMembersMode checks the members-mode value. allowEmpty permits "" so
// a flag registered with a default value can be skipped.
func validateMembersMode(mode string, allowEmpty bool) error {
	if mode == "" && allowEmpty {
		return nil
	}
	switch mode {
	case "embedded", "references":
		return nil
	default:
		return fmt.Errorf("--members-mode must be 'embedded' or 'references', got %q", mode)
	}
}

// readTemplateFile reads and decodes a template JSON file. The document must
// be a JSON object; anything else is rejected before it reaches the server.
func readTemplateFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read template file: %w", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("template file %s is not a valid JSON object: %w", path, err)
	}
	return doc, nil
}

// applyTemplateOverrides deep-merges --overrides-file into the template
// document, then applies each --set dotted path on top (--set wins). Old-style
// `agents.<ref>.<field>` prefixes are no longer interpreted specially: they are
// ordinary dotted paths and fail if the template document has no such key.
func applyTemplateOverrides(cmd *cobra.Command, doc map[string]any) error {
	if filePath, _ := cmd.Flags().GetString("overrides-file"); filePath != "" {
		patch, err := readTemplateFile(filePath)
		if err != nil {
			return err
		}
		deepMerge(doc, patch)
	}

	sets, _ := cmd.Flags().GetStringArray("set")
	for _, raw := range sets {
		path, value, err := parseSetOverride(raw)
		if err != nil {
			return err
		}
		if err := applySetPath(doc, path, value); err != nil {
			return err
		}
	}
	return nil
}

// parseSetOverride splits "path=value" into a parsed dotted path and a value.
// The value is decoded as JSON when possible (numbers, booleans, arrays and
// objects); otherwise it stays a plain string.
func parseSetOverride(raw string) ([]setSegment, any, error) {
	eq := strings.Index(raw, "=")
	if eq <= 0 {
		return nil, nil, fmt.Errorf("invalid --set %q: expected <dotted.path>=<value>", raw)
	}
	pathStr := strings.TrimSpace(raw[:eq])
	valueStr := raw[eq+1:]

	path, err := parseDottedPath(pathStr)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid --set path %q: %w", pathStr, err)
	}
	if len(path) == 0 {
		return nil, nil, fmt.Errorf("invalid --set %q: path must not be empty", raw)
	}

	var value any
	if err := json.Unmarshal([]byte(valueStr), &value); err != nil {
		value = valueStr
	}
	return path, value, nil
}

// parseDottedPath parses a template-document dotted path that may contain
// array indices, e.g. `spec.squad.members[0].role` → key(spec) key(squad)
// key(members) index(0) key(role). A trailing `[0]` after a key is supported;
// brackets are only parsed at segment boundaries.
func parseDottedPath(path string) ([]setSegment, error) {
	var segments []setSegment
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			continue
		}
		if idx, ok := parseTrailingIndex(part); ok {
			segments = append(segments, setSegment{key: strings.TrimSuffix(part, "["+strconv.Itoa(idx)+"]")})
			segments = append(segments, setSegment{index: idx, isIdx: true})
		} else {
			segments = append(segments, setSegment{key: part})
		}
	}
	return segments, nil
}

// parseTrailingIndex extracts a trailing `[N]` suffix from a path segment and
// reports whether one was present.
func parseTrailingIndex(segment string) (int, bool) {
	if !strings.HasSuffix(segment, "]") {
		return 0, false
	}
	open := strings.LastIndex(segment, "[")
	if open <= 0 {
		return 0, false
	}
	digits := segment[open+1 : len(segment)-1]
	if digits == "" {
		return 0, false
	}
	idx, err := strconv.Atoi(digits)
	if err != nil || idx < 0 {
		return 0, false
	}
	return idx, true
}

// applySetPath writes value into doc following path. Missing intermediate
// object keys are created so partial overrides (e.g. a template that omits an
// optional field) still work; an out-of-range array index or descending into a
// non-container is an error, mirroring the server's TEMPLATE_INVALID+path.
func applySetPath(doc map[string]any, path []setSegment, value any) error {
	if len(path) == 0 {
		return errors.New("empty override path")
	}
	current := any(doc)
	for i, seg := range path {
		last := i == len(path)-1
		switch {
		case seg.isIdx:
			arr, ok := current.([]any)
			if !ok {
				return fmt.Errorf("override path %s: %q is not an array", dottedPathString(path[:i+1]), segString(seg))
			}
			if seg.index >= len(arr) {
				return fmt.Errorf("override path %s: array index %d out of range (len %d)", dottedPathString(path[:i+1]), seg.index, len(arr))
			}
			if last {
				arr[seg.index] = value
				return nil
			}
			next := arr[seg.index]
			if next == nil {
				next = map[string]any{}
				arr[seg.index] = next
			}
			current = next
		default:
			m, ok := current.(map[string]any)
			if !ok {
				return fmt.Errorf("override path %s: %q is not an object", dottedPathString(path[:i+1]), segString(seg))
			}
			if last {
				m[seg.key] = value
				return nil
			}
			next, ok := m[seg.key]
			if !ok || next == nil {
				next = map[string]any{}
				m[seg.key] = next
			}
			current = next
		}
	}
	return nil
}

func segString(s setSegment) string {
	if s.isIdx {
		return fmt.Sprintf("[%d]", s.index)
	}
	return s.key
}

func dottedPathString(path []setSegment) string {
	parts := make([]string, 0, len(path))
	for _, s := range path {
		parts = append(parts, segString(s))
	}
	return strings.Join(parts, ".")
}

// deepMerge merges patch into dst: object keys recurse, arrays and scalars
// replace wholesale.
func deepMerge(dst, patch map[string]any) {
	for k, v := range patch {
		if sub, ok := v.(map[string]any); ok {
			if dstSub, ok := dst[k].(map[string]any); ok {
				deepMerge(dstSub, sub)
				continue
			}
		}
		dst[k] = v
	}
}

// resolveTemplateEnv returns the custom_env values for the apply request, keyed
// by agent ref. Batch input via --custom-env-file / --custom-env-stdin is the
// preferred path and mutually exclusive. Otherwise the declared required env
// keys are collected from the template and prompted for interactively
// (non-echoing ReadPassword); skipped keys get a placeholder marker and are
// reported, never submitted as real values.
func resolveTemplateEnv(cmd *cobra.Command, tmpl map[string]any) (map[string]map[string]string, []envKeyRef, error) {
	filePath, _ := cmd.Flags().GetString("custom-env-file")
	fromStdin, _ := cmd.Flags().GetBool("custom-env-stdin")
	if filePath != "" && fromStdin {
		return nil, nil, errors.New("--custom-env-file and --custom-env-stdin are mutually exclusive")
	}

	if filePath != "" || fromStdin {
		raw := ""
		if fromStdin {
			buf, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return nil, nil, fmt.Errorf("read --custom-env-stdin: %w", err)
			}
			raw = string(buf)
		} else {
			buf, err := os.ReadFile(filePath)
			if err != nil {
				return nil, nil, fmt.Errorf("read --custom-env-file: %w", err)
			}
			raw = string(buf)
		}
		env, err := parseCustomEnvShape(raw)
		if err != nil {
			return nil, nil, err
		}
		return env, nil, nil
	}

	keys := collectTemplateEnvKeys(tmpl)
	env := map[string]map[string]string{}
	var skipped []envKeyRef
	if len(keys) == 0 {
		return env, nil, nil
	}

	tty := term.IsTerminal(int(os.Stdin.Fd()))
	for _, k := range keys {
		if !k.required {
			continue
		}
		var value string
		if tty {
			// Interactive fallback: read with echo disabled, allow skip.
			prompted, err := readPasswordPrompt(fmt.Sprintf("  %s.%s (required, leave empty to skip): ", k.agentRef, k.key))
			if err != nil {
				return nil, nil, err
			}
			value = prompted
		}
		if value == "" {
			// Skip path: write a placeholder marker into the env structure so
			// the key is tracked, and report it. The placeholder is stripped
			// before the apply request is sent (never submitted as a real
			// value); the user fills it in later via `multica agent env`.
			if env[k.agentRef] == nil {
				env[k.agentRef] = map[string]string{}
			}
			env[k.agentRef][k.key] = templateEnvPlaceholder
			skipped = append(skipped, k)
			continue
		}
		if env[k.agentRef] == nil {
			env[k.agentRef] = map[string]string{}
		}
		env[k.agentRef][k.key] = value
	}
	return env, skipped, nil
}

// parseCustomEnvShape parses the env payload shape shared by the file/stdin
// channels and the apply request: `{ "<agent_ref>": { "API_KEY": "…" } }`.
func parseCustomEnvShape(raw string) (map[string]map[string]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("custom env input is empty")
	}
	var outer map[string]map[string]string
	if err := json.Unmarshal([]byte(trimmed), &outer); err != nil {
		return nil, fmt.Errorf("custom env must be a JSON object keyed by agent ref, each value an object of KEY=value strings: %w", err)
	}
	return outer, nil
}

// stripEnvPlaceholders removes the placeholder marker values from an env map so
// skipped keys are never submitted as real values.
func stripEnvPlaceholders(env map[string]map[string]string) map[string]map[string]string {
	for ref, kv := range env {
		for key, value := range kv {
			if value == templateEnvPlaceholder {
				delete(kv, key)
			}
		}
		if len(kv) == 0 {
			delete(env, ref)
		}
	}
	return env
}

// readPasswordPrompt reads a value with echo disabled via golang.org/x/term,
// which also works on Windows (term.ReadPassword uses the console API there).
func readPasswordPrompt(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	fd := int(os.Stdin.Fd())
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

// collectTemplateEnvKeys gathers the env keys a template declares as required,
// addressed by apply-time agent ref. Agent templates use the agent name as the
// ref; embedded squad templates use each member's ref.
func collectTemplateEnvKeys(tmpl map[string]any) []envKeyRef {
	kind := strVal(tmpl, "kind")
	spec := nestedMap(tmpl, "spec")
	var keys []envKeyRef

	switch kind {
	case "agent":
		agent := nestedMap(spec, "agent")
		ref := strVal(agent, "name")
		for _, k := range customEnvKeysOf(agent) {
			k.agentRef = ref
			keys = append(keys, k)
		}
	case "squad":
		squad := nestedMap(spec, "squad")
		if members, ok := squad["members"].([]any); ok {
			for _, raw := range members {
				m, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				ref := strVal(m, "ref")
				if agent, ok := m["agent"].(map[string]any); ok {
					for _, k := range customEnvKeysOf(agent) {
						k.agentRef = ref
						keys = append(keys, k)
					}
				}
			}
		}
	}
	return keys
}

// customEnvKeysOf reads the custom_env_keys array of an agent spec.
func customEnvKeysOf(agent map[string]any) []envKeyRef {
	var out []envKeyRef
	raws, _ := agent["custom_env_keys"].([]any)
	for _, raw := range raws {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		key := strVal(m, "key")
		if key == "" {
			continue
		}
		required := false
		if b, ok := m["required"].(bool); ok {
			required = b
		}
		out = append(out, envKeyRef{key: key, required: required})
	}
	return out
}

// missingSkillURLs calls validate and returns the source URLs the user opted
// to install, restricted to URLs already present in the template's spec (the
// client never does name→URL resolution).
func missingSkillURLs(ctx context.Context, client *cli.APIClient, tmpl map[string]any, runtimeID string) ([]string, error) {
	body := map[string]any{
		"template":          tmpl,
		"target_runtime_id": runtimeID,
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/templates/validate", body, &result); err != nil {
		return nil, handleTemplateHTTPError(err)
	}

	templateURLs := specSourceURLs(tmpl)
	var out []string
	seen := map[string]bool{}
	if ri, ok := result["required_inputs"].(map[string]any); ok {
		missing, _ := ri["missing_skills"].([]any)
		for _, raw := range missing {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			url := strVal(m, "source_url")
			if url == "" {
				continue
			}
			installable := false
			if b, ok := m["installable"].(bool); ok {
				installable = b
			}
			if !installable || !templateURLs[url] || seen[url] {
				continue
			}
			seen[url] = true
			out = append(out, url)
		}
	}
	sort.Strings(out)
	return out, nil
}

// specSourceURLs returns the set of source_url values that appear anywhere in
// the template's spec.
func specSourceURLs(tmpl map[string]any) map[string]bool {
	urls := map[string]bool{}
	spec := nestedMap(tmpl, "spec")
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			for k, val := range t {
				if k == "source_url" {
					if s, ok := val.(string); ok && s != "" {
						urls[s] = true
					}
					continue
				}
				walk(val)
			}
		case []any:
			for _, val := range t {
				walk(val)
			}
		}
	}
	walk(spec)
	return urls
}

// newIdempotencyKey generates a client-side idempotency key.
func newIdempotencyKey() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "tmpl-unknown"
	}
	return "tmpl-" + hex.EncodeToString(b)
}

// cloneBody returns a shallow copy of a request body so callers can flip
// dry_run without mutating the original.
func cloneBody(body map[string]any) map[string]any {
	cp := make(map[string]any, len(body))
	for k, v := range body {
		cp[k] = v
	}
	return cp
}

// confirmApply asks the user to proceed.
func confirmApply(cmd *cobra.Command) (bool, error) {
	fmt.Fprint(os.Stderr, "Proceed with apply? [y/N] ")
	reader := bufio.NewReader(cmd.InOrStdin())
	answer, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes", nil
}

// handleTemplateHTTPError maps an HTTP error onto the template exit-code
// contract: 409 = conflict (exit 3), 400/422 = validation (exit 2). Other
// statuses fall through to the generic cli.ExitCodeFor mapping.
func handleTemplateHTTPError(err error) error {
	var httpErr *cli.HTTPError
	if !errors.As(err, &httpErr) {
		return err
	}
	switch httpErr.StatusCode {
	case 409:
		msg := extractTemplateServerMessage(httpErr.Body)
		if msg == "" {
			msg = "name conflict: use --conflict-policy rename or skip"
		}
		return &exitCodeError{code: 3, msg: msg}
	case 400, 422:
		msg := extractTemplateServerMessage(httpErr.Body)
		if msg == "" {
			msg = "template validation failed"
		}
		return &exitCodeError{code: 2, msg: msg}
	default:
		return err
	}
}

// extractTemplateServerMessage pulls a human message out of a server error
// body. Error bodies never echo env values (a server-side contract), and this
// helper deliberately only reads the known message fields.
func extractTemplateServerMessage(body string) string {
	body = strings.TrimSpace(body)
	if body == "" || body[0] != '{' {
		return ""
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return ""
	}
	for _, key := range []string{"message", "error", "detail", "title"} {
		if s, ok := parsed[key].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	if es, ok := parsed["errors"].([]any); ok && len(es) > 0 {
		if e0, ok := es[0].(map[string]any); ok {
			if s, ok := e0["message"].(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

// templateResponseExitError maps a 200-level validate/apply response onto the
// exit-code contract: non-empty errors → 2; conflicts under the fail policy →
// 3. Returns nil for a clean outcome.
func templateResponseExitError(result map[string]any, conflictPolicy string) error {
	if errs, ok := result["errors"].([]any); ok && len(errs) > 0 {
		msg := fmt.Sprintf("template validation failed: %d error(s)", len(errs))
		return &exitCodeError{code: 2, msg: msg}
	}
	if conflictPolicy == "fail" {
		if plan, ok := result["plan"].(map[string]any); ok {
			if conflicts, ok := plan["conflicts"].([]any); ok && len(conflicts) > 0 {
				return &exitCodeError{code: 3, msg: fmt.Sprintf("%d name conflict(s) detected; use --conflict-policy rename or skip", len(conflicts))}
			}
		}
	}
	return nil
}

// printTemplateWarnings prints any export warnings to stderr.
func printTemplateWarnings(result map[string]any) {
	warnings, _ := result["warnings"].([]any)
	for _, raw := range warnings {
		w, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		msg := strVal(w, "message")
		if msg != "" {
			fmt.Fprintf(os.Stderr, "  warning: %s\n", msg)
		}
	}
}

// printValidateTable renders a human-readable validate result. It never
// echoes env values (the response carries key names only).
func printValidateTable(result map[string]any) {
	valid, _ := result["valid"].(bool)
	if valid {
		fmt.Fprintln(os.Stdout, "Template is valid.")
	} else {
		fmt.Fprintln(os.Stdout, "Template is invalid.")
	}

	if errs, ok := result["errors"].([]any); ok && len(errs) > 0 {
		fmt.Fprintf(os.Stdout, "Errors (%d):\n", len(errs))
		for _, raw := range errs {
			e, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			code := strVal(e, "code")
			path := strVal(e, "path")
			msg := strVal(e, "message")
			if path != "" {
				fmt.Fprintf(os.Stdout, "  [%s] %s: %s\n", code, path, msg)
			} else {
				fmt.Fprintf(os.Stdout, "  [%s] %s\n", code, msg)
			}
		}
	}
	if warns, ok := result["warnings"].([]any); ok && len(warns) > 0 {
		fmt.Fprintf(os.Stdout, "Warnings (%d):\n", len(warns))
		for _, raw := range warns {
			w, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			code := strVal(w, "code")
			path := strVal(w, "path")
			msg := strVal(w, "message")
			if path != "" {
				fmt.Fprintf(os.Stdout, "  [%s] %s: %s\n", code, path, msg)
			} else {
				fmt.Fprintf(os.Stdout, "  [%s] %s\n", code, msg)
			}
		}
	}

	if ri, ok := result["required_inputs"].(map[string]any); ok {
		if envKeys, ok := ri["env_keys"].([]any); ok && len(envKeys) > 0 {
			fmt.Fprintf(os.Stdout, "Required env inputs (%d):\n", len(envKeys))
			for _, raw := range envKeys {
				k, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				fmt.Fprintf(os.Stdout, "  %s.%s\n", strVal(k, "agent_ref"), strVal(k, "key"))
			}
		}
		if missing, ok := ri["missing_skills"].([]any); ok && len(missing) > 0 {
			fmt.Fprintf(os.Stdout, "Missing skills (%d):\n", len(missing))
			for _, raw := range missing {
				s, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				fmt.Fprintf(os.Stdout, "  %s (%s)\n", strVal(s, "name"), strVal(s, "source_url"))
			}
		}
	}
}

// printApplyPlan renders a dry-run / preview plan. It shows resources to be
// created, conflicts, required inputs and permission differences — never env
// values.
func printApplyPlan(result map[string]any) {
	if dry, ok := result["dry_run"].(bool); ok && dry {
		fmt.Fprintln(os.Stdout, "Dry-run plan:")
	} else {
		fmt.Fprintln(os.Stdout, "Plan:")
	}
	printPlanResources(result)
}

func printPlanResources(result map[string]any) {
	created, ok := result["created"].(map[string]any)
	if !ok {
		return
	}
	if agents, ok := created["agents"].([]any); ok && len(agents) > 0 {
		fmt.Fprintln(os.Stdout, "  Agents to create:")
		for _, raw := range agents {
			a, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			name := strVal(a, "name")
			ref := strVal(a, "ref")
			id := strVal(a, "id")
			if name == "" {
				name = ref
			}
			if id != "" {
				fmt.Fprintf(os.Stdout, "    agent/%s (%s)\n", name, id)
			} else {
				fmt.Fprintf(os.Stdout, "    agent/%s\n", name)
			}
		}
	}
	if squads, ok := created["squads"].([]any); ok && len(squads) > 0 {
		fmt.Fprintln(os.Stdout, "  Squads to create:")
		for _, raw := range squads {
			s, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			name := strVal(s, "name")
			id := strVal(s, "id")
			if id != "" {
				fmt.Fprintf(os.Stdout, "    squad/%s (%s)\n", name, id)
			} else {
				fmt.Fprintf(os.Stdout, "    squad/%s\n", name)
			}
		}
	}

	if plan, ok := result["plan"].(map[string]any); ok {
		if conflicts, ok := plan["conflicts"].([]any); ok && len(conflicts) > 0 {
			fmt.Fprintf(os.Stdout, "  Conflicts (%d):\n", len(conflicts))
			for _, raw := range conflicts {
				c, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				fmt.Fprintf(os.Stdout, "    %s %s already exists\n", strVal(c, "kind"), strVal(c, "name"))
			}
		}
	}
}

// printApplyResultTable renders a completed apply result.
func printApplyResultTable(result map[string]any) {
	if rolledBack, ok := result["rolled_back"].(bool); ok && rolledBack {
		fmt.Fprintln(os.Stdout, "Apply rolled back; no resources were created.")
		return
	}
	if replay, ok := result["idempotent_replay"].(bool); ok && replay {
		fmt.Fprintln(os.Stdout, "Idempotent replay: resources already exist from a previous apply with this key.")
	}
	created, ok := result["created"].(map[string]any)
	if !ok {
		fmt.Fprintln(os.Stdout, "Apply completed.")
		return
	}
	if agents, ok := created["agents"].([]any); ok && len(agents) > 0 {
		fmt.Fprintln(os.Stdout, "Agents created:")
		for _, raw := range agents {
			a, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			name := strVal(a, "name")
			id := strVal(a, "id")
			fmt.Fprintf(os.Stdout, "  %s (%s)\n", name, id)
		}
	}
	if squads, ok := created["squads"].([]any); ok && len(squads) > 0 {
		fmt.Fprintln(os.Stdout, "Squads created:")
		for _, raw := range squads {
			s, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			fmt.Fprintf(os.Stdout, "  %s (%s)\n", strVal(s, "name"), strVal(s, "id"))
		}
	}
	if skipped, ok := result["skipped"].([]any); ok && len(skipped) > 0 {
		fmt.Fprintf(os.Stdout, "Skipped (%d):\n", len(skipped))
		for _, raw := range skipped {
			s, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			fmt.Fprintf(os.Stdout, "  %s\n", strVal(s, "name"))
		}
	}
	printApplyWarnings(result)
}

func printApplyWarnings(result map[string]any) {
	warnings, _ := result["warnings"].([]any)
	for _, raw := range warnings {
		w, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		msg := strVal(w, "message")
		if msg != "" {
			fmt.Fprintf(os.Stderr, "  warning: %s\n", msg)
		}
	}
}

// printSkippedEnv reports env keys the user skipped so they can fill them in
// later with `multica agent env`. The placeholder is never submitted.
func printSkippedEnv(skipped []envKeyRef) error {
	if len(skipped) == 0 {
		return nil
	}
	fmt.Fprintf(os.Stderr, "Skipped %d required env key(s) (placeholder; configure later with `multica agent env`):\n", len(skipped))
	for _, k := range skipped {
		fmt.Fprintf(os.Stderr, "  %s.%s\n", k.agentRef, k.key)
	}
	return nil
}

// resolveSquad resolves a squad name (or accepts a full UUID) to its UUID,
// scoped to the current workspace, mirroring resolveAgent.
func resolveSquad(ctx context.Context, client *cli.APIClient, nameOrID string) (string, error) {
	if uuidRegexp.MatchString(nameOrID) {
		return nameOrID, nil
	}
	if client.WorkspaceID == "" {
		return "", fmt.Errorf("workspace ID is required to resolve squads; use --workspace-id or set MULTICA_WORKSPACE_ID")
	}

	var squads []map[string]any
	if err := client.GetJSON(ctx, "/api/squads", &squads); err != nil {
		return "", fmt.Errorf("fetch squads: %w", err)
	}

	nameLower := strings.ToLower(nameOrID)
	type match struct{ ID, Name string }
	var matches []match
	for _, s := range squads {
		if strVal(s, "archived_at") != "" {
			continue
		}
		sName := strVal(s, "name")
		if strings.Contains(strings.ToLower(sName), nameLower) {
			matches = append(matches, match{ID: strVal(s, "id"), Name: sName})
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no squad found matching %q", nameOrID)
	case 1:
		return matches[0].ID, nil
	default:
		var parts []string
		for _, m := range matches {
			parts = append(parts, fmt.Sprintf("  %q (%s)", m.Name, truncateID(m.ID)))
		}
		return "", fmt.Errorf("ambiguous squad %q; matches:\n%s", nameOrID, strings.Join(parts, "\n"))
	}
}
