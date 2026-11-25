package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	apiVersion      = "10"
	defaultCacheTTL = 5 * time.Minute
)

// Client is an HTTP client for the Paperless-ngx REST API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	// longHTTPClient has no overall timeout: the request's context bounds it.
	// It serves the AI endpoints, which can take minutes.
	longHTTPClient *http.Client
	aiTimeout      time.Duration
	cache          *Cache
}

// defaultAITimeout bounds an AI request when PAPERLESS_MCP_AI_TIMEOUT is unset:
// paperless-ngx's LLM timeout defaults to 120 s, and suggestions can make two
// LLM calls.
const defaultAITimeout = 300 * time.Second

// checkRedirect limits redirects and drops the auth header when a redirect
// leaves the original origin (scheme/host/port), so the token can't leak.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	if len(via) > 0 && (req.URL.Scheme != via[0].URL.Scheme || req.URL.Host != via[0].URL.Host) {
		req.Header.Del("Authorization")
	}
	return nil
}

// NewClient creates a new Paperless-ngx API client with in-memory caching.
func NewClient(baseURL, token string) *Client {
	parsedBase := strings.TrimRight(baseURL, "/")
	return &Client{
		baseURL: parsedBase,
		token:   token,
		httpClient: &http.Client{
			Timeout:       30 * time.Second,
			CheckRedirect: checkRedirect,
		},
		longHTTPClient: &http.Client{CheckRedirect: checkRedirect},
		aiTimeout:      defaultAITimeout,
		cache:          NewCache(defaultCacheTTL),
	}
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	return c.doWithAccept(req, "application/json; version="+apiVersion)
}

func (c *Client) doWithAccept(req *http.Request, accept string) (*http.Response, error) {
	req.Header.Set("Authorization", "Token "+c.token)
	req.Header.Set("Accept", accept)
	return c.httpClient.Do(req)
}

// Get performs a GET request with optional query parameters.
// Responses for cacheable metadata list endpoints are served from cache when available.
func (c *Client) Get(ctx context.Context, path string, params url.Values) (*http.Response, error) {
	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	// Check cache for metadata list endpoints (no query params = full list)
	var gen uint64
	if c.cache != nil && isCacheable(path) && len(params) == 0 {
		gen = c.cache.Generation()
		if data, ok := c.cache.Get(path); ok {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(data)),
				Header:     http.Header{"Content-Type": {"application/json"}},
			}, nil
		}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}

	// Cache successful metadata list responses
	if c.cache != nil && isCacheable(path) && len(params) == 0 && resp.StatusCode == http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read response for cache: %w", readErr)
		}
		c.cache.SetIfGeneration(path, body, gen)
		resp.Body = io.NopCloser(bytes.NewReader(body))
	}

	return resp, nil
}

// invalidateAround invalidates the cache for path's resource type now and
// returns a function that does it again, for the caller to defer: a write
// can land on the server even when its request errors or times out, and a
// list fetched while it ran must not stay cached.
func (c *Client) invalidateAround(path string) func() {
	prefix := ""
	if c.cache != nil {
		prefix = cachePrefix(path)
		// Users, groups and the profile decide what the cached lists show
		// (permissions, groups, owners): their writes invalidate all of them.
		if prefix == "" && changesVisibility(path) {
			prefix = "/api/"
		}
	}
	if prefix == "" {
		return func() {}
	}
	c.cache.Invalidate(prefix)
	return func() { c.cache.Invalidate(prefix) }
}

// changesVisibility reports whether a write to path can change which metadata
// objects, or what owner, the cached lists show.
func changesVisibility(path string) bool {
	for _, p := range []string{"/api/users/", "/api/groups/", "/api/profile/"} {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// Post performs a POST request with a JSON body.
// Invalidates cache for the affected resource type.
func (c *Client) Post(ctx context.Context, path string, body any) (*http.Response, error) {
	defer c.invalidateAround(path)()
	req, err := newJSONRequest(ctx, "POST", c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

// newJSONRequest builds a request with body encoded as JSON (none when nil).
func newJSONRequest(ctx context.Context, method, u string, body any) (*http.Request, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, fmt.Errorf("encode body: %w", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, u, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// SetAITimeout sets how long an AI request (GetLong, PostStream) may take.
func (c *Client) SetAITimeout(d time.Duration) { c.aiTimeout = d }

// AITimeout is how long an AI request may take.
func (c *Client) AITimeout() time.Duration { return c.aiTimeout }

// GetLong performs an uncached GET with no overall timeout of its own; ctx
// must carry the deadline. The caller closes the body.
func (c *Client) GetLong(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	return c.doLong(req, "application/json; version="+apiVersion)
}

// PostStream performs a JSON POST whose response may be a long stream, with
// no overall timeout of its own; ctx must carry the deadline. It returns the
// unread response; the caller closes the body.
func (c *Client) PostStream(ctx context.Context, path string, body any) (*http.Response, error) {
	req, err := newJSONRequest(ctx, "POST", c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	return c.doLong(req, "application/json; version="+apiVersion+", */*")
}

func (c *Client) doLong(req *http.Request, accept string) (*http.Response, error) {
	req.Header.Set("Authorization", "Token "+c.token)
	req.Header.Set("Accept", accept)
	return c.longHTTPClient.Do(req)
}

// Patch performs a PATCH request with a JSON body.
// Invalidates cache for the affected resource type.
func (c *Client) Patch(ctx context.Context, path string, body any) (*http.Response, error) {
	defer c.invalidateAround(path)()

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, fmt.Errorf("encode body: %w", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, "PATCH", c.baseURL+path, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

// Delete performs a DELETE request with optional query parameters.
// Invalidates cache for the affected resource type.
func (c *Client) Delete(ctx context.Context, path string, params url.Values) (*http.Response, error) {
	defer c.invalidateAround(path)()

	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, "DELETE", u, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

// GetRaw performs a GET request and returns the raw response without reading the body.
// Unlike Get, it accepts any content type and does not attempt caching.
// The caller is responsible for closing the response body.
func (c *Client) GetRaw(ctx context.Context, path string, params url.Values) (*http.Response, error) {
	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	return c.doWithAccept(req, "application/json; version="+apiVersion+", */*")
}

// PostMultipart performs a POST request with multipart/form-data encoding.
// Used for document uploads. Multi-valued fields (e.g., tags) are written as
// repeated form fields with the same name, as expected by DRF list fields.
func (c *Client) PostMultipart(ctx context.Context, path string, fields url.Values, filePath string) (*http.Response, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	for key, vals := range fields {
		for _, val := range vals {
			if err := writer.WriteField(key, val); err != nil {
				return nil, fmt.Errorf("write field %s: %w", key, err)
			}
		}
	}

	if filePath != "" {
		file, err := os.Open(filePath)
		if err != nil {
			return nil, fmt.Errorf("open file: %w", err)
		}
		defer file.Close()

		part, err := writer.CreateFormFile("document", filepath.Base(filePath))
		if err != nil {
			return nil, fmt.Errorf("create form file: %w", err)
		}
		if _, err := io.Copy(part, file); err != nil {
			return nil, fmt.Errorf("copy file: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+path, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return c.do(req)
}
