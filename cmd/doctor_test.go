package cmd

import (
	"encoding/json"
	"testing"
)

func TestSeverityString(t *testing.T) {
	tests := []struct {
		sev  severity
		want string
	}{
		{sevPass, "pass"},
		{sevInfo, "info"},
		{sevWarn, "warn"},
		{sevError, "error"},
		{sevSkipped, "skipped"},
	}
	for _, tc := range tests {
		if got := tc.sev.String(); got != tc.want {
			t.Errorf("severity(%d).String() = %q, want %q", int(tc.sev), got, tc.want)
		}
	}
}

func TestSeverityRank(t *testing.T) {
	if sevWarn.rank() <= sevPass.rank() {
		t.Error("warn should rank higher than pass")
	}
	if sevError.rank() <= sevWarn.rank() {
		t.Error("error should rank higher than warn")
	}
	if sevSkipped.rank() != 0 {
		t.Errorf("skipped rank = %d, want 0", sevSkipped.rank())
	}
}

func TestSeverityMarshalJSON(t *testing.T) {
	b, err := json.Marshal(sevError)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"error"` {
		t.Errorf("MarshalJSON(sevError) = %s, want %q", b, `"error"`)
	}
}
