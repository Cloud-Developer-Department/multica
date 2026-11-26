package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"unicode/utf8"
)

const (
	invalidUTF8BodyCode = "invalid_utf8_body"
	// invalidUTF8BodyMessage is the actionable English 400 body for a payload
	// whose raw bytes are not valid UTF-8. It calls out the common Windows
	// cause (a GBK/ANSI file piped as JSON) and the --content-file fix, so a
	// CLI author can correct it without reading server internals.
	invalidUTF8BodyMessage = "request body is not valid UTF-8; send JSON as UTF-8 (on Windows write the content to a UTF-8 file and use --content-file)"
)

// errInvalidUTF8Body marks a request whose raw bytes are not valid UTF-8.
var errInvalidUTF8Body = errors.New(invalidUTF8BodyCode)

// writeInvalidUTF8Body writes the 400 response for a non-UTF-8 request body.
func writeInvalidUTF8Body(w http.ResponseWriter) {
	writeErrorCode(w, http.StatusBadRequest, invalidUTF8BodyCode, invalidUTF8BodyMessage)
}

// rejectInvalidUTF8Body reports whether the raw request body is invalid UTF-8
// (e.g. GBK), writing the 400 invalid_utf8_body response when it is.
func rejectInvalidUTF8Body(w http.ResponseWriter, body []byte) bool {
	if !utf8.Valid(body) {
		writeInvalidUTF8Body(w)
		return true
	}
	return false
}

// decodeJSONBody reads the entire request body and decodes it as JSON into dst.
// It preserves the former json.NewDecoder(r.Body).Decode(dst) semantics — a
// Decoder rather than Unmarshal, so trailing bytes after the first JSON value
// are ignored. A body that is not valid UTF-8 fails before decode, so GBK and
// other non-UTF-8 bytes are rejected before they can reach storage.
//
// On failure it returns the read error (including *http.MaxBytesError when the
// caller installed a MaxBytesReader), errInvalidUTF8Body, or the JSON decode
// error. Callers map the failure to a response with writeBodyDecodeError,
// optionally translating *http.MaxBytesError to 413 first.
func decodeJSONBody(r *http.Request, dst any) error {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if !utf8.Valid(body) {
		return errInvalidUTF8Body
	}
	return json.NewDecoder(bytes.NewReader(body)).Decode(dst)
}

// writeBodyDecodeError maps a decodeJSONBody failure to an HTTP response. The
// invalid-UTF-8 case gets a stable machine-readable code so CLI/frontend can
// translate it; everything else keeps the existing 400 message.
func writeBodyDecodeError(w http.ResponseWriter, err error) {
	if errors.Is(err, errInvalidUTF8Body) {
		writeInvalidUTF8Body(w)
		return
	}
	writeError(w, http.StatusBadRequest, "invalid request body")
}
