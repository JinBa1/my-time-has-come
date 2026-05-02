package cmd

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestExecuteRootHelpAliases(t *testing.T) {
	for _, args := range [][]string{
		{"mthc"},
		{"mthc", "--help"},
		{"mthc", "-h"},
		{"mthc", "help"},
	} {
		t.Run(strings.Join(args[1:], "_"), func(t *testing.T) {
			stdout, err := executeWithArgs(t, args)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			assertContains(t, stdout, "Usage: mthc <command> [options]")
			assertContains(t, stdout, "Commands:")
			assertContains(t, stdout, "install")
			assertContains(t, stdout, "Internal commands:")
			assertContains(t, stdout, "statusline-shim")
		})
	}
}

func TestExecuteCommandHelp(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "help install",
			args: []string{"mthc", "help", "install"},
			want: []string{"Usage: mthc install [--force]", "--force"},
		},
		{
			name: "install --help",
			args: []string{"mthc", "install", "--help"},
			want: []string{"Usage: mthc install [--force]", "--force"},
		},
		{
			name: "playback --help",
			args: []string{"mthc", "playback", "--help"},
			want: []string{"Usage: mthc playback replay [--config-from-recording] <file|dir>...", "--config-from-recording"},
		},
		{
			name: "shim help does not execute",
			args: []string{"mthc", "hook-shim", "--help"},
			want: []string{"Usage: mthc hook-shim", "Internal command"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, err := executeWithArgs(t, tc.args)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			for _, want := range tc.want {
				assertContains(t, stdout, want)
			}
		})
	}
}

func TestExecuteHelpUnknownCommandErrors(t *testing.T) {
	for _, args := range [][]string{
		{"mthc", "bogus"},
		{"mthc", "help", "bogus"},
	} {
		t.Run(strings.Join(args[1:], "_"), func(t *testing.T) {
			stdout, err := executeWithArgs(t, args)
			if err == nil {
				t.Fatal("Execute() error = nil, want error")
			}
			if !strings.Contains(err.Error(), "unknown command: bogus") {
				t.Fatalf("Execute() error = %q, want unknown command", err)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
		})
	}
}

func executeWithArgs(t *testing.T, args []string) (string, error) {
	t.Helper()

	t.Setenv("HOME", t.TempDir())

	oldArgs := os.Args
	oldStdout := os.Stdout
	t.Cleanup(func() {
		os.Args = oldArgs
		os.Stdout = oldStdout
	})

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Args = append([]string(nil), args...)
	os.Stdout = writer

	executeErr := Execute()
	if err := writer.Close(); err != nil {
		t.Fatalf("closing stdout writer: %v", err)
	}
	output, readErr := io.ReadAll(reader)
	if err := reader.Close(); err != nil {
		t.Fatalf("closing stdout reader: %v", err)
	}
	if readErr != nil {
		t.Fatalf("reading stdout: %v", readErr)
	}

	return string(output), executeErr
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()

	if !strings.Contains(got, want) {
		t.Fatalf("output = %q, want substring %q", got, want)
	}
}
