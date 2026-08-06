package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// multica issue documents submit — registers an issue-flow intermediate
// document (CLO-278). This is the write channel that backs the "Issue
// Documents" page: each stage agent submits the document it produced for an
// issue, and the page lists / versions them for developers.

var issueDocumentsCmd = &cobra.Command{
	Use:   "documents",
	Short: "Manage issue-flow documents",
}

var issueDocumentsSubmitCmd = &cobra.Command{
	Use:   "submit <issue-id>",
	Short: "Submit a new version of an issue-flow document",
	Long: `Submit a new version of an issue-flow document for an issue.

The document is registered under the (issue, type) family; any previous
version of the same type is marked 'superseded' and version history is kept.

Content is inline for markdown/json/text documents (--content, --content-file,
or --content-stdin) or an uploaded attachment id for file documents
(--file-attachment-id with --content-type file).`,
	Args: exactArgs(1),
	RunE: runIssueDocumentsSubmit,
}

func init() {
	issueDocumentsSubmitCmd.Flags().String("type", "", "Document type: requirements, architecture, development, testing, code_review, security, documentation, deployment, other")
	issueDocumentsSubmitCmd.Flags().String("title", "", "Document title (e.g. requirement.md)")
	issueDocumentsSubmitCmd.Flags().String("content", "", "Inline document content")
	issueDocumentsSubmitCmd.Flags().String("content-file", "", "Read document content from a UTF-8 file (preserves multi-line content verbatim; use this on Windows when stdin piping mangles non-ASCII bytes)")
	issueDocumentsSubmitCmd.Flags().Bool("content-stdin", false, "Read document content from stdin")
	issueDocumentsSubmitCmd.Flags().String("content-type", "markdown", "Content type: markdown, json, text, file")
	issueDocumentsSubmitCmd.Flags().String("file-attachment-id", "", "Attachment id (from multica attachment upload) for file-type documents")
	issueDocumentsSubmitCmd.Flags().String("status", "submitted", "Review status: draft or submitted")
	issueDocumentsSubmitCmd.Flags().String("output", "json", "Output format: json or table")
	issueDocumentsSubmitCmd.Flags().Bool("allow-external-file", false, "Allow --content-file to read a path outside the current working directory")

	issueDocumentsCmd.AddCommand(issueDocumentsSubmitCmd)
	issueCmd.AddCommand(issueDocumentsCmd)
}

func runIssueDocumentsSubmit(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	docType, _ := cmd.Flags().GetString("type")
	title, _ := cmd.Flags().GetString("title")
	docType = strings.TrimSpace(docType)
	title = strings.TrimSpace(title)
	if docType == "" {
		return fmt.Errorf("--type is required")
	}
	if title == "" {
		return fmt.Errorf("--title is required")
	}

	content, ok, err := resolveTextFlag(cmd, "content")
	if err != nil {
		return err
	}
	contentType, _ := cmd.Flags().GetString("content-type")
	fileAttachmentID, _ := cmd.Flags().GetString("file-attachment-id")
	status, _ := cmd.Flags().GetString("status")

	body := map[string]any{
		"issue_id":     issueRef.ID,
		"type":         docType,
		"title":        title,
		"content_type": contentType,
		"status":       status,
	}
	switch contentType {
	case "file":
		if strings.TrimSpace(fileAttachmentID) == "" {
			return fmt.Errorf("--file-attachment-id is required for file documents")
		}
		body["file_attachment_id"] = fileAttachmentID
	case "markdown", "json", "text":
		if !ok || strings.TrimSpace(content) == "" {
			return fmt.Errorf("--content, --content-file, or --content-stdin is required for %s documents", contentType)
		}
		body["content"] = content
	default:
		return fmt.Errorf("invalid --content-type %q", contentType)
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issue-documents", body, &result); err != nil {
		return fmt.Errorf("submit issue document: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	version, _ := result["version"].(float64)
	docTypeOut, _ := result["type"].(string)
	fmt.Fprintf(os.Stdout, "Submitted %s v%d for issue %s\n", docTypeOut, int(version), result["issue_identifier"])
	return nil
}
