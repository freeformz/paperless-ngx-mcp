package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestMCPPrintsUsageOnlyForFlagErrors(t *testing.T) {
	// No case gets as far as serving. If one did, the cancelled context would
	// stop the server before it read stdin or wrote stdout.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		name      string
		args      []string
		url       string // PAPERLESS_URL; PAPERLESS_TOKEN is always unset
		wantErr   string // the error line, printed first
		wantUsage bool   // whether usage follows it
	}{
		{"missing PAPERLESS_URL", []string{"mcp"}, "", "Error: PAPERLESS_URL environment variable is required\n", false},
		{"missing PAPERLESS_TOKEN", []string{"mcp"}, "http://127.0.0.1:9", "Error: PAPERLESS_TOKEN environment variable is required\n", false},
		{"unknown flag", []string{"mcp", "--nope"}, "", "Error: unknown flag: --nope\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PAPERLESS_URL", tc.url)
			t.Setenv("PAPERLESS_TOKEN", "")
			// cobra prints the error to its err writer and usage to its out
			// writer. Both default to the process's stderr.
			var stderr bytes.Buffer
			cmd := rootCmd()
			cmd.SetArgs(tc.args)
			cmd.SetOut(&stderr)
			cmd.SetErr(&stderr)
			if err := cmd.ExecuteContext(ctx); err == nil {
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
