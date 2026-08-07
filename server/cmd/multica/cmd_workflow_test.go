package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const testWorkflowUUID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
const testNodeUUID = "11111111-2222-3333-4444-555555555555"
const workflowTestIssueUUID = "99999999-8888-7777-6666-555555555555"

// setWorkflowTestEnv wires the httptest server like setCLITestServerEnv but
// uses a task-scoped mat_ token. In this daemon-managed environment the
// process inherits MULTICA_AGENT_ID / MULTICA_TASK_ID / MULTICA_DAEMON_PORT
// plus a daemon task-context marker in the workdir, so newAPIClient demands a
// mat_ token; the httptest server ignores the Authorization header value.
func setWorkflowTestEnv(t *testing.T, serverURL string) {
	t.Helper()
	setCLITestServerEnv(t, serverURL)
	t.Setenv("MULTICA_TOKEN", "mat_0000000000000000000000000000000000000000")
}

func newWorkflowCreateTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "create"}
	cmd.Flags().String("name", "", "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().String("issue-id", "", "")
	cmd.Flags().String("template", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newWorkflowGetTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "get"}
	cmd.Flags().String("output", "json", "")
	return cmd
}

// TestRunWorkflowCreateSendsExpectedRequest verifies the create command maps
// --name / --issue-id / --description / --template onto the backend payload
// (source_issue_id resolved from the issue key, template_key passthrough).
func TestRunWorkflowCreateSendsExpectedRequest(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/issues/CLO-123":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         workflowTestIssueUUID,
				"identifier": "CLO-123",
				"title":      "实现学生管理系统 CRUD 功能",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/workflows":
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":            testWorkflowUUID,
				"name":          "学生管理系统开发流程",
				"status":        "todo",
				"current_stage": 1,
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	setWorkflowTestEnv(t, srv.URL)

	cmd := newWorkflowCreateTestCmd()
	_ = cmd.Flags().Set("name", "学生管理系统开发流程")
	_ = cmd.Flags().Set("issue-id", "CLO-123")
	_ = cmd.Flags().Set("description", "完成学生管理系统需求分析、开发、测试")
	_ = cmd.Flags().Set("template", "software_rd")

	out, err := captureStdout(t, func() error { return runWorkflowCreate(cmd, nil) })
	if err != nil {
		t.Fatalf("runWorkflowCreate: %v", err)
	}
	if gotBody["name"] != "学生管理系统开发流程" {
		t.Errorf("body name = %v", gotBody["name"])
	}
	if gotBody["source_issue_id"] != workflowTestIssueUUID {
		t.Errorf("source_issue_id = %v, want %s", gotBody["source_issue_id"], workflowTestIssueUUID)
	}
	if gotBody["description"] != "完成学生管理系统需求分析、开发、测试" {
		t.Errorf("body description = %v", gotBody["description"])
	}
	if gotBody["template_key"] != "software_rd" {
		t.Errorf("template_key = %v, want software_rd", gotBody["template_key"])
	}
	if !strings.Contains(out, testWorkflowUUID) {
		t.Errorf("stdout missing workflow id: %q", out)
	}
}

func TestRunWorkflowCreateRequiresNameAndIssue(t *testing.T) {
	setWorkflowTestEnv(t, "http://unused")

	cmd := newWorkflowCreateTestCmd()
	if err := runWorkflowCreate(cmd, nil); err == nil {
		t.Fatal("expected error when --name missing")
	}

	cmd2 := newWorkflowCreateTestCmd()
	_ = cmd2.Flags().Set("name", "X")
	if err := runWorkflowCreate(cmd2, nil); err == nil {
		t.Fatal("expected error when --issue-id missing")
	}
}

// TestRunWorkflowGetDerivesCurrentState verifies the get command combines the
// workflow detail with its artifacts and derives the current node/agent/task.
func TestRunWorkflowGetDerivesCurrentState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/workflows/"+testWorkflowUUID:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":              testWorkflowUUID,
				"name":            "学生管理系统开发流程",
				"status":          "in_progress",
				"current_stage":   1,
				"source_issue_id": workflowTestIssueUUID,
				"stages": []any{
					map[string]any{
						"stage": 1,
						"name":  "需求分析",
						"nodes": []any{
							map[string]any{
								"id":            testNodeUUID,
								"stage":         1,
								"type":          "requirements",
								"name":          "需求分析",
								"status":        "in_progress",
								"issue_id":      workflowTestIssueUUID,
								"assignee_type": "agent",
								"assignee_id":   "22222222-2222-2222-2222-222222222222",
							},
						},
					},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/artifacts":
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
		case r.Method == http.MethodGet && r.URL.Path == "/api/agents":
			_ = json.NewEncoder(w).Encode([]any{
				map[string]any{"id": "22222222-2222-2222-2222-222222222222", "name": "需求分析智能体"},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	setWorkflowTestEnv(t, srv.URL)

	cmd := newWorkflowGetTestCmd()
	out, err := captureStdout(t, func() error { return runWorkflowGet(cmd, []string{testWorkflowUUID}) })
	if err != nil {
		t.Fatalf("runWorkflowGet: %v", err)
	}
	for _, want := range []string{
		`"current_node": "需求分析"`,
		`"current_task": "` + workflowTestIssueUUID + `"`,
		`"status": "in_progress"`,
		`"requirement.md"`,
		`"submitted"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout missing %q:\n%s", want, out)
		}
	}
}

// TestRunWorkflowGetDegradesWhenArtifactsFail pins the FR-2 tolerance: the
// workflow command must still succeed (and mark artifacts empty) when the
// artifact list call fails.
func TestRunWorkflowGetDegradesWhenArtifactsFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/workflows/"+testWorkflowUUID:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":            testWorkflowUUID,
				"name":          "wf",
				"status":        "todo",
				"current_stage": 1,
				"stages":        []any{},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/artifacts":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	setWorkflowTestEnv(t, srv.URL)

	cmd := newWorkflowGetTestCmd()
	out, err := captureStdout(t, func() error { return runWorkflowGet(cmd, []string{testWorkflowUUID}) })
	if err != nil {
		t.Fatalf("runWorkflowGet should tolerate artifacts failure, got: %v", err)
	}
	if !strings.Contains(out, `"artifacts": []`) {
		t.Errorf("artifacts should degrade to empty list, got: %q", out)
	}
}
