package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestMCPPrintsUsageOnlyForFlagErrors(t *testing.T) {
	// Neither case gets as far as starting the server.
	t.Setenv("PAPERLESS_URL", "")
	for _, tc := range []struct {
		name      string
		args      []string
		wantErr   string // the error line, printed first
		wantUsage bool   // whether usage follows it
	}{
		{"missing PAPERLESS_URL", []string{"mcp"}, "Error: PAPERLESS_URL environment variable is required\n", false},
		{"unknown flag", []string{"mcp", "--nope"}, "Error: unknown flag: --nope\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// cobra prints the error to its err writer and usage to its out
			// writer. Both default to the process's stderr.
			var stderr bytes.Buffer
			cmd := rootCmd()
			cmd.SetArgs(tc.args)
			cmd.SetOut(&stderr)
			cmd.SetErr(&stderr)
			if err := cmd.Execute(); err == nil {
				t.Fatal("Execute succeeded, want an error")
			}

			rest, ok := strings.CutPrefix(stderr.String(), tc.wantErr)
			if !ok {
				t.Fatalf("stderr = %q, want it to start with %q", stderr.String(), tc.wantErr)
			}
			if tc.wantUsage && !strings.HasPrefix(rest, "Usage:") {
				t.Errorf("stderr after the error = %q, want usage", rest)
			}
			if !tc.wantUsage && rest != "" {
				t.Errorf("stderr after the error = %q, want nothing", rest)
			}
		})
	}
}
