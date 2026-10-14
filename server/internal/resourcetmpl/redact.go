package resourcetmpl

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// This file implements the secret-detection guard shared by the export and
// import sides of the template pipeline. The design contract (CLO-245 §2/§4)
// is:
//
//   - On export, custom_env is downgraded to custom_env_keys (key + required
//     only) and mcp_config is downgraded to mcp_servers (structural skeleton +
//     requires_auth). That downgrade happens in the export handler (T2), which
//     owns the live agent/squad domain models; resourcetmpl stays free of those
//     dependencies.
//   - On BOTH sides, DetectSecrets is run as a backstop: any plaintext
//     credential that slipped through (e.g. a token hardcoded in an MCP
//     config_skeleton) causes the template to be rejected with
//     SECRET_DETECTED. Secrets are never silently stripped.
//
// DetectSecrets is therefore the portable, model-free core of redact.go: it
// walks an arbitrary template JSON tree and flags credential-shaped locations
// by key name and value shape, with deliberately conservative value heuristics
// to keep false positives low.

// secretKeyFragmentRe matches a credential term inside a compound key, bounded
// by start/end or a non-alphanumeric separator so that benign keys such as
// "transport", "name", "source_url" or "description" are not matched.
var secretKeyFragmentRe = regexp.MustCompile(
	`(?i)(?:^|[^a-z0-9])(token|password|passwd|pwd|secret|api[_-]?key|apikey|private[_-]?key|access[_-]?key|secret[_-]?key|client[_-]?secret|authorization|auth[_-]?token|authtoken|bearer|credential)s?(?:[^a-z0-9]|$)`)

// secretKeyExact lists key names (lower-cased, separator-stripped) that are
// credentials on their own. "auth"/"token"/"secret"/"key"-style field names
// with a string value are treated as secrets.
var secretKeyExact = map[string]bool{
	"token": true, "password": true, "passwd": true, "pwd": true,
	"secret": true, "apikey": true, "privatekey": true, "accesskey": true,
	"authorization": true, "authtoken": true, "bearer": true, "credential": true,
	"auth":    true,
	"api_key": true, "api-key": true, "private_key": true, "access_key": true,
	"secret_key": true, "client_secret": true, "auth_token": true,
	"access_token": true, "refresh_token": true, "id_token": true,
}

// secretValueRe matches values that begin with a well-known credential prefix
// (provider key formats, HTTP auth schemes, PEM private-key headers). Anchored
// at start so prose like "use Bearer ..." in instructions is not flagged.
var secretValueRe = regexp.MustCompile(
	`(?i)^(?:sk-[a-zA-Z0-9_-]{16,}|gh[pousr]_[A-Za-z0-9]{16,}|github_pat_[A-Za-z0-9_]{16,}|` +
		`AKIA[0-9A-Z]{16}|xox[baprs]-[A-Za-z0-9-]{10,}|` +
		`Bearer\s+\S|Basic\s+[A-Za-z0-9+/=]{8,}|` +
		`-----BEGIN [A-Z ]*PRIVATE KEY-----)`)

// connStringRe matches a URL with embedded userinfo ("scheme://user:pass@"),
// which is a classic leaked-credential shape.
var connStringRe = regexp.MustCompile(`(?i)://[^/\s:@]+:[^/\s:@]+@`)

// isSecretKey reports whether a JSON object key name denotes a credential
// location. Exact matches cover the common single-word names; the fragment
// regex covers compound names such as "github_token", "db_password",
// "openai_api_key" or "x-api-key".
func isSecretKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if k == "" {
		return false
	}
	if secretKeyExact[k] {
		return true
	}
	return secretKeyFragmentRe.MatchString(k)
}

// isSecretValue reports whether a string value looks like a credential. The
// heuristics are intentionally specific (known prefixes, PEM headers, URLs
// with embedded userinfo) to avoid flagging ordinary prose or identifiers.
func isSecretValue(v string) bool {
	s := strings.TrimSpace(v)
	if s == "" {
		return false
	}
	if secretValueRe.MatchString(s) {
		return true
	}
	return connStringRe.MatchString(s)
}

// DetectSecrets scans a template JSON document for plaintext credentials and
// returns one CodeSecretDetected error per offending location. It is safe to
// call on partially-valid input: if the bytes are not valid JSON the function
// returns nil (malformed JSON is reported by Validate separately).
//
// Each string value is evaluated against two independent signals: the key it
// sits under (a credential-named key is flagged regardless of value) and the
// value's shape (a credential-looking value is flagged regardless of key).
// This catches both an "api_key" field holding anything and a stray
// "sk-..." token dropped into an unexpected place.
func DetectSecrets(raw []byte) []Error {
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil
	}
	var errs []Error
	scanSecrets(root, "", "", func(path string) {
		errs = append(errs, Error{
			Code:    CodeSecretDetected,
			Path:    path,
			Message: "potential plaintext secret detected",
		})
	})
	return errs
}

// scanSecrets walks a decoded JSON value recursively. key is the object key
// (or repeating element key for arrays) under which v sits, used for the
// key-name signal at string leaves. path is the dotted/[index] location used
// in the resulting error.
func scanSecrets(v any, path, key string, report func(string)) {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			scanSecrets(val, joinPath(path, k), k, report)
		}
	case []any:
		for i, val := range x {
			scanSecrets(val, fmt.Sprintf("%s[%d]", path, i), key, report)
		}
	case string:
		if isSecretKey(key) || isSecretValue(x) {
			report(path)
		}
	}
}

// joinPath builds a dotted location, treating the empty base as the document
// root so top-level keys render as "k" rather than ".k".
func joinPath(base, key string) string {
	if base == "" {
		return key
	}
	return base + "." + key
}
