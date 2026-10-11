package probe

import "testing"

func TestEcho(t *testing.T) {
	if got := Echo("x"); got != "x" {
		t.Errorf("Echo(%q) = %q, want %q", "x", got, "x")
	}
	if got := Echo(""); got != "" {
		t.Errorf("Echo(%q) = %q, want %q", "", got, "")
	}
}
