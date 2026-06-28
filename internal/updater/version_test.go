package main

import (
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		tag    string
		fields []int
		suffix string
		prefix string
		ok     bool
	}{
		{"1.26", []int{1, 26}, "", "", true},
		{"3.13-slim", []int{3, 13}, "-slim", "", true},
		{"21-jdk", []int{21}, "-jdk", "", true},
		{"0.22.0-arm64", []int{0, 22, 0}, "-arm64", "", true},
		{"2026.03.17", []int{2026, 3, 17}, "", "", true},
		{"v1.35.3", []int{1, 35, 3}, "", "v", true},
		{"lychee-v0.24.2", []int{0, 24, 2}, "", "lychee-v", true},
		{"latest", nil, "", "", false},
		{"jdk21", nil, "", "", false}, // starts with non-numeric
	}
	for _, tt := range tests {
		ver, prefix, ok := ParseVersion(tt.tag)
		if ok != tt.ok {
			t.Errorf("ParseVersion(%q) ok=%v want %v", tt.tag, ok, tt.ok)
			continue
		}
		if !ok {
			continue
		}
		if prefix != tt.prefix {
			t.Errorf("ParseVersion(%q) prefix=%q want %q", tt.tag, prefix, tt.prefix)
		}
		if ver.Suffix != tt.suffix {
			t.Errorf("ParseVersion(%q) suffix=%q want %q", tt.tag, ver.Suffix, tt.suffix)
		}
		if len(ver.Fields) != len(tt.fields) {
			t.Errorf("ParseVersion(%q) fields=%v want %v", tt.tag, ver.Fields, tt.fields)
			continue
		}
		for i, f := range tt.fields {
			if ver.Fields[i] != f {
				t.Errorf("ParseVersion(%q) field[%d]=%d want %d", tt.tag, i, ver.Fields[i], f)
			}
		}
	}
}

func TestIsStable(t *testing.T) {
	if !IsStable("1.27.0") {
		t.Error("1.27.0 should be stable")
	}
	for _, tag := range []string{"1.0.0-rc1", "2.0.0-beta", "3.0.0-alpha", "4.0.0-preview", "nightly"} {
		if IsStable(tag) {
			t.Errorf("%q should not be stable", tag)
		}
	}
}

func TestVersionLess(t *testing.T) {
	a, _, _ := ParseVersion("1.26")
	b, _, _ := ParseVersion("1.27")
	if !VersionLess(a, b) {
		t.Error("1.26 should be less than 1.27")
	}
	if VersionLess(b, a) {
		t.Error("1.27 should not be less than 1.26")
	}
}

func TestClassifyBump(t *testing.T) {
	old, _, _ := ParseVersion("1.26")
	new127, _, _ := ParseVersion("1.27")
	new200, _, _ := ParseVersion("2.0")
	new1261, _, _ := ParseVersion("1.26.1") // more fields than old
	if ClassifyBump(old, new127) != "minor" {
		t.Error("1.26→1.27 should be minor")
	}
	if ClassifyBump(old, new200) != "major" {
		t.Error("1.26→2.0 should be major")
	}
	// when new has more fields, compare first len(old.Fields) fields
	if ClassifyBump(old, new1261) != "patch" {
		t.Error("1.26→1.26.1 should be patch (second field same, third differs)")
	}
}
