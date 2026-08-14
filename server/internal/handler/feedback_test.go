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
)

func TestCreateFeedbackHappyPath(t *testing.T) {
	clearFeedbackForTestUser(t)

	req := newRequest("POST", "/api/feedback", CreateFeedbackRequest{
		Message: "Love the product, dark mode flashes on startup",
	})
	w := httptest.NewRecorder()
	testHandler.CreateFeedback(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp FeedbackResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID == "" {
		t.Fatal("expected feedback id in response")
	}
}

func TestCreateFeedbackStoresStructuredContext(t *testing.T) {
	clearFeedbackForTestUser(t)

	req := newRequest("POST", "/api/feedback", CreateFeedbackRequest{
		Message: "Desktop route crashed",
		Context: &FeedbackContext{
			Kind:    "desktop_route_error",
			Trigger: "route-errorElement",
			Error: FeedbackErrorContext{
				Name:    "TypeError",
				Message: "Cannot read properties of undefined",
				Stack:   "TypeError: Cannot read properties of undefined",
			},
		},
	})
	w := httptest.NewRecorder()
	testHandler.CreateFeedback(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp FeedbackResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	var metadata []byte
	if err := testPool.QueryRow(
		context.Background(),
		`SELECT metadata FROM feedback WHERE id = $1`,
		parseUUID(resp.ID),
	).Scan(&metadata); err != nil {
		t.Fatalf("load feedback metadata: %v", err)
	}
	var stored struct {
		Context *FeedbackContext `json:"context"`
	}
	if err := json.Unmarshal(metadata, &stored); err != nil {
		t.Fatalf("decode feedback metadata: %v", err)
	}
	if stored.Context == nil {
		t.Fatal("expected structured context in feedback metadata")
	}
	if stored.Context.Kind != "desktop_route_error" {
		t.Fatalf("context kind = %q, want desktop_route_error", stored.Context.Kind)
	}
	if stored.Context.Trigger != "route-errorElement" {
		t.Fatalf("context trigger = %q, want route-errorElement", stored.Context.Trigger)
	}
	if stored.Context.Error.Name != "TypeError" {
		t.Fatalf("error name = %q, want TypeError", stored.Context.Error.Name)
	}
	if stored.Context.Error.Message != "Cannot read properties of undefined" {
		t.Fatalf(
			"error message = %q, want Cannot read properties of undefined",
			stored.Context.Error.Message,
		)
	}
	if stored.Context.Error.Stack != "TypeError: Cannot read properties of undefined" {
		t.Fatalf(
			"error stack = %q, want TypeError: Cannot read properties of undefined",
			stored.Context.Error.Stack,
		)
	}
}

func TestCreateFeedbackRejectsMalformedContext(t *testing.T) {
	req := newRequest("POST", "/api/feedback", map[string]any{
		"message": "Desktop route crashed",
		"context": "not-an-object",
	})
	w := httptest.NewRecorder()
	testHandler.CreateFeedback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateFeedbackRejectsUnknownContextKind(t *testing.T) {
	req := newRequest("POST", "/api/feedback", CreateFeedbackRequest{
		Message: "Desktop route crashed",
		Context: &FeedbackContext{
			Kind:    "arbitrary_diagnostic",
			Trigger: "route-errorElement",
			Error: FeedbackErrorContext{
				Name:    "TypeError",
				Message: "Cannot read properties of undefined",
			},
		},
	})
	w := httptest.NewRecorder()
	testHandler.CreateFeedback(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateFeedbackRejectsEmptyContextFields(t *testing.T) {
	tests := []struct {
		name    string
		context FeedbackContext
	}{
		{
			name:    "empty context",
			context: FeedbackContext{},
		},
		{
			name: "trigger",
			context: FeedbackContext{
				Kind: desktopRouteErrorFeedbackContextKind,
				Error: FeedbackErrorContext{
					Name:    "TypeError",
					Message: "Cannot read properties of undefined",
				},
			},
		},
		{
			name: "error name",
			context: FeedbackContext{
				Kind:    desktopRouteErrorFeedbackContextKind,
				Trigger: "route-errorElement",
				Error: FeedbackErrorContext{
					Name:    "   ",
					Message: "Cannot read properties of undefined",
				},
			},
		},
		{
			name: "error message",
			context: FeedbackContext{
				Kind:    desktopRouteErrorFeedbackContextKind,
				Trigger: "route-errorElement",
				Error: FeedbackErrorContext{
					Name:    "TypeError",
					Message: "\n\t",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := newRequest("POST", "/api/feedback", CreateFeedbackRequest{
				Message: "Desktop route crashed",
				Context: &tt.context,
			})
			w := httptest.NewRecorder()
			testHandler.CreateFeedback(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestCreateFeedbackEmptyMessage(t *testing.T) {
	req := newRequest("POST", "/api/feedback", CreateFeedbackRequest{Message: "   "})
	w := httptest.NewRecorder()
	testHandler.CreateFeedback(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateFeedbackRateLimit(t *testing.T) {
	clearFeedbackForTestUser(t)

	for i := 0; i < feedbackHourlyRateLimit; i++ {
		req := newRequest("POST", "/api/feedback", CreateFeedbackRequest{
			Message: "feedback #" + strconv.Itoa(i),
		})
		w := httptest.NewRecorder()
		testHandler.CreateFeedback(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("iteration %d: expected 201, got %d: %s", i, w.Code, w.Body.String())
		}
	}
	req := newRequest("POST", "/api/feedback", CreateFeedbackRequest{Message: "one too many"})
	w := httptest.NewRecorder()
	testHandler.CreateFeedback(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", w.Code, w.Body.String())
	}
}

// clearFeedbackForTestUser wipes all feedback rows for the shared test user
// at both test start (fresh state) and test end (via t.Cleanup), so tests
// in this file don't interfere with each other or with the hourly rate-limit
// window when run in sequence.
func clearFeedbackForTestUser(t *testing.T) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `DELETE FROM feedback WHERE creator_id = $1`, parseUUID(testUserID)); err != nil {
		t.Fatalf("clear feedback: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM feedback WHERE creator_id = $1`, parseUUID(testUserID))
	})
}

// TestFeedbackTitleFromMessageCJK verifies that title derivation from the
// legacy message truncates by runes, never splitting a multi-byte UTF-8 (CJK)
// rune — the byte-slice behavior that previously produced invalid UTF-8 titles
// rejected by PostgreSQL.
func TestFeedbackTitleFromMessageCJK(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{
			name:    "empty message falls back to placeholder",
			message: "   ",
			want:    "(no title)",
		},
		{
			name:    "short ascii passes through unchanged",
			message: "short title",
			want:    "short title",
		},
		{
			name:    "ascii longer than 80 bytes truncates",
			message: strings.Repeat("a", 100),
			want:    strings.Repeat("a", 80),
		},
		{
			name:    "exactly 80 ascii bytes unchanged",
			message: strings.Repeat("a", 80),
			want:    strings.Repeat("a", 80),
		},
		{
			name:    "cjk message at 90 bytes (30 runes) unchanged",
			message: strings.Repeat("反", 30),
			want:    strings.Repeat("反", 30),
		},
		{
			name:    "cjk message longer than 80 runes truncated to 80 runes",
			message: strings.Repeat("反", 100),
			want:    strings.Repeat("反", 80),
		},
		{
			name:    "cjk message exactly 80 runes unchanged",
			message: strings.Repeat("反", 80),
			want:    strings.Repeat("反", 80),
		},
		{
			name:    "mixed ascii+cjk over byte budget truncates at rune boundary",
			message: strings.Repeat("a", 70) + strings.Repeat("反", 20),
			want:    strings.Repeat("a", 70) + strings.Repeat("反", 10),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := feedbackTitleFromMessage(tt.message)
			if got != tt.want {
				t.Fatalf("feedbackTitleFromMessage() = %q, want %q", got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("feedbackTitleFromMessage() returned invalid UTF-8: %q", got)
			}
			if n := utf8.RuneCountInString(got); n > 80 {
				t.Fatalf("feedbackTitleFromMessage() returned %d runes, want <= 80", n)
			}
		})
	}
}
