package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// testDownloaderWithBase creates a Downloader with a configured base directory.
func testDownloaderWithBase(t *testing.T, baseDir string) *Downloader {
	t.Helper()
	dl, err := NewDownloader(5, baseDir)
	if err != nil {
		t.Fatalf("create downloader: %s", err)
	}
	t.Cleanup(func() { os.RemoveAll(dl.Dir()) })
	return dl
}

// pdfServer serves a fake PDF for document 1 with the given filename.
func pdfServer(t *testing.T, filename string) *Client {
	t.Helper()
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/download/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("%PDF-1.4 fake content"))
	})
	return testClientAndServer(t, rh)
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"invoice.pdf", "invoice.pdf"},
		{"/etc/passwd", "passwd"},
		{`..\..\evil.exe`, "evil.exe"},
		{"../../../escape.pdf", "escape.pdf"},
		{"..", ""},
		{".", ""},
		{"", ""},
		{"a:b\x00c.pdf", "a_b_c.pdf"},
		{`inv<oice>"x"|y?z*.pdf`, "inv_oice__x__y_z_.pdf"},
		{"  spaced.pdf  ", "spaced.pdf"},
	}
	for _, tt := range tests {
		if got := sanitizeFilename(tt.in); got != tt.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	// Overlong names are truncated but keep their extension.
	long := sanitizeFilename(strings.Repeat("x", 300) + ".pdf")
	if len(long) > 154 || !strings.HasSuffix(long, ".pdf") {
		t.Errorf("long name not truncated properly: %d chars, %q...", len(long), long[:20])
	}
}

func TestResolveDestDirNoBase(t *testing.T) {
	dl := testDownloaderWithBase(t, "")

	// Default goes to temp dir.
	dir, err := dl.ResolveDestDir("")
	if err != nil || dir != dl.Dir() {
		t.Errorf("ResolveDestDir(\"\") = %q, %v; want temp dir", dir, err)
	}

	// dest_dir without a configured base is an error.
	if _, err := dl.ResolveDestDir("sub"); err == nil {
		t.Error("expected error for dest_dir without base dir")
	}
}

func TestResolveDestDirWithBase(t *testing.T) {
	base := t.TempDir()
	dl := testDownloaderWithBase(t, base)

	// Default goes to the base dir.
	dir, err := dl.ResolveDestDir("")
	if err != nil || dir != dl.BaseDir() {
		t.Errorf("ResolveDestDir(\"\") = %q, %v; want base dir %q", dir, err, dl.BaseDir())
	}

	// Relative subdirectory is created inside the base.
	sub, err := dl.ResolveDestDir("scans/2026")
	if err != nil {
		t.Fatalf("resolve subdir: %s", err)
	}
	if !strings.HasPrefix(sub, dl.BaseDir()) {
		t.Errorf("subdir %q not inside base %q", sub, dl.BaseDir())
	}
	if fi, err := os.Stat(sub); err != nil || !fi.IsDir() {
		t.Errorf("subdir not created: %v", err)
	}

	// Escapes are rejected without creating anything.
	for _, esc := range []string{"..", "../outside", "sub/../../outside"} {
		if _, err := dl.ResolveDestDir(esc); err == nil {
			t.Errorf("expected error for escaping dest_dir %q", esc)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dl.BaseDir()), "outside")); !os.IsNotExist(err) {
		t.Error("escaping dest_dir created a directory outside the base")
	}

	// Absolute path outside the base is rejected.
	if _, err := dl.ResolveDestDir(os.TempDir()); err == nil {
		// os.TempDir() could theoretically contain base; only fail when it doesn't.
		if !strings.HasPrefix(dl.BaseDir(), os.TempDir()) {
			t.Error("expected error for absolute dest_dir outside base")
		}
	}
}

func TestResolveDestDirSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	dl := testDownloaderWithBase(t, base)

	// A symlink inside the base pointing outside must be rejected.
	link := filepath.Join(dl.BaseDir(), "sneaky")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("cannot create symlink: %s", err)
	}
	if _, err := dl.ResolveDestDir("sneaky"); err == nil {
		t.Error("expected error for symlink escaping the base dir")
	}

	// A path THROUGH the symlink must be rejected before MkdirAll runs —
	// otherwise MkdirAll would follow the link and create dirs outside base.
	if _, err := dl.ResolveDestDir("sneaky/sub"); err == nil {
		t.Error("expected error for path through an escaping symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "sub")); !os.IsNotExist(err) {
		t.Error("ResolveDestDir created a directory outside the base via a symlink")
	}
}

func TestDownloadFilenameWithoutExtensionGetsOne(t *testing.T) {
	base := t.TempDir()
	dl := testDownloaderWithBase(t, base)

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/download/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="invoice"`)
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("%PDF-1.4 fake content"))
	})
	client := testClientAndServer(t, rh)

	result := callTool(t, handleDocumentDownload(client, dl), map[string]any{"ids": "[1]"})
	assertNotError(t, result)

	m := resultJSON(t, result)
	r0 := m["results"].([]any)[0].(map[string]any)
	if name := filepath.Base(r0["path"].(string)); name != "invoice.pdf" {
		t.Errorf("filename = %q, want invoice.pdf (extension from Content-Type)", name)
	}
}

func TestDownloadToBaseDirPreservesFilenameAndSkipsTracking(t *testing.T) {
	base := t.TempDir()
	dl := testDownloaderWithBase(t, base)
	client := pdfServer(t, "invoice.pdf")

	result := callTool(t, handleDocumentDownload(client, dl), map[string]any{"ids": "[1]"})
	assertNotError(t, result)

	m := resultJSON(t, result)
	r0 := m["results"].([]any)[0].(map[string]any)
	path := r0["path"].(string)
	if filepath.Base(path) != "invoice.pdf" {
		t.Errorf("filename = %q, want invoice.pdf", filepath.Base(path))
	}
	if filepath.Dir(path) != dl.BaseDir() {
		t.Errorf("path %q not in base dir %q", path, dl.BaseDir())
	}
	if m["download_dir"] != dl.BaseDir() {
		t.Errorf("download_dir = %v, want %q", m["download_dir"], dl.BaseDir())
	}

	// Files in the base dir are not tracked and survive cleanup_downloads.
	if tracked := dl.TrackedFiles(); len(tracked) != 0 {
		t.Errorf("expected no tracked files, got %v", tracked)
	}
	cleanupResult := callTool(t, handleCleanupDownloads(dl), map[string]any{})
	assertNotError(t, cleanupResult)
	if _, err := os.Stat(path); err != nil {
		t.Errorf("cleanup_downloads removed a base-dir file: %s", err)
	}
}

func TestDownloadDeduplicatesFilenames(t *testing.T) {
	base := t.TempDir()
	dl := testDownloaderWithBase(t, base)
	client := pdfServer(t, "invoice.pdf")

	for range 2 {
		result := callTool(t, handleDocumentDownload(client, dl), map[string]any{"ids": "[1]"})
		assertNotError(t, result)
	}

	entries, err := os.ReadDir(dl.BaseDir())
	if err != nil {
		t.Fatalf("read base dir: %s", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 files, got %v", names)
	}
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["invoice.pdf"] || !found["invoice-1.pdf"] {
		t.Errorf("expected invoice.pdf and invoice-1.pdf, got %v", names)
	}
}

func TestDownloadDestDirSubdirectory(t *testing.T) {
	base := t.TempDir()
	dl := testDownloaderWithBase(t, base)
	client := pdfServer(t, "doc.pdf")

	result := callTool(t, handleDocumentDownload(client, dl), map[string]any{
		"ids":      "[1]",
		"dest_dir": "taxes",
	})
	assertNotError(t, result)

	m := resultJSON(t, result)
	r0 := m["results"].([]any)[0].(map[string]any)
	path := r0["path"].(string)
	if filepath.Dir(path) != filepath.Join(dl.BaseDir(), "taxes") {
		t.Errorf("path %q not in taxes subdir", path)
	}
}

func TestDownloadDestDirWithoutBaseErrors(t *testing.T) {
	dl := testDownloaderWithBase(t, "")
	client := NewClient("http://unused", "unused")

	result := callTool(t, handleDocumentDownload(client, dl), map[string]any{
		"ids":      "[1]",
		"dest_dir": "anywhere",
	})
	assertIsError(t, result)
	if msg := resultText(t, result); !strings.Contains(msg, "PAPERLESS_MCP_DOWNLOAD_DIR") {
		t.Errorf("error %q should mention the env var", msg)
	}
}

func TestDownloadDestDirWithContentModeErrors(t *testing.T) {
	base := t.TempDir()
	dl := testDownloaderWithBase(t, base)
	client := NewClient("http://unused", "unused")

	result := callTool(t, handleDocumentDownload(client, dl), map[string]any{
		"ids":      "[1]",
		"dest_dir": "sub",
		"content":  true,
	})
	assertIsError(t, result)
}

// content=true image handling

func TestDownloadContentModeImageBecomesImageBlock(t *testing.T) {
	pngData := makeTestPNG(t, 100, 80)
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/thumb/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngData)
	})
	client := testClientAndServer(t, rh)
	dl := testDownloaderWithBase(t, "")

	result := callTool(t, handleDocumentDownload(client, dl), map[string]any{
		"ids":     "[1]",
		"variant": "thumbnail",
		"content": true,
	})
	assertNotError(t, result)

	if len(result.Content) != 2 {
		t.Fatalf("expected 2 content blocks (text + image), got %d", len(result.Content))
	}
	ic, ok := result.Content[1].(mcp.ImageContent)
	if !ok {
		t.Fatalf("expected ImageContent, got %T", result.Content[1])
	}
	if ic.MIMEType != "image/png" {
		t.Errorf("mime = %s, want image/png (small image passes through)", ic.MIMEType)
	}

	// The JSON summary must not contain base64 for the image.
	m := resultJSON(t, result)
	r0 := m["results"].([]any)[0].(map[string]any)
	if r0["content"] != nil {
		t.Error("image result should not carry base64 content in the text block")
	}
	if r0["note"] == nil {
		t.Error("image result should note it was returned as an image block")
	}
}

func TestDownloadContentModeOversizedImageDownscaled(t *testing.T) {
	// Longest edge over the cap forces a decode + downscale + JPEG re-encode.
	pngData := makeTestPNG(t, maxImageEdge+500, 200)
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/thumb/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(pngData)
	})
	client := testClientAndServer(t, rh)
	dl := testDownloaderWithBase(t, "")

	result := callTool(t, handleDocumentDownload(client, dl), map[string]any{
		"ids":     "[1]",
		"variant": "thumbnail",
		"content": true,
	})
	assertNotError(t, result)

	ic, ok := result.Content[1].(mcp.ImageContent)
	if !ok {
		t.Fatalf("expected ImageContent, got %T", result.Content[1])
	}
	if ic.MIMEType != "image/jpeg" {
		t.Errorf("mime = %s, want image/jpeg after normalization", ic.MIMEType)
	}
	img := decodeResultImage(t, ic)
	if edge := max(img.Bounds().Dx(), img.Bounds().Dy()); edge > maxImageEdge {
		t.Errorf("longest edge %d exceeds cap %d", edge, maxImageEdge)
	}
}

func TestDownloadContentModeMixedBatch(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/download/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("%PDF-1.4 fake"))
	})
	rh.Handle("GET", "/api/documents/2/download/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(makeTestPNG(t, 50, 50))
	})
	rh.Handle("GET", "/api/documents/999/download/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	client := testClientAndServer(t, rh)
	dl := testDownloaderWithBase(t, "")

	result := callTool(t, handleDocumentDownload(client, dl), map[string]any{
		"ids":     "[1,2,999]",
		"content": true,
	})
	assertNotError(t, result)

	// One image block after the text summary.
	if len(result.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(result.Content))
	}

	m := resultJSON(t, result)
	results := m["results"].([]any)
	r0 := results[0].(map[string]any)
	if r0["content"] == nil {
		t.Error("PDF result should keep base64 content")
	}
	if note, _ := r0["note"].(string); !strings.Contains(note, "document_page_image") {
		t.Errorf("PDF result note = %q, should point to document_page_image", note)
	}
	r1 := results[1].(map[string]any)
	if r1["content"] != nil || r1["note"] == nil {
		t.Error("image result should be delivered as an image block with a note")
	}
	r2 := results[2].(map[string]any)
	if r2["error"] == nil {
		t.Error("failed download should report its error")
	}
}
