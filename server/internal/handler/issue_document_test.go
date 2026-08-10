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

// TestIssueDocumentGroupedByIssue verifies the grouped-by-issue list mode
// (group=issue, CLO-471): documents are bucketed under their issue, group
// headers carry the issue identifier/title, filters apply before grouping, and
// documents within a group are ordered by the requested sort (default: R&D
// stage order via `type`).
func TestIssueDocumentGroupedByIssue(t *testing.T) {
	issueA := newIssueDocumentFixture(t)
	issueB := newIssueDocumentFixture(t)

	submit := func(issueID, docType, title string) {
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
			t.Fatalf("CreateIssueDocument(%s/%s): expected 201, got %d: %s", issueID, title, w.Code, w.Body.String())
		}
	}

	submit(issueA, "requirements", "aaa_requirements.md")
	submit(issueA, "deployment", "deployment.md")
	submit(issueA, "architecture", "architecture.md")
	submit(issueB, "testing", "test_report.md")

	// Order of issue creation matches the auto-incrementing issue number, so
	// issueA has the lower number and must appear as the first group.
	var prefix string
	if err := testPool.QueryRow(context.Background(), `SELECT issue_prefix FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&prefix); err != nil {
		t.Fatalf("load workspace issue_prefix: %v", err)
	}
	loadNumber := func(issueID string) int {
		t.Helper()
		var n int
		if err := testPool.QueryRow(context.Background(), `SELECT number FROM issue WHERE id = $1`, issueID).Scan(&n); err != nil {
			t.Fatalf("load issue number: %v", err)
		}
		return n
	}
	numA, numB := loadNumber(issueA), loadNumber(issueB)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issue-documents?group=issue", nil)
	testHandler.ListIssueDocuments(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("grouped list: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Groups []IssueDocumentGroupResponse `json:"groups"`
		Total  int                          `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode grouped list: %v", err)
	}
	if body.Total != 4 || len(body.Groups) != 2 {
		t.Fatalf("expected 4 documents in 2 groups, got total=%d groups=%d", body.Total, len(body.Groups))
	}
	first := body.Groups[0]
	if first.IssueIdentifier != fmt.Sprintf("%s-%d", prefix, numA) {
		t.Fatalf("first group identifier: expected %s-%d, got %s", prefix, numA, first.IssueIdentifier)
	}
	if first.Total != 3 || len(first.Items) != 3 {
		t.Fatalf("issue A group: expected 3 documents, got total=%d items=%d", first.Total, len(first.Items))
	}
	// Default within-group sort is the R&D stage order: requirements first.
	if first.Items[0].Type != "requirements" {
		t.Fatalf("issue A first item: expected requirements (stage order), got %s", first.Items[0].Type)
	}
	if first.Items[0].Title != "aaa_requirements.md" {
		t.Fatalf("issue A first item title: expected aaa_requirements.md, got %s", first.Items[0].Title)
	}
	second := body.Groups[1]
	if second.IssueIdentifier != fmt.Sprintf("%s-%d", prefix, numB) || second.Total != 1 {
		t.Fatalf("second group: expected %s-%d with 1 doc, got %s/%d", prefix, numB, second.IssueIdentifier, second.Total)
	}

	// A type filter applies before grouping: only the deployment doc survives.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issue-documents?group=issue&type=deployment", nil)
	testHandler.ListIssueDocuments(w, req)
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode filtered grouped list: %v", err)
	}
	if body.Total != 1 || len(body.Groups) != 1 || body.Groups[0].Items[0].Type != "deployment" {
		t.Fatalf("grouped type filter: expected 1 deployment doc in 1 group, got total=%d groups=%d", body.Total, len(body.Groups))
	}

	// A q filter applies before grouping too.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issue-documents?group=issue&q=test_report", nil)
	testHandler.ListIssueDocuments(w, req)
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode q-filtered grouped list: %v", err)
	}
	if body.Total != 1 || body.Groups[0].Items[0].Title != "test_report.md" {
		t.Fatalf("grouped q filter: expected 1 hit, got total=%d", body.Total)
	}

	// Explicit within-group sort by title asc.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issue-documents?group=issue&sort=title&order=asc", nil)
	testHandler.ListIssueDocuments(w, req)
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode sorted grouped list: %v", err)
	}
	if body.Groups[0].Items[0].Title != "aaa_requirements.md" {
		t.Fatalf("title asc: expected aaa_requirements.md first, got %s", body.Groups[0].Items[0].Title)
	}
	if body.Groups[0].Items[len(body.Groups[0].Items)-1].Title != "deployment.md" {
		t.Fatalf("title asc: expected deployment.md last, got %s", body.Groups[0].Items[len(body.Groups[0].Items)-1].Title)
	}

	// An invalid sort value is rejected in grouped mode too.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issue-documents?group=issue&sort=injected_column", nil)
	testHandler.ListIssueDocuments(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("grouped invalid sort: expected 400, got %d", w.Code)
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

// newIssueDocumentAgentRequest builds a POST /api/issue-documents request that
// authenticates as an agent actor (server-stamped X-Actor-Source=task_token +
// X-Agent-ID, the shape resolveActor trusts as the first-class agent signal).
func newIssueDocumentAgentRequest(agentID string, body map[string]any) *http.Request {
	req := newRequest("POST", "/api/issue-documents", body)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", agentID)
	return req
}

// seedTestAttachment inserts a bare attachment row in the given workspace and
// returns its id. No storage URL is written (the row only feeds the
// GetAttachmentByIDOnly ownership check).
func seedTestAttachment(t *testing.T, workspaceID string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
		VALUES ($1, 'member', $2, 'seed.txt', 'seed://attachment', 'text/plain', 0)
		RETURNING id::text
	`, workspaceID, testUserID).Scan(&id); err != nil {
		t.Fatalf("seed attachment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, id)
	})
	return id
}

// seedForeignWorkspace creates a throwaway workspace (for cross-workspace
// attachment ownership tests) and returns its id.
func seedForeignWorkspace(t *testing.T) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Issue Doc Foreign', 'issue-doc-foreign-' || gen_random_uuid()::text, 'temporary', 'FOR')
		RETURNING id::text
	`).Scan(&id); err != nil {
		t.Fatalf("seed foreign workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, id)
	})
	return id
}

// TestCreateIssueDocument_PlainMemberForbidden locks the CLO-284 S1 write gate:
// the POST /api/issue-documents channel must be reserved for agent identities
// and workspace owner/admin members. A plain member gets 403 even though the
// route sits in the RequireWorkspaceMember group — the page is read-only for
// them and accepting their writes would let any member forge review status or
// overwrite real stage artifacts.
func TestCreateIssueDocument_PlainMemberForbidden(t *testing.T) {
	issueID := newIssueDocumentFixture(t)
	plainMemberID := createPlainMember(t, "issue-doc-plain@multica.test")

	w := httptest.NewRecorder()
	req := newRequestAs(plainMemberID, "POST", "/api/issue-documents", map[string]any{
		"issue_id": issueID,
		"type":     "requirements",
		"title":    "requirement.md",
		"content":  "# Requirements",
	})
	testHandler.CreateIssueDocument(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("plain member create: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// Same gate for a forged `approved` status: a plain member must not be able
	// to fake a review outcome even if the identity gate were bypassed.
	w = httptest.NewRecorder()
	req = newRequestAs(plainMemberID, "POST", "/api/issue-documents", map[string]any{
		"issue_id": issueID,
		"type":     "requirements",
		"title":    "requirement.md",
		"content":  "# Requirements",
		"status":   "approved",
	})
	testHandler.CreateIssueDocument(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("plain member forged approved: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateIssueDocument_AgentIdentityAllowed verifies the primary legitimate
// write path: an agent actor (task-token) may register a document. The
// author_type recorded must be "agent".
func TestCreateIssueDocument_AgentIdentityAllowed(t *testing.T) {
	issueID := newIssueDocumentFixture(t)
	agentID := createHandlerTestAgent(t, "issue-doc-writer-agent", nil)

	w := httptest.NewRecorder()
	req := newIssueDocumentAgentRequest(agentID, map[string]any{
		"issue_id": issueID,
		"type":     "architecture",
		"title":    "architecture.md",
		"content":  "# Architecture",
	})
	testHandler.CreateIssueDocument(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("agent create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created IssueDocumentDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created document: %v", err)
	}
	if created.AuthorType != "agent" || created.AuthorID != agentID {
		t.Fatalf("expected agent author %s, got type=%s id=%s", agentID, created.AuthorType, created.AuthorID)
	}
}

// TestCreateIssueDocument_AgentCannotSetReviewStatus verifies a flow agent may
// register a document in draft/submitted state but must NOT be able to set a
// review status (approved/rejected) — that decision belongs to an owner/admin
// review path (CLO-284 S1).
func TestCreateIssueDocument_AgentCannotSetReviewStatus(t *testing.T) {
	issueID := newIssueDocumentFixture(t)
	agentID := createHandlerTestAgent(t, "issue-doc-review-agent", nil)

	for _, status := range []string{"approved", "rejected"} {
		w := httptest.NewRecorder()
		req := newIssueDocumentAgentRequest(agentID, map[string]any{
			"issue_id": issueID,
			"type":     "security",
			"title":    "security.md",
			"content":  "# Security",
			"status":   status,
		})
		testHandler.CreateIssueDocument(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("agent set %s: expected 403, got %d: %s", status, w.Code, w.Body.String())
		}
	}

	// `superseded` is server-managed and never accepted from a client.
	w := httptest.NewRecorder()
	req := newIssueDocumentAgentRequest(agentID, map[string]any{
		"issue_id": issueID,
		"type":     "security",
		"title":    "security.md",
		"content":  "# Security",
		"status":   "superseded",
	})
	testHandler.CreateIssueDocument(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("agent set superseded: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateIssueDocument_OwnerCanSetReviewStatus verifies the owner/admin path
// may still register a document with an explicit review status (the escaping
// hatch for owners who review directly).
func TestCreateIssueDocument_OwnerCanSetReviewStatus(t *testing.T) {
	issueID := newIssueDocumentFixture(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issue-documents", map[string]any{
		"issue_id": issueID,
		"type":     "security",
		"title":    "security.md",
		"content":  "# Security",
		"status":   "approved",
	})
	testHandler.CreateIssueDocument(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("owner create approved: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created IssueDocumentDetailResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode created document: %v", err)
	}
	if created.Status != "approved" {
		t.Fatalf("expected status approved, got %s", created.Status)
	}
}

// TestCreateIssueDocument_FileAttachmentOwnership locks the CLO-284 S2 gate: a
// file_attachment_id must exist and belong to the current workspace.
func TestCreateIssueDocument_FileAttachmentOwnership(t *testing.T) {
	issueID := newIssueDocumentFixture(t)
	agentID := createHandlerTestAgent(t, "issue-doc-file-agent", nil)

	// Non-existent attachment id → 400.
	w := httptest.NewRecorder()
	req := newIssueDocumentAgentRequest(agentID, map[string]any{
		"issue_id":          issueID,
		"type":              "deployment",
		"title":             "deploy.txt",
		"content_type":      "file",
		"file_attachment_id": "00000000-0000-0000-0000-000000000000",
	})
	testHandler.CreateIssueDocument(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing attachment: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Attachment from another workspace → 400.
	foreignWorkspaceID := seedForeignWorkspace(t)
	otherAttachmentID := seedTestAttachment(t, foreignWorkspaceID)
	w = httptest.NewRecorder()
	req = newIssueDocumentAgentRequest(agentID, map[string]any{
		"issue_id":          issueID,
		"type":              "deployment",
		"title":             "deploy.txt",
		"content_type":      "file",
		"file_attachment_id": otherAttachmentID,
	})
	testHandler.CreateIssueDocument(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("cross-workspace attachment: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Same-workspace attachment → 201.
	ownAttachmentID := seedTestAttachment(t, testWorkspaceID)
	w = httptest.NewRecorder()
	req = newIssueDocumentAgentRequest(agentID, map[string]any{
		"issue_id":          issueID,
		"type":              "deployment",
		"title":             "deploy.txt",
		"content_type":      "file",
		"file_attachment_id": ownAttachmentID,
	})
	testHandler.CreateIssueDocument(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("same-workspace attachment: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateIssueDocument_TitleLengthCaps verifies the title length limit
// (CLO-284 S3) rejects oversized titles.
func TestCreateIssueDocument_TitleLengthCaps(t *testing.T) {
	issueID := newIssueDocumentFixture(t)

	longTitle := strings.Repeat("t", maxIssueDocumentTitleRunes+1)
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issue-documents", map[string]any{
		"issue_id": issueID,
		"type":     "requirements",
		"title":    longTitle,
		"content":  "# Requirements",
	})
	testHandler.CreateIssueDocument(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("oversized title: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// A title exactly at the cap still succeeds.
	okTitle := strings.Repeat("t", maxIssueDocumentTitleRunes)
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issue-documents", map[string]any{
		"issue_id": issueID,
		"type":     "requirements",
		"title":    okTitle,
		"content":  "# Requirements",
	})
	testHandler.CreateIssueDocument(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("title at cap: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

// TestIssueDocumentSearchEscapesWildcards verifies the q filter treats LIKE
// wildcards literally instead of matching everything (CLO-284 S4, mirrors
// ListIssues).
func TestIssueDocumentSearchEscapesWildcards(t *testing.T) {
	issueID := newIssueDocumentFixture(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issue-documents", map[string]any{
		"issue_id": issueID,
		"type":     "testing",
		"title":    "report.md",
		"content":  "# Report",
	})
	testHandler.CreateIssueDocument(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// `%` alone must not act as a match-everything wildcard.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issue-documents?q=%25", nil)
	testHandler.ListIssueDocuments(w, req)
	var list struct {
		Items []IssueDocumentResponse `json:"items"`
		Total int64                   `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Total != 0 {
		t.Fatalf("q='%%': expected 0 hits (wildcard escaped), got %d", list.Total)
	}

	// `_` alone must not match any single-character title.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issue-documents?q=_", nil)
	testHandler.ListIssueDocuments(w, req)
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Total != 0 {
		t.Fatalf("q='_': expected 0 hits (wildcard escaped), got %d", list.Total)
	}
}
