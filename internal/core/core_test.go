package core

import "testing"

func TestResolveHandoffPathNoCollision(t *testing.T) {
	result := ResolveHandoffPath("/tmp/handoff.md", nil)
	if result != "/tmp/handoff.md" {
		t.Errorf("expected /tmp/handoff.md, got %q", result)
	}
}

func TestResolveHandoffPathSingleCollision(t *testing.T) {
	existing := []string{"/tmp/handoff.md"}
	result := ResolveHandoffPath("/tmp/handoff.md", existing)
	if result != "/tmp/handoff-2.md" {
		t.Errorf("expected /tmp/handoff-2.md, got %q", result)
	}
}

func TestResolveHandoffPathMultipleCollisions(t *testing.T) {
	existing := []string{"/tmp/handoff.md", "/tmp/handoff-2.md"}
	result := ResolveHandoffPath("/tmp/handoff.md", existing)
	if result != "/tmp/handoff-3.md" {
		t.Errorf("expected /tmp/handoff-3.md, got %q", result)
	}
}
