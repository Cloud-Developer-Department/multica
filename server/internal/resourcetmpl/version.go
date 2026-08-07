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

// supportedMajor and supportedMinor bound the schema versions this package
// accepts. A template is compatible when its major equals supportedMajor AND
// its minor is at most supportedMinor; patch is ignored entirely. A newer
// minor means the template carries structure this build cannot interpret, so
// it is refused rather than silently read as if the extra semantics were
// absent — no compatibility shim, no downgrade guessing.
const (
	supportedMajor = 1
	supportedMinor = 0
)

// SupportsVersion reports whether v is compatible with the current schema.
// An empty or malformed version is not supported.
//
// Examples (supported == 1.0):
//
//	SupportsVersion("1.0")   == true
//	SupportsVersion("1.0.7") == true   // patch ignored
//	SupportsVersion("1.1")   == false  // minor above what this build reads
//	SupportsVersion("2.0")   == false  // major bump
//	SupportsVersion("0.9")   == false  // older major
//	SupportsVersion("")      == false
//	SupportsVersion("abc")   == false
func SupportsVersion(v string) bool {
	major, minor, ok := parseVersion(v)
	return ok && major == supportedMajor && minor <= supportedMinor
}

// parseVersion extracts the major and minor segments of a
// "MAJOR.MINOR[.PATCH]" version string. Both segments are required and must be
// non-negative integers (so a bare integer or a non-numeric minor is
// rejected — schema_version is always dotted). Any further segment is ignored:
// patch drift carries no format meaning.
func parseVersion(v string) (int, int, bool) {
	v = strings.TrimSpace(v)
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return 0, 0, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil || major < 0 {
		return 0, 0, false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil || minor < 0 {
		return 0, 0, false
	}
	return major, minor, true
}

// versionError builds the structured error emitted for an unsupported or
// missing schema_version. Kept here so validate.go and any future caller agree
// on the message shape.
func versionError(v string) Error {
	return Error{
		Code:    CodeTemplateVersionUnsupported,
		Path:    "schema_version",
		Message: fmt.Sprintf("schema_version %q is not supported (this build reads major %d, minor up to %d; patch ignored)", v, supportedMajor, supportedMinor),
	}
}
