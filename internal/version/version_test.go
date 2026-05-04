package version

import (
	"strings"
	"testing"
)

func TestFormat(t *testing.T) {
	info := Info{
		Version:   "v0.2.0",
		Commit:    "abc1234",
		BuildDate: "2026-05-03T12:00:00Z",
	}

	got := info.Format()
	for _, want := range []string{
		"mthc v0.2.0",
		"commit: abc1234",
		"built: 2026-05-03T12:00:00Z",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("Format() = %q, want substring %q", got, want)
		}
	}
}

func TestCurrentUsesPackageVariables(t *testing.T) {
	oldVersion, oldCommit, oldBuildDate := Version, Commit, BuildDate
	Version = "v9.9.9-test"
	Commit = "deadbeef"
	BuildDate = "2026-05-03T13:00:00Z"
	t.Cleanup(func() {
		Version, Commit, BuildDate = oldVersion, oldCommit, oldBuildDate
	})

	got := Current()
	if got.Version != "v9.9.9-test" || got.Commit != "deadbeef" || got.BuildDate != "2026-05-03T13:00:00Z" {
		t.Fatalf("Current() = %#v", got)
	}
}
