package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func cleanupIssueDocuments(t *testing.T, issueID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `DELETE FROM issue_document WHERE issue_id = $1`, issueID); err != nil {
		t.Fatalf("cleanup issue_document rows: %v", err)
	}
}

// newIssueDocumentFixture creates a test issue and cleans up both the issue
// and its issue_document rows on test end.
func newIssueDocumentFixture(t *testing.T) string {
	t.Helper()
	issueID := createTestIssue(t, "Issue Documents test", "todo", "none")
	t.Cleanup(func() {
		cleanupIssueDocuments(t, issueID)
		deleteTestIssue(t, issueID)
	})
	return issueID
}

// TestCreateAndListIssueDocuments covers the happy path: submitting a new
// document version, listing it with the enriched issue_identifier / author
// fields, and reading the detail back with the inline content.
func TestCreateAndListIssueDocuments(t *testing.T) {
	issueID := newIssueDocumentFixture(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issue-documents", map[string]any{
		"issue_id": issueID,
		"type":     "requirements",
		"title":    "requirement.md",
		"content":  "# Requirements\n\nDraft requirement document.",
	})
	testHandler.CreateIssueDocument(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssueDocument: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created IssueDocumentDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created document: %v", err)
	}
	if created.Version != 1 {
		t.Fatalf("first version: expected 1, got %d", created.Version)
	}
	if created.IssueIdentifier == "" {
		t.Fatalf("expected issue_identifier to be populated, got empty")
	}
	if created.AuthorName == "" {
		t.Fatalf("expected author_name to be populated, got empty")
	}
	if created.Content == nil || *created.Content != "# Requirements\n\nDraft requirement document." {
		t.Fatalf("expected inline content to be returned, got %v", created.Content)
	}

	// List includes the document with the light row shape (no content).
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issue-documents", nil)
	testHandler.ListIssueDocuments(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssueDocuments: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list struct {
		Items []IssueDocumentResponse `json:"items"`
		Total int64                   `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("expected 1 document, got total=%d items=%d", list.Total, len(list.Items))
	}
	if list.Items[0].ID != created.ID {
		t.Fatalf("list item id mismatch: %s vs %s", list.Items[0].ID, created.ID)
	}

	// Detail read.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issue-documents/"+created.ID, nil)
	req = withURLParam(req, "documentId", created.ID)
	testHandler.GetIssueDocument(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetIssueDocument: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var detail IssueDocumentDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.Content == nil || *detail.Content != "# Requirements\n\nDraft requirement document." {
		t.Fatalf("expected content in detail, got %v", detail.Content)
	}
}

// TestIssueDocumentVersioning verifies that submitting the same (issue, type)
// twice bumps the version and marks the previous version superseded, and that
// the version history endpoint lists both.
func TestIssueDocumentVersioning(t *testing.T) {
	issueID := newIssueDocumentFixture(t)

	submit := func(title string) IssueDocumentDetailResponse {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issue-documents", map[string]any{
			"issue_id": issueID,
			"type":     "architecture",
			"title":    title,
			"content":  "# " + title,
		})
		testHandler.CreateIssueDocument(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateIssueDocument(%s): expected 201, got %d: %s", title, w.Code, w.Body.String())
		}
		var doc IssueDocumentDetailResponse
		if err := json.NewDecoder(w.Body).Decode(&doc); err != nil {
			t.Fatalf("decode created document: %v", err)
		}
		return doc
	}

	first := submit("architecture.md")
	if first.Version != 1 {
		t.Fatalf("expected version 1, got %d", first.Version)
	}
	second := submit("architecture.md")
	if second.Version != 2 {
		t.Fatalf("expected version 2, got %d", second.Version)
	}

	// Versions endpoint returns newest-first with the old version superseded.
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issue-documents/"+second.ID+"/versions", nil)
	req = withURLParam(req, "documentId", second.ID)
	testHandler.ListIssueDocumentVersions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssueDocumentVersions: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var versions struct {
		Items []IssueDocumentVersionResponse `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&versions); err != nil {
		t.Fatalf("decode versions: %v", err)
	}
	if len(versions.Items) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions.Items))
	}
	if versions.Items[0].Version != 2 || versions.Items[0].Status != "submitted" {
		t.Fatalf("expected newest version first (v2, submitted), got v%d/%s", versions.Items[0].Version, versions.Items[0].Status)
	}
	if versions.Items[1].Version != 1 || versions.Items[1].Status != "superseded" {
		t.Fatalf("expected v1 superseded, got v%d/%s", versions.Items[1].Version, versions.Items[1].Status)
	}
}

// TestIssueDocumentFilters exercises the type / status / q filters on the list
// endpoint.
func TestIssueDocumentFilters(t *testing.T) {
	issueID := newIssueDocumentFixture(t)

	submit := func(docType, title string) {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issue-documents", map[string]any{
			"issue_id": issueID,
			"type":     docType,
			"title":    title,
			"content":  "# " + title,
		})
		testHandler.CreateIssueDocument(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateIssueDocument(%s): expected 201, got %d: %s", title, w.Code, w.Body.String())
		}
	}

	submit("testing", "test_report.md")
	submit("deployment", "deployment_report.md")

	// type filter
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issue-documents?type=deployment", nil)
	testHandler.ListIssueDocuments(w, req)
	var list struct {
		Items []IssueDocumentResponse `json:"items"`
		Total int64                   `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
		t.Fatalf("decode type-filtered list: %v", err)
	}
	if list.Total != 1 || list.Items[0].Type != "deployment" {
		t.Fatalf("type filter: expected 1 deployment doc, got total=%d", list.Total)
	}

	// q search on title
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issue-documents?q=test_report", nil)
	testHandler.ListIssueDocuments(w, req)
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
		t.Fatalf("decode q-filtered list: %v", err)
	}
	if list.Total != 1 || list.Items[0].Title != "test_report.md" {
		t.Fatalf("q filter: expected test_report.md, got total=%d", list.Total)
	}

	// invalid type → 400
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issue-documents?type=bogus", nil)
	testHandler.ListIssueDocuments(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid type: expected 400, got %d", w.Code)
	}
}

// TestGetIssueDocumentCrossWorkspace ensures a document in another workspace
// is not visible (404).
func TestGetIssueDocumentCrossWorkspace(t *testing.T) {
	issueID := newIssueDocumentFixture(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issue-documents", map[string]any{
		"issue_id": issueID,
		"type":     "security",
		"title":    "security_audit.md",
		"content":  "# Security audit",
	})
	testHandler.CreateIssueDocument(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssueDocument: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created IssueDocumentDetailResponse
	json.NewDecoder(w.Body).Decode(&created)

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issue-documents/"+created.ID, nil)
	req.Header.Set("X-Workspace-ID", "00000000-0000-0000-0000-000000000000")
	req = withURLParam(req, "documentId", created.ID)
	testHandler.GetIssueDocument(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GetIssueDocument cross-workspace: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateIssueDocumentValidation covers required fields and enum validation.
func TestCreateIssueDocumentValidation(t *testing.T) {
	issueID := newIssueDocumentFixture(t)

	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing issue_id", map[string]any{"type": "requirements", "title": "x.md"}},
		{"missing title", map[string]any{"issue_id": issueID, "type": "requirements"}},
		{"invalid type", map[string]any{"issue_id": issueID, "type": "bogus", "title": "x.md"}},
		{"invalid status", map[string]any{"issue_id": issueID, "type": "requirements", "title": "x.md", "status": "bogus"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/issue-documents", tc.body)
			testHandler.CreateIssueDocument(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}
