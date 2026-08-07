package main

import (
	"encoding/json"
	"net/http"
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
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/search/", jsonHandler(t, 200, map[string]any{"results": []any{}}))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleSearchGlobal(client), map[string]any{"query": "test"})
	assertNotError(t, result)
}

func TestSearchGlobalTruncatesDocumentContent(t *testing.T) {
	longContent := strings.Repeat("y", 3000)
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/search/", jsonHandler(t, 200, map[string]any{
		"total":     1,
		"documents": []map[string]any{{"id": 1, "content": longContent}},
		"tags":      []map[string]any{{"id": 7, "name": "invoices"}},
	}))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleSearchGlobal(client), map[string]any{"query": "test"})
	assertNotError(t, result)

	m := resultJSON(t, result)
	doc := m["documents"].([]any)[0].(map[string]any)
	if got := len(doc["content"].(string)); got != defaultContentSnippetLen {
		t.Errorf("content length = %d, want %d", got, defaultContentSnippetLen)
	}
	if doc["content_truncated"] != true {
		t.Error("content_truncated should be true")
	}
}

func TestSearchGlobalStripsNotes(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/search/", jsonHandler(t, 200, map[string]any{
		"documents": []map[string]any{{"id": 1, "notes": []map[string]any{{"id": 10, "note": "verbose"}}}},
	}))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleSearchGlobal(client), map[string]any{"query": "test"})
	assertNotError(t, result)

	m := resultJSON(t, result)
	doc := m["documents"].([]any)[0].(map[string]any)
	if _, ok := doc["notes"]; ok {
		t.Error("notes should be stripped by default")
	}
	if doc["notes_count"] != float64(1) {
		t.Errorf("notes_count = %v, want 1", doc["notes_count"])
	}
}

func TestSearchGlobalIncludeNotes(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/search/", jsonHandler(t, 200, map[string]any{
		"documents": []map[string]any{{"id": 1, "notes": []map[string]any{{"id": 10, "note": "verbose"}}}},
	}))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleSearchGlobal(client), map[string]any{"query": "test", "include_notes": true})
	assertNotError(t, result)

	m := resultJSON(t, result)
	doc := m["documents"].([]any)[0].(map[string]any)
	if notes, ok := doc["notes"].([]any); !ok || len(notes) != 1 {
		t.Fatalf("notes = %v, want 1 full note with include_notes=true", doc["notes"])
	}
}

func TestSearchGlobalFullContent(t *testing.T) {
	longContent := strings.Repeat("y", 3000)
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/search/", jsonHandler(t, 200, map[string]any{
		"documents": []map[string]any{{"id": 1, "content": longContent}},
	}))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleSearchGlobal(client), map[string]any{"query": "test", "full_content": true})
	assertNotError(t, result)

	m := resultJSON(t, result)
	doc := m["documents"].([]any)[0].(map[string]any)
	if got := len(doc["content"].(string)); got != 3000 {
		t.Errorf("content length = %d, want 3000 (untruncated)", got)
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
