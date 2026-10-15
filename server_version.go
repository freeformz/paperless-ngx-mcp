package main

// Paperless-ngx server version detection for tools that need a minimum server
// version. Keep feature-specific minimum versions next to the tools that use
// them, not in this file.

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// versionProbePath is a cheap endpoint any authenticated user may read. Only its
// X-Version response header is used.
const versionProbePath = "/api/ui_settings/"

// recordServerVersion remembers the Paperless-ngx version from the X-Version
// header, which Paperless-ngx sets on every authenticated API response. Updating
// it on every response means a server upgrade is picked up without a restart.
func (c *Client) recordServerVersion(h http.Header) {
	v := h.Get("X-Version")
	if v == "" {
		return
	}
	c.versionMu.Lock()
	c.serverVersion = v
	c.versionMu.Unlock()
}

// ServerVersion returns the Paperless-ngx version reported by the server, e.g.
// "3.2.1". If no response has been seen yet, it probes versionProbePath once.
// It returns "" when the server does not report a version.
func (c *Client) ServerVersion(ctx context.Context) (string, error) {
	c.versionMu.Lock()
	v := c.serverVersion
	c.versionMu.Unlock()
	if v != "" {
		return v, nil
	}

	// Not c.Get: a cached response would carry no X-Version header.
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+versionProbePath, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()

	c.versionMu.Lock()
	defer c.versionMu.Unlock()
	return c.serverVersion, nil
}

// parseVersion extracts major, minor, and patch from a Paperless-ngx version
// string such as "3.2.1" or "3.0.0-beta.rc1" (a pre-release counts as its
// release). Missing minor or patch parts count as 0.
func parseVersion(s string) (v [3]int, ok bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+ "); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

// versionAtLeast reports whether version is at least minVersion. ok is false
// when either version cannot be parsed.
func versionAtLeast(version, minVersion string) (atLeast, ok bool) {
	got, ok1 := parseVersion(version)
	want, ok2 := parseVersion(minVersion)
	if !ok1 || !ok2 {
		return false, false
	}
	for i := range got {
		if got[i] != want[i] {
			return got[i] > want[i], true
		}
	}
	return true, true
}

// requireServerVersion returns an error result when the Paperless-ngx server is
// older than minVersion, or nil if the tool may proceed. If the version cannot be
// determined (probe failed, header missing, unparsable development build), the
// tool proceeds and the API itself reports any failure.
func requireServerVersion(ctx context.Context, client *Client, minVersion, feature string) *mcp.CallToolResult {
	v, err := client.ServerVersion(ctx)
	if err != nil || v == "" {
		return nil
	}
	if atLeast, ok := versionAtLeast(v, minVersion); ok && !atLeast {
		return errResult(fmt.Sprintf("%s requires Paperless-ngx %s or later; the server runs %s", feature, minVersion, v))
	}
	return nil
}
