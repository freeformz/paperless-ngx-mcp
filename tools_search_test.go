package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestSearchAutocomplete(t *testing.T) {
	var capturedTerm string
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/search/autocomplete/", func(w http.ResponseWriter, r *http.Request) {
		capturedTerm = r.URL.Query().Get("term")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]string{"invoice", "insurance"})
	})

	client := testClientAndServer(t, rh)
	result := callTool(t, handleSearchAutocomplete(client), map[string]any{"term": "inv"})
	assertNotError(t, result)

	if capturedTerm != "inv" {
		t.Errorf("term = %q, want inv", capturedTerm)
	}
}

func TestSearchAutocompleteRequiresTerm(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleSearchAutocomplete(client), map[string]any{})
	assertIsError(t, result)
}

func TestSearchGlobal(t *testing.T) {
	var captured url.Values
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/search/", func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"total":     1,
			"documents": []map[string]any{{"id": 1, "title": "Invoice", "created": "2026-01-02"}},
		})
	})

	client := testClientAndServer(t, rh)
	result := callTool(t, handleSearchGlobal(client), map[string]any{
		"query": "invoice",
		// Removed parameters: a client still sending them must not get them forwarded.
		"page": 2, "page_size": 50, "full_content": true, "content_snippet_bytes": 10, "include_notes": true,
	})
	assertNotError(t, result)

	want := url.Values{"query": {"invoice"}}
	if !reflect.DeepEqual(captured, want) {
		t.Errorf("query params = %v, want %v", captured, want)
	}
	m := resultJSON(t, result)
	doc := m["documents"].([]any)[0].(map[string]any)
	if doc["title"] != "Invoice" {
		t.Errorf("document = %v, want it passed through", doc)
	}
}

func TestSearchGlobalPassesResponseThrough(t *testing.T) {
	// The response is passed through unchanged, whatever fields it carries.
	longContent := strings.Repeat("y", 3000)
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/search/", jsonHandler(t, 200, map[string]any{
		"documents": []map[string]any{{"id": 1, "content": longContent, "notes": []map[string]any{{"id": 10}}}},
	}))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleSearchGlobal(client), map[string]any{"query": "test"})
	assertNotError(t, result)

	doc := resultJSON(t, result)["documents"].([]any)[0].(map[string]any)
	if got := len(doc["content"].(string)); got != 3000 {
		t.Errorf("content length = %d, want 3000", got)
	}
	if _, ok := doc["notes"]; !ok {
		t.Error("notes should be passed through")
	}
}

func TestSearchGlobalQueryLength(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{"missing", "", true},
		{"two chars", "ab", true},
		{"three chars", "abc", false},
		{"two multibyte chars", "äö", true},
		{"two astral chars", "🙂🙂", true},
		{"three multibyte chars", "äöü", false},
		{"max length", strings.Repeat("a", 4096), false},
		{"max length multibyte", strings.Repeat("é", 4096), false},
		{"too long", strings.Repeat("a", 4097), true},
		{"too long multibyte", strings.Repeat("é", 4097), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			rh := newRouteHandler(t)
			rh.Handle("GET", "/api/search/", func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte("{}"))
			})
			client := testClientAndServer(t, rh)
			result := callTool(t, handleSearchGlobal(client), map[string]any{"query": tt.query})
			if tt.wantErr {
				assertIsError(t, result)
				if called {
					t.Error("request sent for an invalid query")
				}
			} else {
				assertNotError(t, result)
				if !called {
					t.Error("request not sent for a valid query")
				}
			}
		})
	}
}

func TestStatistics(t *testing.T) {
	stats := map[string]any{"documents_total": float64(100), "documents_inbox": float64(5)}

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/statistics/", jsonHandler(t, 200, stats))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleSimpleGet(client, "/api/statistics/"), nil)
	assertNotError(t, result)

	m := resultJSON(t, result)
	if m["documents_total"] != float64(100) {
		t.Errorf("documents_total = %v, want 100", m["documents_total"])
	}
}


func TestSearchAutocompleteLimit(t *testing.T) {
	var got captured
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/search/autocomplete/", captureHandler(t, &got, 200, []any{}))
	client := testClientAndServer(t, rh)

	for _, limit := range []any{float64(0), "0", ""} {
		result := callTool(t, handleSearchAutocomplete(client), map[string]any{"term": "inv", "limit": limit})
		assertNotError(t, result)
		if got.query.Has("limit") {
			t.Errorf("limit %#v: sent limit=%s, want none", limit, got.query.Get("limit"))
		}
	}
	result := callTool(t, handleSearchAutocomplete(client), map[string]any{"term": "inv", "limit": float64(5)})
	assertNotError(t, result)
	if got.query.Get("limit") != "5" {
		t.Errorf("limit = %q, want 5", got.query.Get("limit"))
	}
	assertIsError(t, callTool(t, handleSearchAutocomplete(client), map[string]any{"term": "inv", "limit": float64(-1)}))
}
