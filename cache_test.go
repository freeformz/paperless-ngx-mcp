package main

import (
	"net/http"
	"testing"
	"time"
)

func TestCacheGetSet(t *testing.T) {
	c := NewCache(1 * time.Minute)

	// Miss
	if _, ok := c.Get("/api/tags/"); ok {
		t.Error("expected cache miss")
	}

	// Set and hit
	data := []byte(`{"count":1}`)
	c.Set("/api/tags/", data)

	got, ok := c.Get("/api/tags/")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if string(got) != string(data) {
		t.Errorf("got %s, want %s", got, data)
	}
}

func TestCacheExpiration(t *testing.T) {
	c := NewCache(1 * time.Millisecond)
	c.Set("/api/tags/", []byte(`{}`))

	time.Sleep(5 * time.Millisecond)

	if _, ok := c.Get("/api/tags/"); ok {
		t.Error("expected cache miss after expiration")
	}
}

func TestCacheInvalidate(t *testing.T) {
	c := NewCache(1 * time.Minute)
	c.Set("/api/tags/", []byte(`{"count":5}`))

	c.Invalidate("/api/tags/")

	if _, ok := c.Get("/api/tags/"); ok {
		t.Error("expected cache miss after invalidation")
	}
}

func TestCacheInvalidatePrefix(t *testing.T) {
	c := NewCache(1 * time.Minute)
	c.Set("/api/tags/", []byte(`{"tags":[]}`))
	c.Set("/api/correspondents/", []byte(`{"correspondents":[]}`))

	// Invalidate only tags
	c.Invalidate("/api/tags/")

	if _, ok := c.Get("/api/tags/"); ok {
		t.Error("expected tags cache miss")
	}
	if _, ok := c.Get("/api/correspondents/"); !ok {
		t.Error("expected correspondents cache hit")
	}
}

func TestIsCacheable(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/api/tags/", true},
		{"/api/correspondents/", true},
		{"/api/document_types/", true},
		{"/api/storage_paths/", true},
		{"/api/custom_fields/", true},
		{"/api/documents/", false},
		{"/api/tags/1/", false},
		{"/api/search/", false},
	}

	for _, tt := range tests {
		if got := isCacheable(tt.path); got != tt.want {
			t.Errorf("isCacheable(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestCachePrefix(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/api/tags/", "/api/tags/"},
		{"/api/tags/5/", "/api/tags/"},
		{"/api/correspondents/3/", "/api/correspondents/"},
		{"/api/documents/1/", ""},
		{"/api/search/", ""},
	}

	for _, tt := range tests {
		if got := cachePrefix(tt.path); got != tt.want {
			t.Errorf("cachePrefix(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestClientCacheIntegration(t *testing.T) {
	callCount := 0
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/tags/", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"count":1,"results":[{"id":1,"name":"test"}]}`))
	})

	client := testClientAndServer(t, rh)

	// First call - cache miss
	result := callTool(t, handleTagList(client), nil)
	assertNotError(t, result)
	if callCount != 1 {
		t.Errorf("expected 1 API call, got %d", callCount)
	}

	// Second call - cache hit
	result = callTool(t, handleTagList(client), nil)
	assertNotError(t, result)
	if callCount != 1 {
		t.Errorf("expected still 1 API call after cache hit, got %d", callCount)
	}

	// Create a tag - should invalidate cache
	rh.Handle("POST", "/api/tags/", jsonHandler(t, 201, map[string]any{"id": 2, "name": "new"}))
	result = callTool(t, handleTagCreate(client), map[string]any{"name": "new"})
	assertNotError(t, result)

	// Third call - cache miss after invalidation
	result = callTool(t, handleTagList(client), nil)
	assertNotError(t, result)
	if callCount != 2 {
		t.Errorf("expected 2 API calls after invalidation, got %d", callCount)
	}
}
