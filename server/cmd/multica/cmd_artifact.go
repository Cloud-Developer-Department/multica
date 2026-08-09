package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var artifactCmd = &cobra.Command{
	Use:   "artifact",
	Short: "Work with artifacts",
}

var artifactSubmitCmd = &cobra.Command{
	Use:   "submit",
	Short: "Submit an artifact for a workflow node",
	RunE:  runArtifactSubmit,
}

var artifactListCmd = &cobra.Command{
	Use:   "list",
	Short: "List artifacts in the workspace",
	RunE:  runArtifactList,
}

// artifactTypeAliases maps user-friendly --type spellings (from the CLO-175
// requirement doc) onto the backend artifact type enum. The backend accepts
// exactly: requirements, architecture, development, testing, code_review,
// security, documentation, deployment, other. The CLI normalizes aliases here
// so agents can pass e.g. `--type requirement` without tripping the server's
// CHECK constraint.
var artifactTypeAliases = map[string]string{
	"requirement":   "requirements",
	"requirements":  "requirements",
	"architecture":  "architecture",
	"development":   "development",
	"code":          "development",
	"testing":       "testing",
	"test":          "testing",
	"test_report":   "testing",
	"code_review":   "code_review",
	"review":        "code_review",
	"security":      "security",
	"documentation": "documentation",
	"doc":           "documentation",
	"deployment":    "deployment",
	"deploy":        "deployment",
	"other":         "other",
}

func normalizeArtifactType(input string) (string, error) {
	t := strings.ToLower(strings.TrimSpace(input))
	if normalized, ok := artifactTypeAliases[t]; ok {
		return normalized, nil
	}
	return "", fmt.Errorf("invalid artifact type %q; valid values: %s", input,
		"requirements, architecture, development, testing, code_review, security, documentation, deployment, other")
}

func init() {
	artifactCmd.AddCommand(artifactSubmitCmd)
	artifactCmd.AddCommand(artifactListCmd)

	// artifact submit
	artifactSubmitCmd.Flags().String("workflow-id", "", "Workflow the artifact belongs to (required)")
	artifactSubmitCmd.Flags().String("node-id", "", "Workflow node the artifact is submitted for (required)")
	artifactSubmitCmd.Flags().String("type", "", "Artifact type (requirements, architecture, development, testing, code_review, security, documentation, deployment, other)")
	artifactSubmitCmd.Flags().String("name", "", "Artifact name/title (e.g. requirement.md)")
	artifactSubmitCmd.Flags().String("file", "", "Path to the artifact file to submit")
	artifactSubmitCmd.Flags().Bool("allow-external-file", false, "Allow --file to read a path outside the current working directory. Off by default so a stale file from another run/environment can't be picked up (MUL-4252).")
	artifactSubmitCmd.Flags().String("output", "json", "Output format: table or json")

	// artifact list
	artifactListCmd.Flags().String("workflow-id", "", "Filter artifacts by workflow")
	artifactListCmd.Flags().String("type", "", "Filter artifacts by type")
	artifactListCmd.Flags().String("status", "", "Filter artifacts by status (draft, submitted, approved, rejected, superseded)")
	artifactListCmd.Flags().String("output", "table", "Output format: table or json")
}

// ---------------------------------------------------------------------------
// Artifact commands
// ---------------------------------------------------------------------------

func runArtifactSubmit(cmd *cobra.Command, _ []string) error {
	workflowID, _ := cmd.Flags().GetString("workflow-id")
	if workflowID == "" {
		return fmt.Errorf("--workflow-id is required")
	}
	nodeID, _ := cmd.Flags().GetString("node-id")
	if nodeID == "" {
		return fmt.Errorf("--node-id is required (the workflow node this artifact is submitted for)")
	}
	artifactType, _ := cmd.Flags().GetString("type")
	if artifactType == "" {
		return fmt.Errorf("--type is required")
	}
	normalizedType, err := normalizeArtifactType(artifactType)
	if err != nil {
		return err
	}
	title, _ := cmd.Flags().GetString("name")
	if title == "" {
		return fmt.Errorf("--name is required")
	}
	filePath, _ := cmd.Flags().GetString("file")
	if filePath == "" {
		return fmt.Errorf("--file is required")
	}
	if isHTTPURL(filePath) {
		return fmt.Errorf("--file accepts a local file path, not a URL: %s", filePath)
	}
	if err := ensureFileFlagWithinWorkdir(cmd, "file", "file", filePath); err != nil {
		return err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file %s: %w", filePath, err)
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), cli.AtLeastAPITimeout(60*time.Second))
	defer cancel()

	contentType, inline := artifactContentType(filePath, data)

	body := map[string]any{
		"workflow_id":  workflowID,
		"node_id":      nodeID,
		"type":         normalizedType,
		"title":        title,
		"content_type": contentType,
	}
	if inline {
		body["content"] = string(data)
	} else {
		// Binary or large file: upload first, reference the attachment id.
		attID, _, err := client.UploadFileWithURL(ctx, data, filepath.Base(filePath))
		if err != nil {
			return fmt.Errorf("upload artifact file: %w", err)
		}
		body["file_attachment_id"] = attID
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/artifacts", body, &result); err != nil {
		return fmt.Errorf("submit artifact: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "TITLE", "TYPE", "STATUS", "VERSION"}
		rows := [][]string{{
			strVal(result, "id"),
			strVal(result, "title"),
			strVal(result, "type"),
			strVal(result, "status"),
			fmt.Sprintf("%v", asInt(result["version"])),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, result)
}

// maxInlineArtifactBytes caps the text that may be inlined into the artifact
// JSON body by the CLI. Files larger than this are routed through the
// attachment upload channel instead (V-03 security audit — a huge inline text
// file would otherwise balloon the request body into a DoS vector).
const maxInlineArtifactBytes = 5 << 20 // 5 MB

// artifactContentType decides how a --file payload travels to the server.
// Text-ish files are inlined into the artifact `content` field with a content
// type inferred from the extension; everything else (binary formats, or files
// too large to inline safely) is uploaded as an attachment and referenced by
// file_attachment_id. Mirrors the CLO-175 §6.3 pending-question-7 resolution
// from architecture.md §4.4.
func artifactContentType(filename string, data []byte) (contentType string, inline bool) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".md", ".markdown":
		return inlineOrFile("markdown", data)
	case ".json":
		return inlineOrFile("json", data)
	case ".txt", ".text", ".yaml", ".yml", ".toml", ".ini", ".cfg", ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".java", ".c", ".cpp", ".h", ".sql", ".sh", ".bat", ".ps1", ".html", ".css", ".xml", ".csv":
		return inlineOrFile("text", data)
	case ".pdf", ".zip", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx":
		return "file", false
	}
	// Unknown extension: treat NUL-containing content as binary, otherwise
	// inline as text (subject to the size threshold).
	if strings.IndexByte(string(data), 0) >= 0 {
		return "file", false
	}
	return inlineOrFile("text", data)
}

// inlineOrFile enforces the inline size threshold: text-like payloads larger
// than maxInlineArtifactBytes fall back to the attachment upload channel.
func inlineOrFile(contentType string, data []byte) (string, bool) {
	if len(data) > maxInlineArtifactBytes {
		return "file", false
	}
	return contentType, true
}

func runArtifactList(cmd *cobra.Command, _ []string) error {
	workflowID, _ := cmd.Flags().GetString("workflow-id")
	if workflowID == "" {
		return fmt.Errorf("--workflow-id is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	params := url.Values{}
	params.Set("workflow_id", workflowID)
	if client.WorkspaceID != "" {
		params.Set("workspace_id", client.WorkspaceID)
	}
	if v, _ := cmd.Flags().GetString("type"); v != "" {
		normalized, err := normalizeArtifactType(v)
		if err != nil {
			return err
		}
		params.Set("type", normalized)
	}
	if v, _ := cmd.Flags().GetString("status"); v != "" {
		params.Set("status", v)
	}

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/artifacts?"+params.Encode(), &result); err != nil {
		return fmt.Errorf("list artifacts: %w", err)
	}

	itemsRaw, _ := result["items"].([]any)
	items := make([]map[string]any, 0, len(itemsRaw))
	for _, raw := range itemsRaw {
		if a, ok := raw.(map[string]any); ok {
			items = append(items, a)
		}
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		wrapped := map[string]any{
			"items": items,
			"total": result["total"],
		}
		return cli.PrintJSON(os.Stdout, wrapped)
	}

	cli.PrintTable(os.Stdout, []string{"TITLE", "TYPE", "STATUS", "VERSION", "AUTHOR"}, artifactRows(items))
	return nil
}
