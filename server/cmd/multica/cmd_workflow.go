package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var workflowCmd = &cobra.Command{
	Use:   "workflow",
	Short: "Work with workflows",
}

var workflowCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new workflow from an issue",
	RunE:  runWorkflowCreate,
}

var workflowGetCmd = &cobra.Command{
	Use:   "get <workflow-id>",
	Short: "Get workflow details",
	Args:  exactArgs(1),
	RunE:  runWorkflowGet,
}

func init() {
	workflowCmd.AddCommand(workflowCreateCmd)
	workflowCmd.AddCommand(workflowGetCmd)

	// workflow create
	workflowCreateCmd.Flags().String("name", "", "Workflow name (required)")
	workflowCreateCmd.Flags().String("description", "", "Workflow description")
	workflowCreateCmd.Flags().String("issue-id", "", "Source issue ID (key like CLO-123 or full UUID)")
	workflowCreateCmd.Flags().String("template", "", "Workflow template key (default: software_rd)")
	workflowCreateCmd.Flags().String("output", "json", "Output format: table or json")

	// workflow get
	workflowGetCmd.Flags().String("output", "json", "Output format: table or json")
}

// ---------------------------------------------------------------------------
// Workflow commands
// ---------------------------------------------------------------------------

func runWorkflowCreate(cmd *cobra.Command, _ []string) error {
	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	issueRef, _ := cmd.Flags().GetString("issue-id")
	if issueRef == "" {
		return fmt.Errorf("--issue-id is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issue, err := resolveIssueRef(ctx, client, issueRef)
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	body := map[string]any{
		"name":            name,
		"source_issue_id": issue.ID,
	}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}
	if v, _ := cmd.Flags().GetString("template"); v != "" {
		body["template_key"] = v
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/workflows", body, &result); err != nil {
		return fmt.Errorf("create workflow: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "NAME", "STATUS", "CURRENT STAGE"}
		rows := [][]string{{
			strVal(result, "id"),
			strVal(result, "name"),
			strVal(result, "status"),
			fmt.Sprintf("%v", asInt(result["current_stage"])),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, result)
}

func runWorkflowGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var wf map[string]any
	if err := client.GetJSON(ctx, "/api/workflows/"+url.PathEscape(args[0]), &wf); err != nil {
		return fmt.Errorf("get workflow: %w", err)
	}

	// Best-effort artifact fetch for the completed-artifacts view (FR-2). A
	// failure here must not fail the whole command — keep the workflow data
	// and mark artifacts unavailable.
	artifacts := []map[string]any{}
	params := url.Values{}
	params.Set("workflow_id", args[0])
	if client.WorkspaceID != "" {
		params.Set("workspace_id", client.WorkspaceID)
	}
	var artifactResp map[string]any
	if err := client.GetJSON(ctx, "/api/artifacts?"+params.Encode(), &artifactResp); err == nil {
		if items, ok := artifactResp["items"].([]any); ok {
			for _, raw := range items {
				if a, ok := raw.(map[string]any); ok {
					artifacts = append(artifacts, a)
				}
			}
		}
	}

	cur := deriveWorkflowCurrent(wf, client, ctx)

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "NAME", "STATUS", "CURRENT STAGE", "CURRENT NODE", "CURRENT AGENT", "CURRENT TASK"}
		rows := [][]string{{
			strVal(wf, "id"),
			strVal(wf, "name"),
			strVal(wf, "status"),
			fmt.Sprintf("%v", asInt(wf["current_stage"])),
			cur.node,
			cur.agent,
			cur.task,
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		if len(artifacts) > 0 {
			cli.PrintTable(os.Stderr, []string{"TITLE", "TYPE", "STATUS", "VERSION"}, artifactRows(artifacts))
		}
		return nil
	}

	out := map[string]any{
		"id":              strVal(wf, "id"),
		"name":            strVal(wf, "name"),
		"status":          strVal(wf, "status"),
		"current_stage":   wf["current_stage"],
		"current_node":    cur.node,
		"current_agent":   cur.agent,
		"current_task":    cur.task,
		"artifacts":       artifacts,
		"stages":          wf["stages"],
		"progress":        wf["progress"],
		"source_issue_id": strVal(wf, "source_issue_id"),
	}
	return cli.PrintJSON(os.Stdout, out)
}

// asInt renders an integer value from JSON-decoded data (int or float64).
func asInt(raw any) int {
	switch n := raw.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i)
		}
	}
	return 0
}

// workflowCurrent is the derived "what's active right now" summary for a
// workflow: the first non-terminal node in the current stage, its assignee,
// and its mapped task issue.
type workflowCurrent struct {
	node  string
	agent string
	task  string
}

// deriveWorkflowCurrent walks the workflow response's stages to find the first
// non-terminal node in the current stage. The assignee display name is
// resolved through the actor lookup (best-effort).
func deriveWorkflowCurrent(wf map[string]any, client *cli.APIClient, ctx context.Context) workflowCurrent {
	stagesRaw, _ := wf["stages"].([]any)
	if len(stagesRaw) == 0 {
		return workflowCurrent{}
	}
	curStage := asInt(wf["current_stage"])

	// Locate the stage matching current_stage; fall back to the first stage.
	var selected map[string]any
	for _, raw := range stagesRaw {
		s, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if asInt(s["stage"]) == curStage {
			selected = s
			break
		}
	}
	if selected == nil {
		selected, _ = stagesRaw[0].(map[string]any)
	}
	if selected == nil {
		return workflowCurrent{}
	}

	nodesRaw, _ := selected["nodes"].([]any)
	for _, raw := range nodesRaw {
		n, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		switch strVal(n, "status") {
		case "done", "cancelled", "blocked", "backlog":
			continue
		}
		return workflowCurrent{
			node:  strVal(n, "name"),
			agent: formatAssignee(n, loadActorDisplayLookup(ctx, client)),
			task:  strVal(n, "issue_id"),
		}
	}
	return workflowCurrent{}
}

func artifactRows(artifacts []map[string]any) [][]string {
	rows := make([][]string, 0, len(artifacts))
	for _, a := range artifacts {
		rows = append(rows, []string{
			strVal(a, "title"),
			strVal(a, "type"),
			strVal(a, "status"),
			fmt.Sprintf("%v", asInt(a["version"])),
		})
	}
	return rows
}
