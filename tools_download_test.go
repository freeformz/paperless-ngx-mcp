package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testDownloader(t *testing.T, concurrency int) *Downloader {
	t.Helper()
	dl, err := NewDownloader(concurrency)
	if err != nil {
		t.Fatalf("create downloader: %s", err)
	}
	t.Cleanup(func() { os.RemoveAll(dl.Dir()) })
	return dl
}

func TestDocumentDownloadSingle(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/download/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Token test-token" {
			t.Errorf("missing auth header")
		}
		if r.URL.Query().Get("original") != "" {
			t.Errorf("unexpected original param for archived variant")
		}
		w.Header().Set("Content-Disposition", `attachment; filename="invoice.pdf"`)
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("%PDF-1.4 fake content"))
	})

	client := testClientAndServer(t, rh)
	dl := testDownloader(t, 5)

	result := callTool(t, handleDocumentDownload(client, dl), map[string]any{
		"ids": "[1]",
	})
	assertNotError(t, result)

	m := resultJSON(t, result)
	results := m["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r0 := results[0].(map[string]any)
	if r0["id"] != float64(1) {
		t.Errorf("id = %v, want 1", r0["id"])
	}
	if r0["error"] != nil {
		t.Errorf("unexpected error: %v", r0["error"])
	}

	path := r0["path"].(string)
	if !strings.HasPrefix(path, dl.Dir()) {
		t.Errorf("path %q not in download dir %q", path, dl.Dir())
	}
	if !strings.HasSuffix(path, ".pdf") {
		t.Errorf("path %q doesn't end in .pdf", path)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read downloaded file: %s", err)
	}
	if string(content) != "%PDF-1.4 fake content" {
		t.Errorf("content = %q", string(content))
	}
}

func TestDocumentDownloadOriginalVariant(t *testing.T) {
	var capturedOriginal string
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/download/", func(w http.ResponseWriter, r *http.Request) {
		capturedOriginal = r.URL.Query().Get("original")
		w.Header().Set("Content-Disposition", `attachment; filename="scan.png"`)
		w.Write([]byte("png data"))
	})

	client := testClientAndServer(t, rh)
	dl := testDownloader(t, 5)

	result := callTool(t, handleDocumentDownload(client, dl), map[string]any{
		"ids":     "[1]",
		"variant": "original",
	})
	assertNotError(t, result)

	if capturedOriginal != "true" {
		t.Errorf("original param = %q, want true", capturedOriginal)
	}
}

func TestDocumentDownloadThumbnailVariant(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/thumb/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/webp")
		w.Write([]byte("webp data"))
	})

	client := testClientAndServer(t, rh)
	dl := testDownloader(t, 5)

	result := callTool(t, handleDocumentDownload(client, dl), map[string]any{
		"ids":     "[1]",
		"variant": "thumbnail",
	})
	assertNotError(t, result)

	m := resultJSON(t, result)
	results := m["results"].([]any)
	r0 := results[0].(map[string]any)
	if r0["error"] != nil {
		t.Errorf("unexpected error: %v", r0["error"])
	}
}

func TestDocumentDownloadMultipleWithPartialFailure(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/download/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="doc1.pdf"`)
		w.Write([]byte("doc1"))
	})
	rh.Handle("GET", "/api/documents/999/download/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	rh.Handle("GET", "/api/documents/2/download/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="doc2.pdf"`)
		w.Write([]byte("doc2"))
	})

	client := testClientAndServer(t, rh)
	dl := testDownloader(t, 5)

	result := callTool(t, handleDocumentDownload(client, dl), map[string]any{
		"ids": "[1, 999, 2]",
	})
	assertNotError(t, result)

	m := resultJSON(t, result)
	results := m["results"].([]any)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Results are ordered by input order
	r0 := results[0].(map[string]any)
	if r0["id"] != float64(1) {
		t.Errorf("r0 id = %v, want 1", r0["id"])
	}
	if r0["path"] == nil || r0["path"] == "" {
		t.Error("r0 should have a path")
	}

	r1 := results[1].(map[string]any)
	if r1["id"] != float64(999) {
		t.Errorf("r1 id = %v, want 999", r1["id"])
	}
	if r1["error"] == nil || r1["error"] == "" {
		t.Error("r1 should have an error")
	}

	r2 := results[2].(map[string]any)
	if r2["id"] != float64(2) {
		t.Errorf("r2 id = %v, want 2", r2["id"])
	}
	if r2["path"] == nil || r2["path"] == "" {
		t.Error("r2 should have a path")
	}
}

func TestDocumentDownloadInvalidVariant(t *testing.T) {
	client := NewClient("http://unused", "unused")
	dl := testDownloader(t, 5)

	result := callTool(t, handleDocumentDownload(client, dl), map[string]any{
		"ids":     "[1]",
		"variant": "bogus",
	})
	assertIsError(t, result)
}

func TestDocumentDownloadEmptyIDs(t *testing.T) {
	client := NewClient("http://unused", "unused")
	dl := testDownloader(t, 5)

	result := callTool(t, handleDocumentDownload(client, dl), map[string]any{
		"ids": "[]",
	})
	assertIsError(t, result)
}

func TestDocumentDownloadInvalidIDsJSON(t *testing.T) {
	client := NewClient("http://unused", "unused")
	dl := testDownloader(t, 5)

	result := callTool(t, handleDocumentDownload(client, dl), map[string]any{
		"ids": "not json",
	})
	assertIsError(t, result)
}

func TestDocumentDownloadMissingIDs(t *testing.T) {
	client := NewClient("http://unused", "unused")
	dl := testDownloader(t, 5)

	result := callTool(t, handleDocumentDownload(client, dl), map[string]any{})
	assertIsError(t, result)
}

func TestDocumentDownloadConcurrencyRespected(t *testing.T) {
	// Download 10 documents with concurrency 2 — verify max in-flight never exceeds 2
	var (
		inflight    atomic.Int32
		maxInflight atomic.Int32
	)

	rh := newRouteHandler(t)
	for i := 1; i <= 10; i++ {
		id := i
		rh.Handle("GET", fmt.Sprintf("/api/documents/%d/download/", id), func(w http.ResponseWriter, r *http.Request) {
			cur := inflight.Add(1)
			// Update max observed concurrency
			for {
				old := maxInflight.Load()
				if cur <= old || maxInflight.CompareAndSwap(old, cur) {
					break
				}
			}
			// Small sleep to increase overlap window
			time.Sleep(10 * time.Millisecond)
			inflight.Add(-1)

			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="doc%d.pdf"`, id))
			fmt.Fprintf(w, "content-%d", id)
		})
	}

	client := testClientAndServer(t, rh)
	dl := testDownloader(t, 2)

	result := callTool(t, handleDocumentDownload(client, dl), map[string]any{
		"ids": "[1,2,3,4,5,6,7,8,9,10]",
	})
	assertNotError(t, result)

	m := resultJSON(t, result)
	results := m["results"].([]any)
	if len(results) != 10 {
		t.Fatalf("expected 10 results, got %d", len(results))
	}
	for i, r := range results {
		rm := r.(map[string]any)
		if rm["error"] != nil {
			t.Errorf("result %d: unexpected error: %v", i, rm["error"])
		}
	}

	if observed := maxInflight.Load(); observed > 2 {
		t.Errorf("max in-flight = %d, want <= 2", observed)
	}
}

// cleanup_downloads tests

func TestCleanupDownloadsAll(t *testing.T) {
	dl := testDownloader(t, 5)

	// Create some files in the download dir
	for _, name := range []string{"a.pdf", "b.pdf", "c.txt"} {
		p := filepath.Join(dl.Dir(), name)
		if err := os.WriteFile(p, []byte("data"), 0o600); err != nil {
			t.Fatalf("write file: %s", err)
		}
		dl.TrackFile(p)
	}

	result := callTool(t, handleCleanupDownloads(dl), map[string]any{})
	assertNotError(t, result)

	m := resultJSON(t, result)
	if m["removed_count"] != float64(3) {
		t.Errorf("removed_count = %v, want 3", m["removed_count"])
	}

	// Directory should be empty
	entries, _ := os.ReadDir(dl.Dir())
	if len(entries) != 0 {
		t.Errorf("expected empty dir, got %d entries", len(entries))
	}

	// Tracked files should be empty
	if tracked := dl.TrackedFiles(); len(tracked) != 0 {
		t.Errorf("expected no tracked files, got %d", len(tracked))
	}
}

func TestCleanupDownloadsSpecificFiles(t *testing.T) {
	dl := testDownloader(t, 5)

	paths := make([]string, 3)
	for i, name := range []string{"a.pdf", "b.pdf", "c.txt"} {
		p := filepath.Join(dl.Dir(), name)
		if err := os.WriteFile(p, []byte("data"), 0o600); err != nil {
			t.Fatalf("write file: %s", err)
		}
		dl.TrackFile(p)
		paths[i] = p
	}

	// Only remove first two
	toRemove, _ := json.Marshal(paths[:2])
	result := callTool(t, handleCleanupDownloads(dl), map[string]any{
		"files": string(toRemove),
	})
	assertNotError(t, result)

	m := resultJSON(t, result)
	if m["removed_count"] != float64(2) {
		t.Errorf("removed_count = %v, want 2", m["removed_count"])
	}

	// Third file should still exist
	if _, err := os.Stat(paths[2]); err != nil {
		t.Errorf("expected third file to still exist: %s", err)
	}
}

func TestCleanupDownloadsRejectsOutsideDir(t *testing.T) {
	dl := testDownloader(t, 5)

	// Try to remove a file outside the download dir
	outsideFile := filepath.Join(os.TempDir(), "should-not-be-removed.txt")
	toRemove, _ := json.Marshal([]string{outsideFile})
	result := callTool(t, handleCleanupDownloads(dl), map[string]any{
		"files": string(toRemove),
	})
	assertNotError(t, result) // not an error, but the file should be in "failed"

	m := resultJSON(t, result)
	if m["failed_count"] != float64(1) {
		t.Errorf("failed_count = %v, want 1", m["failed_count"])
	}
}

func TestCleanupDownloadsInvalidJSON(t *testing.T) {
	dl := testDownloader(t, 5)

	result := callTool(t, handleCleanupDownloads(dl), map[string]any{
		"files": "not json",
	})
	assertIsError(t, result)
}

func TestCleanupDownloadsEmptyArray(t *testing.T) {
	dl := testDownloader(t, 5)

	result := callTool(t, handleCleanupDownloads(dl), map[string]any{
		"files": "[]",
	})
	assertIsError(t, result)
}
