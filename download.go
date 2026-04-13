package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Downloader manages document file downloads into a per-instance temp directory.
type Downloader struct {
	dir         string
	concurrency int
	mu          sync.Mutex // protects file tracking
	files       map[string]struct{}
}

// NewDownloader creates a Downloader with a unique temp directory under os.TempDir().
// The directory is created immediately. The caller should remove it when finished,
// for example with os.RemoveAll(d.Dir()).
func NewDownloader(concurrency int) (*Downloader, error) {
	if concurrency < 1 {
		return nil, fmt.Errorf("concurrency must be >= 1, got %d", concurrency)
	}
	dir, err := os.MkdirTemp("", "paperless-ngx-mcp-")
	if err != nil {
		return nil, fmt.Errorf("create download dir: %w", err)
	}
	return &Downloader{
		dir:         dir,
		concurrency: concurrency,
		files:       make(map[string]struct{}),
	}, nil
}

// Dir returns the instance download directory path.
func (d *Downloader) Dir() string {
	return d.dir
}

// Concurrency returns the max parallel download limit.
func (d *Downloader) Concurrency() int {
	return d.concurrency
}

// TrackFile records a file path as managed by this downloader.
func (d *Downloader) TrackFile(path string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.files[path] = struct{}{}
}

// UntrackFile removes a file path from tracking.
func (d *Downloader) UntrackFile(path string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.files, path)
}

// TrackedFiles returns a copy of all tracked file paths.
func (d *Downloader) TrackedFiles() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	paths := make([]string, 0, len(d.files))
	for p := range d.files {
		paths = append(paths, p)
	}
	return paths
}

// CleanupAll removes all files in the download directory.
func (d *Downloader) CleanupAll() ([]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	entries, err := os.ReadDir(d.dir)
	if err != nil {
		return nil, fmt.Errorf("read download dir: %w", err)
	}

	var removed []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		p := filepath.Join(d.dir, e.Name())
		if err := os.Remove(p); err != nil {
			return removed, fmt.Errorf("remove %s: %w", e.Name(), err)
		}
		delete(d.files, p)
		removed = append(removed, p)
	}
	return removed, nil
}

// CleanupFiles removes specific files, validating each is inside the download directory.
func (d *Downloader) CleanupFiles(paths []string) ([]string, []string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var removed, failed []string
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: invalid path: %s", p, err))
			continue
		}
		if !strings.HasPrefix(abs, d.dir+string(filepath.Separator)) {
			failed = append(failed, fmt.Sprintf("%s: not inside download directory", p))
			continue
		}
		if err := os.Remove(abs); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %s", p, err))
			continue
		}
		delete(d.files, abs)
		removed = append(removed, abs)
	}
	return removed, failed, nil
}

// randomHex returns n random bytes as a hex string (2n chars).
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// randomFileName generates a random filename with the given extension.
func randomFileName(ext string) (string, error) {
	name, err := randomHex(8)
	if err != nil {
		return "", err
	}
	if ext != "" && !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return name + ext, nil
}

// extensionFromResponse extracts a file extension from the HTTP response.
// It checks Content-Disposition first, then falls back to Content-Type.
func extensionFromResponse(resp *http.Response) string {
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		_, params, err := mime.ParseMediaType(cd)
		if err == nil {
			if filename, ok := params["filename"]; ok {
				if ext := filepath.Ext(filename); ext != "" {
					return ext
				}
			}
		}
	}

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		mediaType, _, _ := mime.ParseMediaType(ct)
		exts, err := mime.ExtensionsByType(mediaType)
		if err == nil && len(exts) > 0 {
			return exts[0]
		}
	}

	return ""
}
