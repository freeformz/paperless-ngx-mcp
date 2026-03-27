package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientAuthAndVersionHeaders(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Token test-token" {
			t.Errorf("Authorization = %q, want %q", got, "Token test-token")
		}
		if got := r.Header.Get("Accept"); got != "application/json; version=9" {
			t.Errorf("Accept = %q, want %q", got, "application/json; version=9")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	t.Cleanup(ts.Close)

	client := NewClient(ts.URL, "test-token")

	resp, err := client.Get("/api/test/", nil)
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
	resp, err := client.Post("/api/tags/", map[string]any{"name": "test"})
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
	resp, err := client.Patch("/api/tags/1/", map[string]any{"name": "updated"})
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
	resp, err := client.Delete("/api/tags/1/", nil)
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
	resp, err := client.Get("/api/test/", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}
