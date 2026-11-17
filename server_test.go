package main

import (
	"os"
	"strings"
	"testing"
)

func TestNewServer(t *testing.T) {
	client := NewClient("http://localhost", "test-token")
	dl, err := NewDownloader(5, "")
	if err != nil {
		t.Fatalf("create downloader: %s", err)
	}
	t.Cleanup(func() { os.RemoveAll(dl.Dir()) })

	srv := NewServer(client, dl)
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
}

// Agents only see the server instructions and tool descriptions, not the
// README, so the saved view limits of ownership must be spelled out here.
func TestServerInstructionsCoverSavedViewOwnership(t *testing.T) {
	for _, want := range []string{
		"storage paths, and saved views created without an owner argument",
		"Saved views are not supported by bulk_edit_objects; fix an existing view with saved_view_update.",
		"Paperless-ngx 3.0.0 or later",
	} {
		if !strings.Contains(serverInstructions, want) {
			t.Errorf("server instructions lack %q", want)
		}
	}
}
