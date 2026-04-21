package policy

import (
	"testing"
	"time"
)

func TestDecideReturnsNoActionsByDefault(t *testing.T) {
	now := time.Now()
	got := Decide(State{}, Config{}, now)
	if got != NoAction {
		t.Errorf("expected NoAction, got %v", got)
	}
}
