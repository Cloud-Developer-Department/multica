package resourcetmpl

import (
	"fmt"
	"strconv"
	"strings"
)

// SchemaVersion is the format version this package reads and writes. It is the
// value stamped into every template produced by export, and the baseline
// against which incoming templates are compared.
const SchemaVersion = "1.0"

// supportedMajor is the only schema major version the current package accepts.
// A template whose schema_version has a different major is rejected with
// CodeTemplateVersionUnsupported — no compatibility shim, no silent downgrade.
const supportedMajor = 1

// SupportsVersion reports whether v is compatible with the current schema.
// Compatibility is major-version based: the same major is accepted regardless
// of minor (forward-compatible additions), while a different major is refused.
// An empty or malformed version is not supported.
//
// Examples (supportedMajor == 1):
//
//	SupportsVersion("1.0")   == true
//	SupportsVersion("1.7")   == true   // same major, newer minor
//	SupportsVersion("2.0")   == false  // major bump
//	SupportsVersion("")      == false
//	SupportsVersion("abc")   == false
func SupportsVersion(v string) bool {
	major, ok := parseMajor(v)
	return ok && major == supportedMajor
}

// parseMajor extracts the leading integer segment of a "MAJOR.minor[.patch]"
// style version string. It requires at least one digit and a "." separator
// (so bare integers are rejected — schema_version is always dotted). The
// remaining segments are intentionally ignored: minor/patch drift is allowed.
func parseMajor(v string) (int, bool) {
	v = strings.TrimSpace(v)
	dot := strings.IndexByte(v, '.')
	if dot <= 0 {
		return 0, false
	}
	major, err := strconv.Atoi(v[:dot])
	if err != nil || major < 0 {
		return 0, false
	}
	return major, true
}

// versionError builds the structured error emitted for an unsupported or
// missing schema_version. Kept here so validate.go and any future caller agree
// on the message shape.
func versionError(v string) Error {
	return Error{
		Code:    CodeTemplateVersionUnsupported,
		Path:    "schema_version",
		Message: fmt.Sprintf("schema_version %q is not supported (supported major: %d)", v, supportedMajor),
	}
}
