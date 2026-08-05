package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func newArtifactSubmitTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "submit"}
	cmd.Flags().String("workflow-id", "", "")
	cmd.Flags().String("node-id", "", "")
	cmd.Flags().String("type", "", "")
	cmd.Flags().String("name", "", "")
	cmd.Flags().String("file", "", "")
	cmd.Flags().Bool("allow-external-file", false, "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newArtifactListTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "list"}
	cmd.Flags().String("workflow-id", "", "")
	cmd.Flags().String("type", "", "")
	cmd.Flags().String("status", "", "")
	cmd.Flags().String("output", "table", "")
	return cmd
}

// setArtifactTestEnv wires the httptest server like setCLITestServerEnv but
// uses a task-scoped mat_ token. In this daemon-managed environment the
// process inherits MULTICA_AGENT_ID / MULTICA_TASK_ID / MULTICA_DAEMON_PORT
// plus a daemon task-context marker in the workdir, so newAPIClient demands a
// mat_ token; the httptest server ignores the Authorization header value.
func setArtifactTestEnv(t *testing.T, serverURL string) {
	t.Helper()
	setCLITestServerEnv(t, serverURL)
	t.Setenv("MULTICA_TOKEN", "mat_0000000000000000000000000000000000000000")
}

// TestRunArtifactSubmitSendsExpectedRequest verifies the submit command maps
// --workflow-id / --node-id / --type (alias-normalized) / --name / --file onto
// the backend payload: title from --name, content inlined for markdown.
func TestRunArtifactSubmitSendsExpectedRequest(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/artifacts" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "cccccccc-dddd-eeee-ffff-000000000000",
			"workflow_id": testWorkflowUUID,
			"node_id":     testNodeUUID,
			"type":        "requirements",
			"title":       "requirement.md",
			"status":      "submitted",
			"version":     1,
		})
	}))
	defer srv.Close()
	setArtifactTestEnv(t, srv.URL)

	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "requirement.md")
	if err := os.WriteFile(filePath, []byte("# 需求分析\n\n实现学生管理系统"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cmd := newArtifactSubmitTestCmd()
	_ = cmd.Flags().Set("workflow-id", testWorkflowUUID)
	_ = cmd.Flags().Set("node-id", testNodeUUID)
	_ = cmd.Flags().Set("type", "requirement") // alias → requirements
	_ = cmd.Flags().Set("name", "requirement.md")
	_ = cmd.Flags().Set("file", filePath)
	_ = cmd.Flags().Set("allow-external-file", "true")

	out, err := captureStdout(t, func() error { return runArtifactSubmit(cmd, nil) })
	if err != nil {
		t.Fatalf("runArtifactSubmit: %v", err)
	}
	if gotBody["workflow_id"] != testWorkflowUUID {
		t.Errorf("workflow_id = %v", gotBody["workflow_id"])
	}
	if gotBody["node_id"] != testNodeUUID {
		t.Errorf("node_id = %v", gotBody["node_id"])
	}
	if gotBody["type"] != "requirements" {
		t.Errorf("type = %v, want normalized requirements", gotBody["type"])
	}
	if gotBody["title"] != "requirement.md" {
		t.Errorf("title = %v", gotBody["title"])
	}
	if gotBody["content"] != "# 需求分析\n\n实现学生管理系统" {
		t.Errorf("content = %q", gotBody["content"])
	}
	if gotBody["content_type"] != "markdown" {
		t.Errorf("content_type = %v, want markdown", gotBody["content_type"])
	}
	if !strings.Contains(out, `"status": "submitted"`) {
		t.Errorf("stdout missing status: %q", out)
	}
}

// TestRunArtifactSubmitRejectsBinaryInlineWhenUploadFails pins the file-style
// path: a non-text file must go through /api/upload-file before the artifact
// payload references file_attachment_id.
func TestRunArtifactSubmitUploadsBinaryFile(t *testing.T) {
	var gotAttachmentID string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/upload-file":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatalf("parse multipart: %v", err)
			}
			if f, _, err := r.FormFile("file"); err == nil {
				_ = f.Close()
			} else {
				t.Fatalf("missing file part: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":  "att-upload-1",
				"url": "https://cdn.example/att-upload-1",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/artifacts":
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			gotAttachmentID, _ = gotBody["file_attachment_id"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "cccccccc-dddd-eeee-ffff-000000000000",
				"type":    "development",
				"title":   "binary.bin",
				"status":  "submitted",
				"version": 1,
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	setArtifactTestEnv(t, srv.URL)

	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "binary.bin")
	if err := os.WriteFile(filePath, []byte{0x00, 0x01, 0x02, 0x00, 0xff}, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cmd := newArtifactSubmitTestCmd()
	_ = cmd.Flags().Set("workflow-id", testWorkflowUUID)
	_ = cmd.Flags().Set("node-id", testNodeUUID)
	_ = cmd.Flags().Set("type", "development")
	_ = cmd.Flags().Set("name", "binary.bin")
	_ = cmd.Flags().Set("file", filePath)
	_ = cmd.Flags().Set("allow-external-file", "true")

	if _, err := captureStdout(t, func() error { return runArtifactSubmit(cmd, nil) }); err != nil {
		t.Fatalf("runArtifactSubmit: %v", err)
	}
	if gotAttachmentID != "att-upload-1" {
		t.Errorf("file_attachment_id = %q, want att-upload-1", gotAttachmentID)
	}
	if gotBody["content_type"] != "file" {
		t.Errorf("content_type = %v, want file", gotBody["content_type"])
	}
}

func TestRunArtifactSubmitValidation(t *testing.T) {
	setArtifactTestEnv(t, "http://unused")
	cmd := newArtifactSubmitTestCmd()
	if err := runArtifactSubmit(cmd, nil); err == nil {
		t.Fatal("expected error when required flags missing")
	}

	cmd2 := newArtifactSubmitTestCmd()
	_ = cmd2.Flags().Set("workflow-id", "w")
	_ = cmd2.Flags().Set("node-id", "n")
	_ = cmd2.Flags().Set("type", "bogus-type")
	_ = cmd2.Flags().Set("name", "x.md")
	_ = cmd2.Flags().Set("file", "x.md")
	if err := runArtifactSubmit(cmd2, nil); err == nil || !strings.Contains(err.Error(), "invalid artifact type") {
		t.Fatalf("expected invalid type error, got: %v", err)
	}
}

func TestNormalizeArtifactType(t *testing.T) {
	cases := map[string]string{
		"requirement":  "requirements",
		"requirements": "requirements",
		"test_report":  "testing",
		"review":       "code_review",
		"deploy":       "deployment",
		"doc":          "documentation",
		"code":         "development",
	}
	for in, want := range cases {
		got, err := normalizeArtifactType(in)
		if err != nil {
			t.Errorf("normalizeArtifactType(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("normalizeArtifactType(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := normalizeArtifactType("bogus"); err == nil {
		t.Error("expected error for unknown type")
	}
}

func TestArtifactContentType(t *testing.T) {
	md, inline := artifactContentType("a.md", []byte("# hi"))
	if md != "markdown" || !inline {
		t.Errorf("md → %q inline=%v", md, inline)
	}
	js, inline := artifactContentType("a.json", []byte(`{}`))
	if js != "json" || !inline {
		t.Errorf("json → %q inline=%v", js, inline)
	}
	_, inline = artifactContentType("a.pdf", []byte("%PDF"))
	if inline {
		t.Error("pdf should not be inline")
	}
	_, inline = artifactContentType("unknown.ext", []byte{0x00, 0x01})
	if inline {
		t.Error("NUL-containing unknown ext should not be inline")
	}
	txt, inline := artifactContentType("a.txt", []byte("plain"))
	if txt != "text" || !inline {
		t.Errorf("txt → %q inline=%v", txt, inline)
	}
}

// TestArtifactContentTypeOverSizeThresholdRoutesToFile pins the V-03 fix: a
// text-ish file larger than maxInlineArtifactBytes must NOT be inlined into the
// JSON body (which would balloon the request into a DoS vector) — it falls back
// to the attachment upload channel (content_type "file", not inline).
func TestArtifactContentTypeOverSizeThresholdRoutesToFile(t *testing.T) {
	big := make([]byte, maxInlineArtifactBytes+1)
	for i := range big {
		big[i] = 'a'
	}
	md, inline := artifactContentType("a.md", big)
	if inline || md != "file" {
		t.Errorf("large md → %q inline=%v, want file/inline=false", md, inline)
	}
	txt, inline := artifactContentType("a.txt", big)
	if inline || txt != "file" {
		t.Errorf("large txt → %q inline=%v, want file/inline=false", txt, inline)
	}
	unknown, inline := artifactContentType("unknown.ext", big)
	if inline || unknown != "file" {
		t.Errorf("large unknown ext → %q inline=%v, want file/inline=false", unknown, inline)
	}
}

// TestRunArtifactSubmitUploadsOversizedTextFile pins the V-03 CLI path: an
// oversized text file is routed through /api/upload-file (attachment) instead
// of being inlined as `content`, so the JSON request body stays bounded.
func TestRunArtifactSubmitUploadsOversizedTextFile(t *testing.T) {
	var gotAttachmentID string
	var gotBody map[string]any
	var uploadedSize int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/upload-file":
			if err := r.ParseMultipartForm(maxInlineArtifactBytes + (1 << 20)); err != nil {
				t.Fatalf("parse multipart: %v", err)
			}
			if f, h, err := r.FormFile("file"); err == nil {
				uploadedSize = h.Size
				_ = f.Close()
			} else {
				t.Fatalf("missing file part: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":  "att-large-text",
				"url": "https://cdn.example/att-large-text",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/artifacts":
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			gotAttachmentID, _ = gotBody["file_attachment_id"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "cccccccc-dddd-eeee-ffff-000000000000",
				"type":    "requirements",
				"title":   "big.md",
				"status":  "submitted",
				"version": 1,
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	setArtifactTestEnv(t, srv.URL)

	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "big.md")
	big := make([]byte, maxInlineArtifactBytes+1)
	for i := range big {
		big[i] = 'a'
	}
	if err := os.WriteFile(filePath, big, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cmd := newArtifactSubmitTestCmd()
	_ = cmd.Flags().Set("workflow-id", testWorkflowUUID)
	_ = cmd.Flags().Set("node-id", testNodeUUID)
	_ = cmd.Flags().Set("type", "requirements")
	_ = cmd.Flags().Set("name", "big.md")
	_ = cmd.Flags().Set("file", filePath)
	_ = cmd.Flags().Set("allow-external-file", "true")

	if _, err := captureStdout(t, func() error { return runArtifactSubmit(cmd, nil) }); err != nil {
		t.Fatalf("runArtifactSubmit: %v", err)
	}
	if gotAttachmentID != "att-large-text" {
		t.Errorf("file_attachment_id = %q, want att-large-text (must upload, not inline)", gotAttachmentID)
	}
	if uploadedSize != int64(len(big)) {
		t.Errorf("uploaded size = %d, want %d", uploadedSize, len(big))
	}
	if _, hasContent := gotBody["content"]; hasContent {
		t.Error("oversized text must not be inlined into content")
	}
	if gotBody["content_type"] != "file" {
		t.Errorf("content_type = %v, want file", gotBody["content_type"])
	}
}

func TestRunArtifactListUnwrapsItems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/artifacts" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("workflow_id") != testWorkflowUUID {
			t.Fatalf("workflow_id query = %q", r.URL.Query().Get("workflow_id"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []any{
				map[string]any{
					"id":      "cccccccc-dddd-eeee-ffff-000000000000",
					"title":   "requirement.md",
					"type":    "requirements",
					"status":  "submitted",
					"version": 1,
				},
			},
			"total": 1,
		})
	}))
	defer srv.Close()
	setArtifactTestEnv(t, srv.URL)

	cmd := newArtifactListTestCmd()
	_ = cmd.Flags().Set("workflow-id", testWorkflowUUID)
	_ = cmd.Flags().Set("output", "json")

	out, err := captureStdout(t, func() error { return runArtifactList(cmd, nil) })
	if err != nil {
		t.Fatalf("runArtifactList: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("stdout not JSON: %v\n%s", err, out)
	}
	items, ok := parsed["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v, want 1 entry", parsed["items"])
	}
	if parsed["total"].(float64) != 1 {
		t.Errorf("total = %v, want 1", parsed["total"])
	}
}
