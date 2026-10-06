package resourcetmpl

import "testing"

func TestSupportsVersion(t *testing.T) {
	cases := []struct {
		v    string
		want bool
	}{
		{"1.0", true},
		{"1.7", true},   // same major, newer minor
		{"1.0.0", true}, // trailing .patch tolerated by major check
		{"2.0", false},  // major bump
		{"0.9", false},  // different major
		{"", false},
		{"abc", false},
		{"1", false}, // not dotted
		{"-1.0", false},
	}
	for _, tc := range cases {
		if got := SupportsVersion(tc.v); got != tc.want {
			t.Errorf("SupportsVersion(%q) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

func TestSchemaVersionConstant(t *testing.T) {
	// The stamped version must itself be supported.
	if !SupportsVersion(SchemaVersion) {
		t.Errorf("SchemaVersion %q is not supported by SupportsVersion", SchemaVersion)
	}
}
