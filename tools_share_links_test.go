package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
)

func TestShareLinkList(t *testing.T) {
	rh := newRouteHandler(t)
	var got url.Values
	rh.Handle("GET", "/api/share_links/", func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		jsonHandler(t, 200, paginatedResponse([]map[string]any{{"id": 1, "slug": "abc", "document": 5, "document_title": "Invoice"}}, 1))(w, r)
	})
	client := testClientAndServer(t, rh)
	result := callTool(t, handleShareLinkList(client), map[string]any{
		"created_after":     "2025-01-01",
		"created_before":    "2025-06-30",
		"expiration_after":  "2025-07-01",
		"expiration_before": "2025-12-31",
		"ordering":          "-document__title",
	})
	assertNotError(t, result)

	want := map[string]string{
		"created__date__gte":    "2025-01-01",
		"created__date__lte":    "2025-06-30",
		"expiration__date__gte": "2025-07-01",
		"expiration__date__lte": "2025-12-31",
		"ordering":              "-document__title",
	}
	for k, v := range want {
		if got.Get(k) != v {
			t.Errorf("query %s = %q, want %q", k, got.Get(k), v)
		}
	}

	results, _ := resultJSON(t, result)["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results = %v, want 1 item", results)
	}
	if title := results[0].(map[string]any)["document_title"]; title != "Invoice" {
		t.Errorf("document_title = %v, want Invoice", title)
	}
}

func TestShareLinkListNoFilters(t *testing.T) {
	rh := newRouteHandler(t)
	var got url.Values
	rh.Handle("GET", "/api/share_links/", func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		jsonHandler(t, 200, paginatedResponse([]map[string]any{}, 0))(w, r)
	})
	client := testClientAndServer(t, rh)
	result := callTool(t, handleShareLinkList(client), nil)
	assertNotError(t, result)
	for _, k := range []string{"created__date__gte", "created__date__lte", "expiration__date__gte", "expiration__date__lte", "ordering"} {
		if got.Has(k) {
			t.Errorf("query has %s = %q, want it unset", k, got.Get(k))
		}
	}
}

func TestShareLinkGet(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/share_links/1/", jsonHandler(t, 200, map[string]any{"id": 1, "slug": "abc"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleGetByID(client, "/api/share_links/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestShareLinkCreate(t *testing.T) {
	rh := newRouteHandler(t)
	var body map[string]any
	rh.Handle("POST", "/api/share_links/", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		jsonHandler(t, 201, map[string]any{"id": 1, "document": 5})(w, r)
	})
	client := testClientAndServer(t, rh)
	// slug is no longer a parameter: the server always generates one, so it must not be sent.
	result := callTool(t, handleShareLinkCreate(client), map[string]any{
		"document":             float64(5),
		"expiration":           "2025-12-31",
		"use_archive_version":  false,
		"slug":                 "custom",
	})
	assertNotError(t, result)

	if body["document"] != float64(5) || body["expiration"] != "2025-12-31" || body["file_version"] != "original" {
		t.Errorf("body = %v", body)
	}
	if _, ok := body["slug"]; ok {
		t.Errorf("body has slug %v, want it omitted", body["slug"])
	}
}

func TestShareLinkCreateRequiresDocument(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleShareLinkCreate(client), map[string]any{})
	assertIsError(t, result)
}

func TestShareLinkDelete(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("DELETE", "/api/share_links/1/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDeleteByID(client, "/api/share_links/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}
