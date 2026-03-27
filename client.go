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
	apiVersion      = "9"
	defaultCacheTTL = 5 * time.Minute
)

// Client is an HTTP client for the Paperless-ngx REST API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	cache      *Cache
}

// NewClient creates a new Paperless-ngx API client with in-memory caching.
func NewClient(baseURL, token string) *Client {
	parsedBase := strings.TrimRight(baseURL, "/")
	return &Client{
		baseURL: parsedBase,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("stopped after 10 redirects")
				}
				// Strip auth header if redirected to a different origin (scheme/host/port) to prevent token leaking.
				if len(via) > 0 && (req.URL.Scheme != via[0].URL.Scheme || req.URL.Host != via[0].URL.Host) {
					req.Header.Del("Authorization")
				}
				return nil
			},
		},
		cache: NewCache(defaultCacheTTL),
	}
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Token "+c.token)
	req.Header.Set("Accept", "application/json; version="+apiVersion)
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
	if c.cache != nil && isCacheable(path) && len(params) == 0 {
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
		c.cache.Set(path, body)
		resp.Body = io.NopCloser(bytes.NewReader(body))
	}

	return resp, nil
}

// Post performs a POST request with a JSON body.
// Invalidates cache for the affected resource type.
func (c *Client) Post(ctx context.Context, path string, body any) (*http.Response, error) {
	if c.cache != nil {
		if prefix := cachePrefix(path); prefix != "" {
			c.cache.Invalidate(prefix)
		}
	}

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, fmt.Errorf("encode body: %w", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+path, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

// Patch performs a PATCH request with a JSON body.
// Invalidates cache for the affected resource type.
func (c *Client) Patch(ctx context.Context, path string, body any) (*http.Response, error) {
	if c.cache != nil {
		if prefix := cachePrefix(path); prefix != "" {
			c.cache.Invalidate(prefix)
		}
	}

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
	if c.cache != nil {
		if prefix := cachePrefix(path); prefix != "" {
			c.cache.Invalidate(prefix)
		}
	}

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

// PostMultipart performs a POST request with multipart/form-data encoding.
// Used for document uploads.
func (c *Client) PostMultipart(ctx context.Context, path string, fields map[string]string, filePath string) (*http.Response, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	for key, val := range fields {
		if err := writer.WriteField(key, val); err != nil {
			return nil, fmt.Errorf("write field %s: %w", key, err)
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
