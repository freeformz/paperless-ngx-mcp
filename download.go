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

// Downloader manages document file downloads into a per-instance temp directory,
// or an optional user-configured base directory.
type Downloader struct {
	dir         string // per-instance temp dir; cleanup tools operate only here
	baseDir     string // optional user-configured dir (PAPERLESS_MCP_DOWNLOAD_DIR); never cleaned up
	concurrency int
	mu          sync.Mutex // protects file tracking
	files       map[string]struct{}
}

// NewDownloader creates a Downloader with a unique temp directory under os.TempDir().
// The directory is created immediately. The caller should remove it when finished,
// for example with os.RemoveAll(d.Dir()).
//
// baseDir, if non-empty, redirects downloads to that directory instead of the
// temp dir. It is created if missing. Files saved there are not tracked and are
// never touched by cleanup_downloads.
func NewDownloader(concurrency int, baseDir string) (*Downloader, error) {
	if concurrency < 1 {
		return nil, fmt.Errorf("concurrency must be >= 1, got %d", concurrency)
	}
	if baseDir != "" {
		abs, err := filepath.Abs(baseDir)
		if err != nil {
			return nil, fmt.Errorf("resolve download base dir: %w", err)
		}
		if err := os.MkdirAll(abs, 0o700); err != nil {
			return nil, fmt.Errorf("create download base dir: %w", err)
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return nil, fmt.Errorf("resolve download base dir symlinks: %w", err)
		}
		baseDir = resolved
	}
	dir, err := os.MkdirTemp("", "paperless-ngx-mcp-")
	if err != nil {
		return nil, fmt.Errorf("create download dir: %w", err)
	}
	return &Downloader{
		dir:         dir,
		baseDir:     baseDir,
		concurrency: concurrency,
		files:       make(map[string]struct{}),
	}, nil
}

// Dir returns the instance temp download directory path.
func (d *Downloader) Dir() string {
	return d.dir
}

// BaseDir returns the user-configured download directory, or "" if unset.
func (d *Downloader) BaseDir() string {
	return d.baseDir
}

// ResolveDestDir resolves the effective destination directory for a download.
// destDir may only select the configured base directory or a subdirectory of
// it; requesting one without a configured base directory is an error. With no
// destDir, downloads go to the base directory if configured, else the temp dir.
func (d *Downloader) ResolveDestDir(destDir string) (string, error) {
	if destDir == "" {
		if d.baseDir != "" {
			return d.baseDir, nil
		}
		return d.dir, nil
	}
	if d.baseDir == "" {
		return "", fmt.Errorf("dest_dir requires the PAPERLESS_MCP_DOWNLOAD_DIR environment variable to be set on the server")
	}
	dir := destDir
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(d.baseDir, dir)
	}
	dir = filepath.Clean(dir)
	// Lexical containment check before creating anything.
	if !dirWithin(d.baseDir, dir) {
		return "", fmt.Errorf("dest_dir %q is outside the configured download directory", destDir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create dest_dir: %w", err)
	}
	// Re-check after resolving symlinks: a link inside the base directory
	// must not redirect writes outside it.
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("resolve dest_dir: %w", err)
	}
	if !dirWithin(d.baseDir, resolved) {
		return "", fmt.Errorf("dest_dir %q resolves outside the configured download directory", destDir)
	}
	return resolved, nil
}

// dirWithin reports whether dir is base itself or a subdirectory of base.
// Both paths must be absolute and cleaned.
func dirWithin(base, dir string) bool {
	rel, err := filepath.Rel(base, dir)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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

		d.mu.Lock()
		delete(d.files, p)
		d.mu.Unlock()

		removed = append(removed, p)
	}

	return removed, nil
}

// CleanupFiles removes specific files, validating each is a direct child of the download directory.
// Symlinks are resolved to prevent traversal outside the download directory.
func (d *Downloader) CleanupFiles(paths []string) ([]string, []string, error) {
	dirAbs, err := filepath.Abs(d.dir)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve download dir: %w", err)
	}
	dirResolved, err := filepath.EvalSymlinks(dirAbs)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve download dir symlinks: %w", err)
	}

	var removed, failed []string
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: invalid path: %s", p, err))
			continue
		}

		// Must be a direct child (no subdirectories)
		parentAbs := filepath.Dir(filepath.Clean(abs))
		if parentAbs != dirAbs {
			failed = append(failed, fmt.Sprintf("%s: not a direct child of download directory", p))
			continue
		}

		// Resolve symlinks and verify parent is still the download dir
		parentResolved, err := filepath.EvalSymlinks(parentAbs)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: cannot resolve parent: %s", p, err))
			continue
		}
		if parentResolved != dirResolved {
			failed = append(failed, fmt.Sprintf("%s: not inside download directory", p))
			continue
		}

		if err := os.Remove(abs); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %s", p, err))
			continue
		}
		removed = append(removed, abs)
	}

	if len(removed) > 0 {
		d.mu.Lock()
		for _, p := range removed {
			delete(d.files, p)
		}
		d.mu.Unlock()
	}

	return removed, failed, nil
}

// sanitizeFilename reduces a server-provided filename to a safe base name for
// local saving. Returns "" if nothing usable remains.
func sanitizeFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r == 0x7f || r == ':' {
			b.WriteRune('_')
			continue
		}
		b.WriteRune(r)
	}
	name = strings.TrimSpace(b.String())
	if name == "" || name == "." || name == ".." {
		return ""
	}
	if runes := []rune(name); len(runes) > 150 {
		ext := filepath.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		name = string(runes[:150-len([]rune(ext))]) + ext
	}
	return name
}

// createUniqueFile creates a new file named filename in dir, de-duplicating
// collisions with a numeric suffix (name-1.ext, name-2.ext, ...). An empty
// filename falls back to a random name with the given extension.
func createUniqueFile(dir, filename, ext string) (*os.File, string, error) {
	if filename == "" {
		name, err := randomFileName(ext)
		if err != nil {
			return nil, "", fmt.Errorf("generate filename: %w", err)
		}
		filename = name
	}
	fext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, fext)
	for i := range 1000 {
		name := filename
		if i > 0 {
			name = fmt.Sprintf("%s-%d%s", base, i, fext)
		}
		dest := filepath.Join(dir, name)
		f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
		if err == nil {
			return f, dest, nil
		}
		if !os.IsExist(err) {
			return nil, "", fmt.Errorf("create file: %w", err)
		}
	}
	return nil, "", fmt.Errorf("too many existing files named %s in %s", filename, dir)
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

// filenameFromResponse extracts the filename from the Content-Disposition header.
// Returns empty string if no filename is found.
func filenameFromResponse(resp *http.Response) string {
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		_, params, err := mime.ParseMediaType(cd)
		if err == nil {
			if filename, ok := params["filename"]; ok {
				return filename
			}
		}
	}
	return ""
}

// extensionFromResponse extracts a file extension from the HTTP response.
// It checks Content-Disposition first, then falls back to Content-Type.
func extensionFromResponse(resp *http.Response) string {
	if filename := filenameFromResponse(resp); filename != "" {
		if ext := filepath.Ext(filename); ext != "" {
			return ext
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
