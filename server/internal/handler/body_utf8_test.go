package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newRawRequest builds a request with an exact byte body, for cases where the
// wire bytes must be crafted precisely — e.g. a payload that is not valid
// UTF-8. Headers mirror newRequest so the handler sees an authenticated,
// workspace-scoped call.
func newRawRequest(method, path string, body []byte) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	return req
}

// gbkTitle is "【派单】验证" encoded as GBK. Every byte is a GBK lead/trail
// byte that is structurally invalid as UTF-8, so utf8.Valid rejects the whole
// request body — exactly the class of bad data this gate keeps out of storage.
var gbkTitle = []byte{0xa1, 0xbe, 0xc5, 0xc9, 0xb5, 0xa5, 0xa1, 0xbf, 0xd1, 0xe9, 0xd6, 0xa4}

// TestCreateIssue_RejectsInvalidUTF8Body pins the UTF-8 request-body gate. A
// client that posts GBK-encoded bytes (invalid UTF-8) must get a 400 with the
// stable invalid_utf8_body code, not have the bytes stored as mojibake.
func TestCreateIssue_RejectsInvalidUTF8Body(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	body := append([]byte(`{"title":"`), gbkTitle...)
	body = append(body, []byte(`"}`)...)

	w := httptest.NewRecorder()
	r := newRawRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, body)

	testHandler.CreateIssue(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["code"] != "invalid_utf8_body" {
		t.Fatalf("expected code=invalid_utf8_body, got %q (body=%s)", resp["code"], w.Body.String())
	}
}

// TestCreateIssue_AcceptsValidUTF8Body pins that a legitimate UTF-8 request
// (including CJK) still creates the issue — the gate is zero-cost for
// well-formed clients.
func TestCreateIssue_AcceptsValidUTF8Body(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "合法的中文标题",
		"status":   "todo",
		"priority": "medium",
	})
	testHandler.CreateIssue(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&issue); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	t.Cleanup(func() { deleteTestIssue(t, issue.ID) })
}

// TestDecodeJSONBody_PreservesDecoderTrailingByteSemantics pins the Decoder
// (not Unmarshal) requirement: bytes after the first JSON value are ignored,
// matching the former json.NewDecoder(r.Body).Decode behavior. Unmarshal would
// error on the trailing garbage, silently changing accept/refuse for clients
// that send concatenated or padded JSON.
func TestDecodeJSONBody_PreservesDecoderTrailingByteSemantics(t *testing.T) {
	var dst struct {
		Title string `json:"title"`
	}
	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte(`{"title":"x"} trailing garbage`)))
	if err := decodeJSONBody(req, &dst); err != nil {
		t.Fatalf("decodeJSONBody with trailing bytes: expected nil, got %v", err)
	}
	if dst.Title != "x" {
		t.Fatalf("title = %q, want %q", dst.Title, "x")
	}
}

// TestWriteBodyDecodeError_InvalidUTF8 maps errInvalidUTF8Body to the stable
// 400 invalid_utf8_body response the CLI/frontend translate on code.
func TestWriteBodyDecodeError_InvalidUTF8(t *testing.T) {
	w := httptest.NewRecorder()
	writeBodyDecodeError(w, errInvalidUTF8Body)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["code"] != "invalid_utf8_body" {
		t.Fatalf("expected code=invalid_utf8_body, got %q", resp["code"])
	}
	if resp["error"] == "" {
		t.Fatal("expected a non-empty actionable error message")
	}
}
