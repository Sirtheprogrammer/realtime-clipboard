package api

import (
	"strings"
	"testing"
)

func TestNewRoomCodeShape(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 200; i++ {
		code := NewRoomCode()
		if got, ok := NormalizeRoomCode(code); !ok || got != code {
			t.Fatalf("generated code %q did not survive normalization (ok=%v, got=%q)", code, ok, got)
		}
		if len(code) != 9 || code[4] != '-' {
			t.Fatalf("unexpected code shape: %q", code)
		}
		if seen[code] {
			t.Fatalf("duplicate code within 200 draws: %q", code)
		}
		seen[code] = true
	}
}

func TestNormalizeRoomCode(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"abcd-efgh", "abcd-efgh", true},
		{"  ABCD-EFGH  ", "abcd-efgh", true},
		{"-abcd-", "abcd", true},
		{"abc", "", false},                       // too short
		{"abcd efgh", "", false},                 // space
		{"abcd/efgh", "", false},                 // path separator
		{"../../etc/passwd", "", false},          // traversal
		{strings.Repeat("a", 65), "", false},     // too long
		{"", "", false},
	}
	for _, tc := range tests {
		got, ok := NormalizeRoomCode(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("NormalizeRoomCode(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestClassifyText(t *testing.T) {
	tests := map[string]string{
		"https://example.com/a?b=c": "link",
		"http://localhost:8080":     "link",
		"example.com":               "text", // no scheme, so not treated as a link
		"ftp://example.com":         "text",
		"look at https://x.com":     "text", // prose containing a URL stays text
		"just some words":           "text",
		"":                          "text",
	}
	for in, want := range tests {
		if got := classifyText(in); got != want {
			t.Errorf("classifyText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeDevice(t *testing.T) {
	if got := sanitizeDevice("Windows \x00· Chrome\n"); got != "Windows · Chrome" {
		t.Errorf("control characters survived: %q", got)
	}
	if got := sanitizeDevice(strings.Repeat("x", 100)); len(got) != 48 {
		t.Errorf("long name not truncated: %d chars", len(got))
	}
	if got := sanitizeDevice("   "); got == "" {
		t.Error("blank name should fall back to a generated one")
	}
}
