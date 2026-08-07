package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// TestCreateIssueDocumentSanitizesNullBytes verifies NUL bytes in the content
// are stripped before insert (CLO-283 R2, GH #5388 precedent), so the PG TEXT
// column never rejects the row with an opaque 500.
func TestCreateIssueDocumentSanitizesNullBytes(t *testing.T) {
	issueID := newIssueDocumentFixture(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issue-documents", map[string]any{
		"issue_id": issueID,
		"type":     "requirements",
		"title":    "requirement.md",
		"content":  "# Head\u0000ing\n\nNUL \u0000 byte inside.",
	})
	testHandler.CreateIssueDocument(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssueDocument: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created IssueDocumentDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created document: %v", err)
	}
	if created.Content == nil {
		t.Fatalf("expected content in response")
	}
	if strings.Contains(*created.Content, "\x00") {
		t.Fatalf("NUL byte leaked into stored content: %q", *created.Content)
	}
	if !strings.Contains(*created.Content, "NUL  byte inside.") {
		t.Fatalf("expected surrounding content preserved, got %q", *created.Content)
	}
}

// TestIssueDocumentServerSort verifies the list endpoint orders server-side
// (CLO-283 R1): a non-default sort is applied in SQL before LIMIT/OFFSET so
// pagination stays stable across pages.
func TestIssueDocumentServerSort(t *testing.T) {
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

	submit("architecture", "zzz_architecture.md")
	submit("deployment", "aaa_deployment.md")

	list := func(query string) []IssueDocumentResponse {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("GET", "/api/issue-documents"+query, nil)
		testHandler.ListIssueDocuments(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("ListIssueDocuments%s: expected 200, got %d: %s", query, w.Code, w.Body.String())
		}
		var body struct {
			Items []IssueDocumentResponse `json:"items"`
		}
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		return body.Items
	}

	asc := list("?sort=title&order=asc")
	if len(asc) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(asc))
	}
	if asc[0].Title != "aaa_deployment.md" || asc[1].Title != "zzz_architecture.md" {
		t.Fatalf("title asc ordering wrong: %s, %s", asc[0].Title, asc[1].Title)
	}

	desc := list("?sort=title&order=desc")
	if desc[0].Title != "zzz_architecture.md" || desc[1].Title != "aaa_deployment.md" {
		t.Fatalf("title desc ordering wrong: %s, %s", desc[0].Title, desc[1].Title)
	}

	// invalid sort value → 400
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issue-documents?sort=injected_column", nil)
	testHandler.ListIssueDocuments(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid sort: expected 400, got %d", w.Code)
	}
}

// TestIssueDocumentQSearchMatchesIdentifier verifies the q search also matches
// the issue's full identifier (prefix + number), so typing "CLO-<n>" hits the
// document (CLO-283 R4).
func TestIssueDocumentQSearchMatchesIdentifier(t *testing.T) {
	setWorkspaceIssuePrefixForTest(t, "CLO")
	issueID := newIssueDocumentFixture(t)

	// Fetch the created issue's number to build the expected identifier.
	var number int
	if err := testPool.QueryRow(context.Background(), `SELECT number FROM issue WHERE id = $1`, issueID).Scan(&number); err != nil {
		t.Fatalf("load issue number: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issue-documents", map[string]any{
		"issue_id": issueID,
		"type":     "testing",
		"title":    "test_report.md",
		"content":  "# Report",
	})
	testHandler.CreateIssueDocument(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssueDocument: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	identifier := fmt.Sprintf("CLO-%d", number)
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issue-documents?q="+identifier, nil)
	testHandler.ListIssueDocuments(w, req)
	var body struct {
		Items []IssueDocumentResponse `json:"items"`
		Total int64                   `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if body.Total != 1 || len(body.Items) != 1 {
		t.Fatalf("q=%s: expected 1 hit, got total=%d items=%d", identifier, body.Total, len(body.Items))
	}
	if body.Items[0].IssueIdentifier != identifier {
		t.Fatalf("expected issue_identifier %s, got %s", identifier, body.Items[0].IssueIdentifier)
	}
}

// TestIssueDocumentVersionConflictNotGeneric500 races concurrent submissions of
// the same (issue, type): MAX(version)+1 can collide, the unique index rejects
// the loser, and the handler must answer 409 (CLO-283 R6) instead of a generic
// 500. With enough concurrent writers at least one collision is virtually
// certain; regardless, no response may be a 500.
func TestIssueDocumentVersionConflictNotGeneric500(t *testing.T) {
	issueID := newIssueDocumentFixture(t)

	const workers = 8
	results := make(chan int, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/issue-documents", map[string]any{
				"issue_id": issueID,
				"type":     "architecture",
				"title":    fmt.Sprintf("architecture_%d.md", n),
				"content":  "# " + fmt.Sprintf("Architecture %d", n),
			})
			testHandler.CreateIssueDocument(w, req)
			results <- w.Code
		}(i)
	}
	wg.Wait()
	close(results)

	created := 0
	conflicts := 0
	for code := range results {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("unexpected status during version race: %d", code)
		}
	}
	if created == 0 {
		t.Fatalf("expected at least one successful create, got none")
	}
	t.Logf("version race: %d created, %d conflict(409)", created, conflicts)
}
