package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/util"
)

// feedbackTestCleanup removes all feedback rows (plus votes/comments, which
// cascade) for the shared handler test user so tests in this file are
// hermetic and the hourly rate-limit window does not leak across tests.
func feedbackTestCleanup(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `DELETE FROM feedback WHERE creator_id = $1`, parseUUID(testUserID)); err != nil {
		t.Fatalf("clear feedback: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM feedback WHERE creator_id = $1`, parseUUID(testUserID))
	})
}

// insertTestFeedback inserts a feedback row directly and returns its id.
func insertTestFeedback(t *testing.T, title, typ, desc string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO feedback (creator_id, workspace_id, title, type, description)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, testUserID, testWorkspaceID, title, typ, desc).Scan(&id); err != nil {
		t.Fatalf("insert feedback: %v", err)
	}
	return id
}

// insertTestFeedbackByUser inserts a feedback row owned by another user
// (still inside the test workspace) so list/detail can exercise creator names.
func insertTestFeedbackByUser(t *testing.T, creatorID, title, typ, desc string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO feedback (creator_id, workspace_id, title, type, description)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, creatorID, testWorkspaceID, title, typ, desc).Scan(&id); err != nil {
		t.Fatalf("insert feedback: %v", err)
	}
	return id
}

func TestListFeedbacksPagination(t *testing.T) {
	feedbackTestCleanup(t)
	ctx := context.Background()

	for i := 0; i < 25; i++ {
		insertTestFeedback(t, "feedback title "+strconv.Itoa(i), "feature", "description "+strconv.Itoa(i))
	}

	req := newRequest("GET", "/api/feedbacks?page=1&page_size=10", nil)
	w := httptest.NewRecorder()
	testHandler.ListFeedbacks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp FeedbackListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Total != 25 {
		t.Fatalf("expected total 25, got %d", resp.Total)
	}
	if len(resp.Items) != 10 {
		t.Fatalf("expected 10 items on page, got %d", len(resp.Items))
	}
	if !resp.HasMore {
		t.Fatal("expected has_more=true on first page of 25")
	}
	if resp.Page != 1 || resp.PageSize != 10 {
		t.Fatalf("unexpected page/page_size: %d/%d", resp.Page, resp.PageSize)
	}
	if resp.Items[0].CreatedAt < resp.Items[1].CreatedAt {
		t.Fatal("expected newest-first ordering (latest sort)")
	}

	// Second page
	req2 := newRequest("GET", "/api/feedbacks?page=3&page_size=10", nil)
	w2 := httptest.NewRecorder()
	testHandler.ListFeedbacks(w2, req2)
	var resp2 FeedbackListResponse
	if err := json.NewDecoder(w2.Body).Decode(&resp2); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp2.Items) != 5 {
		t.Fatalf("expected 5 items on last page, got %d", len(resp2.Items))
	}
	if resp2.HasMore {
		t.Fatal("expected has_more=false on last page")
	}

	// Clean up the extra rows created in the loop.
	testPool.Exec(ctx, `DELETE FROM feedback WHERE creator_id = $1`, parseUUID(testUserID))
}

func TestListFeedbacksTypeFilterAndKeyword(t *testing.T) {
	feedbackTestCleanup(t)

	insertTestFeedback(t, "bug report", "bug", "it crashed")
	insertTestFeedback(t, "feature idea", "feature", "please add dark mode")
	insertTestFeedback(t, "other stuff", "other", "just saying hi")

	// Type filter
	req := newRequest("GET", "/api/feedbacks?type=bug", nil)
	w := httptest.NewRecorder()
	testHandler.ListFeedbacks(w, req)
	var resp FeedbackListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Total != 1 || len(resp.Items) != 1 || resp.Items[0].Type != "bug" {
		t.Fatalf("expected 1 bug feedback, got %d items (total %d)", len(resp.Items), resp.Total)
	}

	// Keyword search matches title and description.
	req2 := newRequest("GET", "/api/feedbacks?keyword=dark%20mode", nil)
	w2 := httptest.NewRecorder()
	testHandler.ListFeedbacks(w2, req2)
	var resp2 FeedbackListResponse
	if err := json.NewDecoder(w2.Body).Decode(&resp2); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp2.Total != 1 || resp2.Items[0].Title != "feature idea" {
		t.Fatalf("keyword search failed: %+v", resp2)
	}

	req3 := newRequest("GET", "/api/feedbacks?keyword=it%20crashed", nil)
	w3 := httptest.NewRecorder()
	testHandler.ListFeedbacks(w3, req3)
	var resp3 FeedbackListResponse
	if err := json.NewDecoder(w3.Body).Decode(&resp3); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp3.Total != 1 || resp3.Items[0].Title != "bug report" {
		t.Fatalf("description keyword search failed: %+v", resp3)
	}

	// Combined type + keyword.
	req4 := newRequest("GET", "/api/feedbacks?type=feature&keyword=please", nil)
	w4 := httptest.NewRecorder()
	testHandler.ListFeedbacks(w4, req4)
	var resp4 FeedbackListResponse
	if err := json.NewDecoder(w4.Body).Decode(&resp4); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp4.Total != 1 {
		t.Fatalf("combined filter failed: %+v", resp4)
	}
}

func TestListFeedbacksInvalidParams(t *testing.T) {
	feedbackTestCleanup(t)

	req := newRequest("GET", "/api/feedbacks?type=bogus", nil)
	w := httptest.NewRecorder()
	testHandler.ListFeedbacks(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid type: expected 400, got %d", w.Code)
	}

	req2 := newRequest("GET", "/api/feedbacks?sort=bogus", nil)
	w2 := httptest.NewRecorder()
	testHandler.ListFeedbacks(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("invalid sort: expected 400, got %d", w2.Code)
	}
}

func TestListFeedbacksHotAndCommentsSort(t *testing.T) {
	feedbackTestCleanup(t)

	fbA := insertTestFeedback(t, "popular", "feature", "many votes")
	fbB := insertTestFeedback(t, "talked about", "bug", "many comments")

	// fbA: 3 votes; fbB: 1 vote + 2 comments.
	for i := 0; i < 3; i++ {
		user := createFeedbackTestUser(t)
		testPool.Exec(context.Background(), `INSERT INTO feedback_vote (feedback_id, user_id) VALUES ($1, $2)`, parseUUID(fbA), parseUUID(user))
	}
	userB := createFeedbackTestUser(t)
	testPool.Exec(context.Background(), `INSERT INTO feedback_vote (feedback_id, user_id) VALUES ($1, $2)`, parseUUID(fbB), parseUUID(userB))
	for i := 0; i < 2; i++ {
		user := createFeedbackTestUser(t)
		testPool.Exec(context.Background(), `INSERT INTO feedback_comment (feedback_id, user_id, content) VALUES ($1, $2, $3)`, parseUUID(fbB), parseUUID(user), "nice")
	}

	// hot sort -> fbA first (3 votes vs 1).
	req := newRequest("GET", "/api/feedbacks?sort=hot", nil)
	w := httptest.NewRecorder()
	testHandler.ListFeedbacks(w, req)
	var resp FeedbackListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Items) != 2 || resp.Items[0].Title != "popular" {
		t.Fatalf("hot sort: expected popular first, got %+v", resp.Items)
	}
	if resp.Items[0].VoteCount != 3 {
		t.Fatalf("expected vote_count 3, got %d", resp.Items[0].VoteCount)
	}

	// comments sort -> fbB first (2 comments vs 0).
	req2 := newRequest("GET", "/api/feedbacks?sort=comments", nil)
	w2 := httptest.NewRecorder()
	testHandler.ListFeedbacks(w2, req2)
	var resp2 FeedbackListResponse
	if err := json.NewDecoder(w2.Body).Decode(&resp2); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp2.Items) != 2 || resp2.Items[0].Title != "talked about" {
		t.Fatalf("comments sort: expected 'talked about' first, got %+v", resp2.Items)
	}
	if resp2.Items[0].CommentCount != 2 {
		t.Fatalf("expected comment_count 2, got %d", resp2.Items[0].CommentCount)
	}
}

func TestGetFeedback(t *testing.T) {
	feedbackTestCleanup(t)

	fbID := insertTestFeedback(t, "detail title", "improvement", "full description here")
	// Give it a vote + a comment so detail counts are exercised.
	other := createFeedbackTestUser(t)
	testPool.Exec(context.Background(), `INSERT INTO feedback_vote (feedback_id, user_id) VALUES ($1, $2)`, parseUUID(fbID), parseUUID(other))
	testPool.Exec(context.Background(), `INSERT INTO feedback_comment (feedback_id, user_id, content) VALUES ($1, $2, $3)`, parseUUID(fbID), parseUUID(other), "first!")

	req := newRequest("GET", "/api/feedbacks/"+fbID, nil)
	req = withURLParam(req, "id", fbID)
	w := httptest.NewRecorder()
	testHandler.GetFeedback(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp FeedbackSummary
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID != fbID || resp.Title != "detail title" || resp.Type != "improvement" {
		t.Fatalf("unexpected detail: %+v", resp)
	}
	if resp.VoteCount != 1 || resp.CommentCount != 1 {
		t.Fatalf("expected 1 vote and 1 comment, got %d/%d", resp.VoteCount, resp.CommentCount)
	}
	if resp.MyVote {
		t.Fatal("my_vote should be false when viewer did not vote")
	}
}

func TestGetFeedbackMyVote(t *testing.T) {
	feedbackTestCleanup(t)

	fbID := insertTestFeedback(t, "my vote", "bug", "desc")
	testPool.Exec(context.Background(), `INSERT INTO feedback_vote (feedback_id, user_id) VALUES ($1, $2)`, parseUUID(fbID), parseUUID(testUserID))

	req := newRequest("GET", "/api/feedbacks/"+fbID, nil)
	req = withURLParam(req, "id", fbID)
	w := httptest.NewRecorder()
	testHandler.GetFeedback(w, req)
	var resp FeedbackSummary
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.MyVote {
		t.Fatal("expected my_vote=true when viewer voted")
	}
	if resp.VoteCount != 1 {
		t.Fatalf("expected vote_count 1, got %d", resp.VoteCount)
	}
}

func TestGetFeedbackNotFoundAndInvalid(t *testing.T) {
	feedbackTestCleanup(t)

	// Valid UUID but nonexistent.
	req := newRequest("GET", "/api/feedbacks/00000000-0000-0000-0000-000000000001", nil)
	req = withURLParam(req, "id", "00000000-0000-0000-0000-000000000001")
	w := httptest.NewRecorder()
	testHandler.GetFeedback(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}

	// Malformed id.
	req2 := newRequest("GET", "/api/feedbacks/not-a-uuid", nil)
	req2 = withURLParam(req2, "id", "not-a-uuid")
	w2 := httptest.NewRecorder()
	testHandler.GetFeedback(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestCreateFeedbackCenterHappyPath(t *testing.T) {
	feedbackTestCleanup(t)

	req := newRequest("POST", "/api/feedbacks", CreateFeedbackCenterRequest{
		Type:        "feature",
		Title:       "Batch create agents",
		Description: "I want to create many agents at once.",
	})
	w := httptest.NewRecorder()
	testHandler.CreateFeedbackCenter(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp FeedbackSummary
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Title != "Batch create agents" || resp.Type != "feature" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.CreatorID != testUserID {
		t.Fatalf("expected creator_id from session, got %q", resp.CreatorID)
	}
	if resp.WorkspaceID != testWorkspaceID {
		t.Fatalf("expected workspace_id from context, got %q", resp.WorkspaceID)
	}
}

func TestCreateFeedbackCenterRequiresAuth(t *testing.T) {
	feedbackTestCleanup(t)

	req := newRequest("POST", "/api/feedbacks", CreateFeedbackCenterRequest{
		Type:        "bug",
		Title:       "no auth",
		Description: "should be rejected",
	})
	req.Header.Del("X-User-ID")
	w := httptest.NewRecorder()
	testHandler.CreateFeedbackCenter(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateFeedbackCenterValidation(t *testing.T) {
	feedbackTestCleanup(t)

	tests := []struct {
		name string
		req  CreateFeedbackCenterRequest
	}{
		{name: "empty title", req: CreateFeedbackCenterRequest{Type: "bug", Title: "  ", Description: "desc"}},
		{name: "empty description", req: CreateFeedbackCenterRequest{Type: "bug", Title: "title", Description: "   "}},
		{name: "invalid type", req: CreateFeedbackCenterRequest{Type: "bogus", Title: "title", Description: "desc"}},
		{name: "missing type", req: CreateFeedbackCenterRequest{Type: "", Title: "title", Description: "desc"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := newRequest("POST", "/api/feedbacks", tt.req)
			w := httptest.NewRecorder()
			testHandler.CreateFeedbackCenter(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

// TestCreateFeedbackCenterCJKLengthBoundaries verifies title/description
// length validation counts runes (utf8.RuneCountInString), matching the
// frontend maxLength semantics. A 200-CJK-character title is 600 bytes but
// must be accepted; 201 must be rejected — byte-based len() would wrongly
// reject the valid CJK input.
func TestCreateFeedbackCenterCJKLengthBoundaries(t *testing.T) {
	feedbackTestCleanup(t)

	cjkTitle := strings.Repeat("反", feedbackCenterMaxTitle)
	if utf8.RuneCountInString(cjkTitle) != feedbackCenterMaxTitle {
		t.Fatalf("test setup: want %d runes, got %d", feedbackCenterMaxTitle, utf8.RuneCountInString(cjkTitle))
	}
	if len(cjkTitle) != feedbackCenterMaxTitle*3 {
		t.Fatalf("test setup: want %d bytes, got %d", feedbackCenterMaxTitle*3, len(cjkTitle))
	}

	// Exactly 200 CJK chars must pass (bytes are 600, runes are 200).
	req := newRequest("POST", "/api/feedbacks", CreateFeedbackCenterRequest{
		Type:        "bug",
		Title:       cjkTitle,
		Description: "描述",
	})
	w := httptest.NewRecorder()
	testHandler.CreateFeedbackCenter(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("title of %d CJK chars: expected 201, got %d: %s", feedbackCenterMaxTitle, w.Code, w.Body.String())
	}

	// 201 CJK chars must be rejected.
	req2 := newRequest("POST", "/api/feedbacks", CreateFeedbackCenterRequest{
		Type:        "bug",
		Title:       cjkTitle + "反",
		Description: "描述",
	})
	w2 := httptest.NewRecorder()
	testHandler.CreateFeedbackCenter(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("title of %d CJK chars: expected 400, got %d: %s", feedbackCenterMaxTitle+1, w2.Code, w2.Body.String())
	}

	// Exactly 10000 CJK chars description must pass.
	cjkDesc := strings.Repeat("反", feedbackCenterMaxDescription)
	req3 := newRequest("POST", "/api/feedbacks", CreateFeedbackCenterRequest{
		Type:        "bug",
		Title:       "title",
		Description: cjkDesc,
	})
	w3 := httptest.NewRecorder()
	testHandler.CreateFeedbackCenter(w3, req3)
	if w3.Code != http.StatusCreated {
		t.Fatalf("description of %d CJK chars: expected 201, got %d: %s", feedbackCenterMaxDescription, w3.Code, w3.Body.String())
	}

	// 10001 CJK chars description must be rejected.
	req4 := newRequest("POST", "/api/feedbacks", CreateFeedbackCenterRequest{
		Type:        "bug",
		Title:       "title",
		Description: cjkDesc + "反",
	})
	w4 := httptest.NewRecorder()
	testHandler.CreateFeedbackCenter(w4, req4)
	if w4.Code != http.StatusBadRequest {
		t.Fatalf("description of %d CJK chars: expected 400, got %d: %s", feedbackCenterMaxDescription+1, w4.Code, w4.Body.String())
	}
}

// TestCreateFeedbackCommentCJKLengthBoundaries verifies comment length
// validation counts runes so a valid CJK comment (5000 runes, 15000 bytes)
// is accepted while 5001 runes is rejected.
func TestCreateFeedbackCommentCJKLengthBoundaries(t *testing.T) {
	feedbackTestCleanup(t)

	fbID := insertTestFeedback(t, "cjk comment target", "bug", "desc")

	cjkComment := strings.Repeat("反", feedbackCenterMaxComment)
	if len(cjkComment) != feedbackCenterMaxComment*3 {
		t.Fatalf("test setup: want %d bytes, got %d", feedbackCenterMaxComment*3, len(cjkComment))
	}

	// Exactly 5000 CJK chars must pass.
	req := newRequest("POST", "/api/feedbacks/"+fbID+"/comments", CreateFeedbackCommentRequest{Content: cjkComment})
	req = withURLParam(req, "id", fbID)
	w := httptest.NewRecorder()
	testHandler.CreateFeedbackComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("comment of %d CJK chars: expected 201, got %d: %s", feedbackCenterMaxComment, w.Code, w.Body.String())
	}

	// 5001 CJK chars must be rejected.
	req2 := newRequest("POST", "/api/feedbacks/"+fbID+"/comments", CreateFeedbackCommentRequest{Content: cjkComment + "反"})
	req2 = withURLParam(req2, "id", fbID)
	w2 := httptest.NewRecorder()
	testHandler.CreateFeedbackComment(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("comment of %d CJK chars: expected 400, got %d: %s", feedbackCenterMaxComment+1, w2.Code, w2.Body.String())
	}
}

// TestCreateFeedbackLegacyCJKMessage verifies the legacy message-only
// POST /api/feedback chain keeps working end-to-end with multi-byte CJK
// messages: the derived title must be valid UTF-8 (rune-aware truncation),
// so the row is persisted rather than rejected by PostgreSQL.
func TestCreateFeedbackLegacyCJKMessage(t *testing.T) {
	clearFeedbackForTestUser(t)

	// 100 CJK chars = 300 bytes: exceeds the 80-byte budget but must truncate
	// to exactly 80 runes without producing invalid UTF-8.
	message := strings.Repeat("反", 100)
	req := newRequest("POST", "/api/feedback", CreateFeedbackRequest{Message: message})
	w := httptest.NewRecorder()
	testHandler.CreateFeedback(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp FeedbackResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	var title string
	if err := testPool.QueryRow(context.Background(), `SELECT title FROM feedback WHERE id = $1`, parseUUID(resp.ID)).Scan(&title); err != nil {
		t.Fatalf("load feedback title: %v", err)
	}
	if !utf8.ValidString(title) {
		t.Fatalf("stored title is invalid UTF-8: %q", title)
	}
	if n := utf8.RuneCountInString(title); n != 80 {
		t.Fatalf("expected title truncated to 80 runes, got %d: %q", n, title)
	}
	if title != strings.Repeat("反", 80) {
		t.Fatalf("unexpected title content: %q", title)
	}
}

func TestCreateFeedbackVote(t *testing.T) {
	feedbackTestCleanup(t)

	fbID := insertTestFeedback(t, "votable", "feature", "desc")

	req := newRequest("POST", "/api/feedbacks/"+fbID+"/vote", nil)
	req = withURLParam(req, "id", fbID)
	w := httptest.NewRecorder()
	testHandler.CreateFeedbackVote(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["voted"] != true || int64(resp["vote_count"].(float64)) != 1 {
		t.Fatalf("unexpected vote response: %+v", resp)
	}
}

func TestCreateFeedbackVoteDuplicateIsIdempotent(t *testing.T) {
	feedbackTestCleanup(t)

	fbID := insertTestFeedback(t, "double vote", "bug", "desc")

	for i := 0; i < 2; i++ {
		req := newRequest("POST", "/api/feedbacks/"+fbID+"/vote", nil)
		req = withURLParam(req, "id", fbID)
		w := httptest.NewRecorder()
		testHandler.CreateFeedbackVote(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("iteration %d: expected 200, got %d: %s", i, w.Code, w.Body.String())
		}
	}

	var count int64
	testPool.QueryRow(context.Background(), `SELECT count(*) FROM feedback_vote WHERE feedback_id = $1`, parseUUID(fbID)).Scan(&count)
	if count != 1 {
		t.Fatalf("expected exactly 1 vote row after duplicate vote, got %d", count)
	}
}

func TestCreateFeedbackVoteRequiresAuth(t *testing.T) {
	feedbackTestCleanup(t)

	fbID := insertTestFeedback(t, "auth vote", "feature", "desc")
	req := newRequest("POST", "/api/feedbacks/"+fbID+"/vote", nil)
	req.Header.Del("X-User-ID")
	req = withURLParam(req, "id", fbID)
	w := httptest.NewRecorder()
	testHandler.CreateFeedbackVote(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateFeedbackVoteNotFoundAndInvalid(t *testing.T) {
	feedbackTestCleanup(t)

	req := newRequest("POST", "/api/feedbacks/00000000-0000-0000-0000-000000000001/vote", nil)
	req = withURLParam(req, "id", "00000000-0000-0000-0000-000000000001")
	w := httptest.NewRecorder()
	testHandler.CreateFeedbackVote(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}

	req2 := newRequest("POST", "/api/feedbacks/not-a-uuid/vote", nil)
	req2 = withURLParam(req2, "id", "not-a-uuid")
	w2 := httptest.NewRecorder()
	testHandler.CreateFeedbackVote(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestDeleteFeedbackVote(t *testing.T) {
	feedbackTestCleanup(t)

	fbID := insertTestFeedback(t, "unvote", "bug", "desc")
	testPool.Exec(context.Background(), `INSERT INTO feedback_vote (feedback_id, user_id) VALUES ($1, $2)`, parseUUID(fbID), parseUUID(testUserID))

	req := newRequest("DELETE", "/api/feedbacks/"+fbID+"/vote", nil)
	req = withURLParam(req, "id", fbID)
	w := httptest.NewRecorder()
	testHandler.DeleteFeedbackVote(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["voted"] != false || int64(resp["vote_count"].(float64)) != 0 {
		t.Fatalf("unexpected unvote response: %+v", resp)
	}
}

func TestDeleteFeedbackVoteIdempotent(t *testing.T) {
	feedbackTestCleanup(t)

	fbID := insertTestFeedback(t, "no vote to delete", "feature", "desc")

	req := newRequest("DELETE", "/api/feedbacks/"+fbID+"/vote", nil)
	req = withURLParam(req, "id", fbID)
	w := httptest.NewRecorder()
	testHandler.DeleteFeedbackVote(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for idempotent delete, got %d: %s", w.Code, w.Body.String())
	}
}

func TestFeedbackVoteUniqueConstraint(t *testing.T) {
	feedbackTestCleanup(t)

	fbID := insertTestFeedback(t, "unique", "bug", "desc")
	// Direct insert of a second identical vote must fail at the DB layer.
	_, err := testPool.Exec(context.Background(), `
		INSERT INTO feedback_vote (feedback_id, user_id)
		VALUES ($1, $2)
	`, parseUUID(fbID), parseUUID(testUserID))
	if err != nil {
		t.Fatalf("first vote insert failed: %v", err)
	}
	_, err = testPool.Exec(context.Background(), `
		INSERT INTO feedback_vote (feedback_id, user_id)
		VALUES ($1, $2)
	`, parseUUID(fbID), parseUUID(testUserID))
	if err == nil || !strings.Contains(err.Error(), "duplicate key") {
		t.Fatalf("expected duplicate key error on second vote, got %v", err)
	}
}

func TestListFeedbackComments(t *testing.T) {
	feedbackTestCleanup(t)

	fbID := insertTestFeedback(t, "commented", "feature", "desc")
	other := createFeedbackTestUser(t)
	for _, content := range []string{"first comment", "second comment"} {
		testPool.Exec(context.Background(), `INSERT INTO feedback_comment (feedback_id, user_id, content) VALUES ($1, $2, $3)`, parseUUID(fbID), parseUUID(other), content)
	}

	req := newRequest("GET", "/api/feedbacks/"+fbID+"/comments", nil)
	req = withURLParam(req, "id", fbID)
	w := httptest.NewRecorder()
	testHandler.ListFeedbackComments(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp FeedbackCommentListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Total != 2 || len(resp.Items) != 2 {
		t.Fatalf("expected 2 comments, got total %d items %d", resp.Total, len(resp.Items))
	}
	if resp.Items[0].Content != "first comment" || resp.Items[0].UserName == "" {
		t.Fatalf("unexpected comment ordering or missing user: %+v", resp.Items[0])
	}
	if resp.Items[1].Content != "second comment" {
		t.Fatalf("unexpected second comment: %+v", resp.Items[1])
	}
}

func TestListFeedbackCommentsInvalidFeedback(t *testing.T) {
	feedbackTestCleanup(t)

	req := newRequest("GET", "/api/feedbacks/not-a-uuid/comments", nil)
	req = withURLParam(req, "id", "not-a-uuid")
	w := httptest.NewRecorder()
	testHandler.ListFeedbackComments(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed id, got %d", w.Code)
	}

	req2 := newRequest("GET", "/api/feedbacks/00000000-0000-0000-0000-000000000001/comments", nil)
	req2 = withURLParam(req2, "id", "00000000-0000-0000-0000-000000000001")
	w2 := httptest.NewRecorder()
	testHandler.ListFeedbackComments(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing feedback, got %d", w2.Code)
	}
}

func TestCreateFeedbackComment(t *testing.T) {
	feedbackTestCleanup(t)

	fbID := insertTestFeedback(t, "comment me", "feature", "desc")

	req := newRequest("POST", "/api/feedbacks/"+fbID+"/comments", CreateFeedbackCommentRequest{Content: "great idea"})
	req = withURLParam(req, "id", fbID)
	w := httptest.NewRecorder()
	testHandler.CreateFeedbackComment(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp FeedbackCommentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Content != "great idea" || resp.UserID != testUserID {
		t.Fatalf("unexpected comment response: %+v", resp)
	}

	var count int64
	testPool.QueryRow(context.Background(), `SELECT count(*) FROM feedback_comment WHERE feedback_id = $1`, parseUUID(fbID)).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 comment row, got %d", count)
	}
}

func TestCreateFeedbackCommentValidation(t *testing.T) {
	feedbackTestCleanup(t)

	fbID := insertTestFeedback(t, "comment validation", "bug", "desc")

	// Empty content.
	req := newRequest("POST", "/api/feedbacks/"+fbID+"/comments", CreateFeedbackCommentRequest{Content: "   "})
	req = withURLParam(req, "id", fbID)
	w := httptest.NewRecorder()
	testHandler.CreateFeedbackComment(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty comment: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Missing feedback.
	req2 := newRequest("POST", "/api/feedbacks/00000000-0000-0000-0000-000000000001/comments", CreateFeedbackCommentRequest{Content: "hello"})
	req2 = withURLParam(req2, "id", "00000000-0000-0000-0000-000000000001")
	w2 := httptest.NewRecorder()
	testHandler.CreateFeedbackComment(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("missing feedback comment: expected 404, got %d", w2.Code)
	}

	// Malformed id.
	req3 := newRequest("POST", "/api/feedbacks/not-a-uuid/comments", CreateFeedbackCommentRequest{Content: "hello"})
	req3 = withURLParam(req3, "id", "not-a-uuid")
	w3 := httptest.NewRecorder()
	testHandler.CreateFeedbackComment(w3, req3)
	if w3.Code != http.StatusBadRequest {
		t.Fatalf("malformed id comment: expected 400, got %d", w3.Code)
	}
}

func TestCreateFeedbackCommentRequiresAuth(t *testing.T) {
	feedbackTestCleanup(t)

	fbID := insertTestFeedback(t, "auth comment", "feature", "desc")
	req := newRequest("POST", "/api/feedbacks/"+fbID+"/comments", CreateFeedbackCommentRequest{Content: "hello"})
	req.Header.Del("X-User-ID")
	req = withURLParam(req, "id", fbID)
	w := httptest.NewRecorder()
	testHandler.CreateFeedbackComment(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

// createFeedbackTestUser inserts a throwaway user (not a workspace member —
// only used as a vote/comment author) and returns the id.
func createFeedbackTestUser(t *testing.T) string {
	t.Helper()
	var id string
	email := "feedback-test-" + util.UUIDToString(parseUUID(genTestID(t))) + "@multica.ai"
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Feedback Test User", email).Scan(&id); err != nil {
		t.Fatalf("insert feedback test user: %v", err)
	}
	return id
}

func genTestID(t *testing.T) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
		t.Fatalf("gen id: %v", err)
	}
	return id
}
