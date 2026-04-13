package main

import (
	"os"
	"testing"
)

func TestNewServer(t *testing.T) {
	client := NewClient("http://localhost", "test-token")
	dl, err := NewDownloader(5)
	if err != nil {
		t.Fatalf("create downloader: %s", err)
	}
	t.Cleanup(func() { os.RemoveAll(dl.Dir()) })

	srv := NewServer(client, dl)
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
}
