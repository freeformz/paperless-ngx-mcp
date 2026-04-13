package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
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

func TestDocumentDownloadContextCancellation(t *testing.T) {
	// Cancel context after first download completes — remaining should report context error
	var started atomic.Int32

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	rh := newRouteHandler(t)
	for i := 1; i <= 5; i++ {
		id := i
		rh.Handle("GET", fmt.Sprintf("/api/documents/%d/download/", id), func(w http.ResponseWriter, r *http.Request) {
			n := started.Add(1)
			if n >= 2 {
				cancel()
				// Give workers time to observe cancellation
				time.Sleep(20 * time.Millisecond)
			}
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="doc%d.pdf"`, id))
			fmt.Fprintf(w, "content-%d", id)
		})
	}

	client := testClientAndServer(t, rh)
	dl := testDownloader(t, 1) // concurrency 1 to serialize downloads

	handler := handleDocumentDownload(client, dl)
	result, err := handler(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"ids": "[1,2,3,4,5]",
			},
		},
	})
	if err != nil {
		t.Fatalf("handler returned error: %s", err)
	}
	assertNotError(t, result)

	m := resultJSON(t, result)
	results := m["results"].([]any)
	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}

	// At least one result should have a context cancellation error
	var cancelErrors int
	for _, r := range results {
		rm := r.(map[string]any)
		if e, ok := rm["error"].(string); ok && e != "" {
			cancelErrors++
			if !strings.Contains(e, "cancel") {
				t.Errorf("expected cancel error, got: %s", e)
			}
		}
	}
	if cancelErrors == 0 {
		t.Error("expected at least one context cancellation error")
	}
}

// content mode tests (base64 inline)

func TestDocumentDownloadContentMode(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/download/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="invoice.pdf"`)
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("%PDF-1.4 fake content"))
	})

	client := testClientAndServer(t, rh)
	dl := testDownloader(t, 5)

	result := callTool(t, handleDocumentDownload(client, dl), map[string]any{
		"ids":     "[1]",
		"content": true,
	})
	assertNotError(t, result)

	m := resultJSON(t, result)

	// Should not include download_dir in content mode
	if m["download_dir"] != nil {
		t.Errorf("unexpected download_dir in content mode")
	}

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

	// Should have content, not path
	if r0["path"] != nil {
		t.Errorf("unexpected path in content mode: %v", r0["path"])
	}

	content := r0["content"].(string)
	decoded, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		t.Fatalf("decode base64: %s", err)
	}
	if string(decoded) != "%PDF-1.4 fake content" {
		t.Errorf("decoded content = %q", string(decoded))
	}

	if r0["content_type"] != "application/pdf" {
		t.Errorf("content_type = %v, want application/pdf", r0["content_type"])
	}
	if r0["filename"] != "invoice.pdf" {
		t.Errorf("filename = %v, want invoice.pdf", r0["filename"])
	}
}

func TestDocumentDownloadContentModeMultiple(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/download/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="doc1.pdf"`)
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("doc1-content"))
	})
	rh.Handle("GET", "/api/documents/2/download/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="doc2.pdf"`)
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("doc2-content"))
	})

	client := testClientAndServer(t, rh)
	dl := testDownloader(t, 5)

	result := callTool(t, handleDocumentDownload(client, dl), map[string]any{
		"ids":     "[1,2]",
		"content": true,
	})
	assertNotError(t, result)

	m := resultJSON(t, result)
	results := m["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	for i, r := range results {
		rm := r.(map[string]any)
		if rm["content"] == nil || rm["content"] == "" {
			t.Errorf("result %d: missing content", i)
		}
		if rm["path"] != nil {
			t.Errorf("result %d: unexpected path in content mode", i)
		}
	}
}

func TestDocumentDownloadDiskModeReturnsMetadata(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/download/", func(w http.ResponseWriter, r *http.Request) {
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
	r0 := results[0].(map[string]any)

	if r0["content_type"] != "application/pdf" {
		t.Errorf("content_type = %v, want application/pdf", r0["content_type"])
	}
	if r0["filename"] != "invoice.pdf" {
		t.Errorf("filename = %v, want invoice.pdf", r0["filename"])
	}
	if r0["path"] == nil || r0["path"] == "" {
		t.Error("expected path in disk mode")
	}
	if r0["content"] != nil {
		t.Errorf("unexpected content in disk mode")
	}
}

func TestDocumentDownloadDiskModeDirRecreated(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/download/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="doc.pdf"`)
		w.Write([]byte("data"))
	})

	client := testClientAndServer(t, rh)
	dl := testDownloader(t, 5)

	// Remove the download dir to simulate it disappearing
	os.RemoveAll(dl.Dir())

	result := callTool(t, handleDocumentDownload(client, dl), map[string]any{
		"ids": "[1]",
	})
	assertNotError(t, result)

	m := resultJSON(t, result)
	results := m["results"].([]any)
	r0 := results[0].(map[string]any)
	if r0["error"] != nil {
		t.Errorf("unexpected error: %v", r0["error"])
	}
	if r0["path"] == nil || r0["path"] == "" {
		t.Error("expected path")
	}
}

func TestDocumentDownloadContentModeExceedsMaxSize(t *testing.T) {
	// Generate content larger than maxInlineSize
	largeBody := make([]byte, maxInlineSize+1)
	for i := range largeBody {
		largeBody[i] = 'x'
	}

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/download/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="huge.pdf"`)
		w.Header().Set("Content-Type", "application/pdf")
		w.Write(largeBody)
	})

	client := testClientAndServer(t, rh)
	dl := testDownloader(t, 5)

	result := callTool(t, handleDocumentDownload(client, dl), map[string]any{
		"ids":     "[1]",
		"content": true,
	})
	assertNotError(t, result)

	m := resultJSON(t, result)
	results := m["results"].([]any)
	r0 := results[0].(map[string]any)
	errMsg, ok := r0["error"].(string)
	if !ok || errMsg == "" {
		t.Fatal("expected error for oversized document")
	}
	if !strings.Contains(errMsg, "exceeds maximum inline size") {
		t.Errorf("unexpected error: %s", errMsg)
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
	entries, err := os.ReadDir(dl.Dir())
	if err != nil {
		t.Fatalf("read dir: %s", err)
	}
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
