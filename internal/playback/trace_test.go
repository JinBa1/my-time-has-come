package playback

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/JinBa1/my-time-has-come/internal/config"
)

var updateGoldens = flag.Bool("update", false, "rewrite trace golden files")

func TestTraceGoldens(t *testing.T) {
	fixtures := []string{
		"baseline",
		"soft_inject",
		"hard_gate",
		// Task 3 appends: "seven_day_gate", "absence_grace",
		// "rollover_rearm", "monotonic_regression",
	}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			steps, err := Replay(
				[]string{filepath.Join("testdata", name+".jsonl")},
				config.Defaults(),
			)
			if err != nil {
				t.Fatalf("replay: %v", err)
			}
			got, err := json.MarshalIndent(Trace(steps), "", "  ")
			if err != nil {
				t.Fatalf("marshal trace: %v", err)
			}
			got = append(got, '\n')

			goldenPath := filepath.Join("testdata", "traces", name+".golden.json")
			if *updateGoldens {
				if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("read golden (run with -update to create): %v", err)
			}
			if string(got) != string(want) {
				t.Errorf("trace mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
			}
		})
	}
}
