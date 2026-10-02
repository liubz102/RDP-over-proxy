package model

import "testing"

func TestNewID(t *testing.T) {
	a, b := NewID(), NewID()
	if len(a) != 16 || !ValidID(a) {
		t.Fatalf("NewID() = %q, want 16 lowercase hex digits", a)
	}
	if a == b {
		t.Fatalf("two NewID calls returned the same ID %q", a)
	}
	if a == DirectProxyID {
		t.Fatal("NewID must never produce the built-in direct proxy's ID")
	}
}

func TestValidID(t *testing.T) {
	for _, id := range []string{"a", "0123456789abcdef", DirectProxyID, "0123456789012345678901234567890123456789012345678901234567890123"} {
		if !ValidID(id) {
			t.Errorf("ValidID(%q) = false", id)
		}
	}
	for _, id := range []string{"", "ABC", "a-b", "a b", "a.json", "..", `a\b`, "é",
		"01234567890123456789012345678901234567890123456789012345678901234"} {
		if ValidID(id) {
			t.Errorf("ValidID(%q) = true", id)
		}
	}
}
