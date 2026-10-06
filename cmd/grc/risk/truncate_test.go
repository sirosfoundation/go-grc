package risk

import (
	"testing"
	"unicode/utf8"
)

func TestTruncate_RuneSafe(t *testing.T) {
	in := "ÅÅÅÅÅÅÅÅÅÅÅÅÅÅÅÅÅÅÅÅÅÅÅÅ"
	got := truncate(in, 10)
	if !utf8.ValidString(got) {
		t.Fatalf("truncate produced invalid UTF-8: %q", got)
	}
	if got != "ÅÅÅÅÅÅÅ..." {
		t.Errorf("unexpected truncation: %q", got)
	}
	if truncate("short", 10) != "short" {
		t.Error("short strings must be unchanged")
	}
}
