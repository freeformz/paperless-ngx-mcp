package main

import (
	"net/http"
	"sync"
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

func TestCacheSetIfGeneration(t *testing.T) {
	c := NewCache(time.Minute)
	gen := c.Generation()
	c.Invalidate("/api/tags/")
	if c.SetIfGeneration("/api/tags/", []byte("stale"), gen) {
		t.Fatal("stored a response fetched before an invalidation")
	}
	if _, ok := c.Get("/api/tags/"); ok {
		t.Fatal("stale entry cached")
	}
	if !c.SetIfGeneration("/api/tags/", []byte("fresh"), c.Generation()) {
		t.Fatal("refused a current response")
	}
}

// A list GET that is in flight while a write runs must not cache its
// (possibly stale) response.
func TestClientGetDuringWriteNotCached(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/tags/", func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"count":0,"results":[]}`))
	})
	mux.HandleFunc("PATCH /api/tags/1/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":1}`))
	})
	client := testClientAndServer(t, mux)
	// Registered after the server's Close, so it runs first: a failing test
	// can't leave the handler blocked and Close waiting for it.
	t.Cleanup(releaseOnce)

	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := client.Get(t.Context(), "/api/tags/", nil)
		if err == nil {
			resp.Body.Close()
		}
	}()
	<-started
	resp, err := client.Patch(t.Context(), "/api/tags/1/", map[string]any{"name": "x"})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	releaseOnce()
	<-done
	if _, ok := client.cache.Get("/api/tags/"); ok {
		t.Fatal("list fetched during a write was cached")
	}
}

// A write whose request fails may still have landed: the cache is
// invalidated afterwards too.
func TestClientWriteErrorInvalidatesAfter(t *testing.T) {
	client := NewClient("http://127.0.0.1:1", "t")
	gen := client.cache.Generation()
	if _, err := client.Post(t.Context(), "/api/tags/", map[string]any{}); err == nil {
		t.Fatal("expected a connection error")
	}
	if got := client.cache.Generation(); got != gen+2 {
		t.Fatalf("generation = %d, want %d (before and after)", got, gen+2)
	}
}

// Users, groups and the profile change what the cached lists show, so their
// writes invalidate every cached list.
func TestUserWriteInvalidatesMetadataCache(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("PATCH /api/users/3/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":3}`))
	})
	client := testClientAndServer(t, mux)
	client.cache.Set("/api/tags/", []byte("{}"))
	client.cache.Set("/api/custom_fields/", []byte("{}"))
	resp, err := client.Patch(t.Context(), "/api/users/3/", map[string]any{"user_permissions": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	for _, k := range []string{"/api/tags/", "/api/custom_fields/"} {
		if _, ok := client.cache.Get(k); ok {
			t.Errorf("%s still cached after a user write", k)
		}
	}
}

// A list GET that starts and finishes while a write is still in flight must
// not be cached: it may predate the write.
func TestClientGetWithinWriteNotCached(t *testing.T) {
	patchStarted := make(chan struct{})
	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/tags/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"count":0,"results":[]}`))
	})
	mux.HandleFunc("PATCH /api/tags/1/", func(w http.ResponseWriter, r *http.Request) {
		close(patchStarted)
		<-release
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":1}`))
	})
	client := testClientAndServer(t, mux)
	t.Cleanup(releaseOnce)

	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := client.Patch(t.Context(), "/api/tags/1/", map[string]any{"name": "x"})
		if err == nil {
			resp.Body.Close()
		}
	}()
	<-patchStarted
	resp, err := client.Get(t.Context(), "/api/tags/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if _, ok := client.cache.Get("/api/tags/"); ok {
		t.Error("list fetched while a write was in flight was cached")
	}
	releaseOnce()
	<-done

	// Once the write is over, lists cache again.
	resp, err = client.Get(t.Context(), "/api/tags/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if _, ok := client.cache.Get("/api/tags/"); !ok {
		t.Error("list not cached after the write ended")
	}
}

// storage_path_test only renders a template: it leaves the cache alone.
func TestReadOnlyPostKeepsCache(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/storage_paths/test/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`"a/b"`))
	})
	client := testClientAndServer(t, mux)
	client.cache.Set("/api/storage_paths/", []byte("{}"))
	gen := client.cache.Generation()
	resp, err := client.Post(t.Context(), "/api/storage_paths/test/", map[string]any{"path": "x", "document": 1})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if _, ok := client.cache.Get("/api/storage_paths/"); !ok || client.cache.Generation() != gen {
		t.Error("storage_paths/test invalidated the cache")
	}
}
