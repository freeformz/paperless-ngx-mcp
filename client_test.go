package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIVersion(t *testing.T) {
	// paperless-ngx 3.5.0 is the minimum supported server; it serves API v10.
	if apiVersion != "10" {
		t.Errorf("apiVersion = %q, want 10", apiVersion)
	}
}

func TestClientAuthAndVersionHeaders(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Token test-token" {
			t.Errorf("Authorization = %q, want %q", got, "Token test-token")
		}
		if want := "application/json; version=" + apiVersion; r.Header.Get("Accept") != want {
			t.Errorf("Accept = %q, want %q", r.Header.Get("Accept"), want)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	t.Cleanup(ts.Close)

	client := NewClient(ts.URL, "test-token")

	resp, err := client.Get(t.Context(), "/api/test/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestClientPostJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}

		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatalf("unmarshal body: %s", err)
		}
		if m["name"] != "test" {
			t.Errorf("body name = %v, want test", m["name"])
		}

		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":1,"name":"test"}`))
	}))
	t.Cleanup(ts.Close)

	client := NewClient(ts.URL, "test-token")
	resp, err := client.Post(t.Context(), "/api/tags/", map[string]any{"name": "test"})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
}

func TestClientPatch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":1}`))
	}))
	t.Cleanup(ts.Close)

	client := NewClient(ts.URL, "test-token")
	resp, err := client.Patch(t.Context(), "/api/tags/1/", map[string]any{"name": "updated"})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestClientDelete(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(ts.Close)

	client := NewClient(ts.URL, "test-token")
	resp, err := client.Delete(t.Context(), "/api/tags/1/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
}

func TestClientBaseURLTrailingSlash(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	t.Cleanup(ts.Close)

	client := NewClient(ts.URL+"/", "test-token")
	resp, err := client.Get(t.Context(), "/api/test/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}
