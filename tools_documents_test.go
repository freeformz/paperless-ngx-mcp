package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestDocumentList(t *testing.T) {
	docs := paginatedResponse([]map[string]any{
		{"id": 1, "title": "Invoice"},
		{"id": 2, "title": "Receipt"},
	}, 2)

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/", jsonHandler(t, 200, docs))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentList(client), nil)
	assertNotError(t, result)

	m := resultJSON(t, result)
	if m["count"] != float64(2) {
		t.Errorf("count = %v, want 2", m["count"])
	}
}

func TestDocumentListWithQuery(t *testing.T) {
	var capturedQuery string
	ts := newRouteHandler(t)
	ts.Handle("GET", "/api/documents/", func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.Query().Get("query")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(paginatedResponse([]any{}, 0))
	})

	client := testClientAndServer(t, ts)
	result := callTool(t, handleDocumentList(client), map[string]any{"query": "invoice"})
	assertNotError(t, result)

	if capturedQuery != "invoice" {
		t.Errorf("query = %q, want invoice", capturedQuery)
	}
}

func TestDocumentGet(t *testing.T) {
	doc := map[string]any{"id": float64(42), "title": "Test Document"}

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/42/", jsonHandler(t, 200, doc))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleGetByID(client, "/api/documents/%d/"), map[string]any{"id": float64(42)})
	assertNotError(t, result)

	m := resultJSON(t, result)
	if m["id"] != float64(42) {
		t.Errorf("id = %v, want 42", m["id"])
	}
}

func TestDocumentGetRequiresID(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleGetByID(client, "/api/documents/%d/"), map[string]any{})
	assertIsError(t, result)
}

func TestDocumentUpdate(t *testing.T) {
	var capturedBody map[string]any

	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/documents/1/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"id":1,"title":"Updated"}`))
	})

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentUpdate(client), map[string]any{
		"id":    float64(1),
		"title": "Updated",
	})
	assertNotError(t, result)

	if capturedBody["title"] != "Updated" {
		t.Errorf("title = %v, want Updated", capturedBody["title"])
	}
}

func TestDocumentUpdateClearCorrespondent(t *testing.T) {
	var capturedBody map[string]any

	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/documents/1/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"id":1}`))
	})

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentUpdate(client), map[string]any{
		"id":            float64(1),
		"correspondent": nil,
	})
	assertNotError(t, result)

	if _, ok := capturedBody["correspondent"]; !ok {
		t.Error("expected correspondent in body")
	}
	if capturedBody["correspondent"] != nil {
		t.Errorf("correspondent = %v, want nil", capturedBody["correspondent"])
	}
}

func TestDocumentUpdateNoFields(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleDocumentUpdate(client), map[string]any{
		"id": float64(1),
	})
	assertIsError(t, result)
}

func TestDocumentDelete(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("DELETE", "/api/documents/1/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDeleteByID(client, "/api/documents/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestDocumentNoteList(t *testing.T) {
	notes := []map[string]any{{"id": 1, "note": "test note"}}

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/notes/", jsonHandler(t, 200, notes))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleGetByID(client, "/api/documents/%d/notes/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestDocumentNoteAdd(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/documents/1/notes/", jsonHandler(t, 201, map[string]any{"id": 1, "note": "new note"}))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentNoteAdd(client), map[string]any{
		"id":   float64(1),
		"note": "new note",
	})
	assertNotError(t, result)
}

func TestDocumentNoteAddRequiresNote(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleDocumentNoteAdd(client), map[string]any{"id": float64(1)})
	assertIsError(t, result)
}

func TestDocumentNoteDelete(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("DELETE", "/api/documents/1/notes/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("id") != "5" {
			t.Errorf("note id query param = %q, want 5", r.URL.Query().Get("id"))
		}
		w.WriteHeader(http.StatusNoContent)
	})

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentNoteDelete(client), map[string]any{
		"id":      float64(1),
		"note_id": float64(5),
	})
	assertNotError(t, result)
}

func TestDocumentMetadata(t *testing.T) {
	meta := map[string]any{"original_checksum": "abc123", "original_size": float64(1024)}

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/metadata/", jsonHandler(t, 200, meta))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleGetByID(client, "/api/documents/%d/metadata/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestDocumentSuggestions(t *testing.T) {
	suggestions := map[string]any{"correspondents": []any{1, 2}, "tags": []any{3}}

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/suggestions/", jsonHandler(t, 200, suggestions))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleGetByID(client, "/api/documents/%d/suggestions/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestDocumentNextASN(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/next_asn/", jsonHandler(t, 200, float64(42)))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleSimpleGet(client, "/api/documents/next_asn/"), nil)
	assertNotError(t, result)
}

func TestDocumentShareLinksHandler(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/share_links/", jsonHandler(t, 200, []map[string]any{{"id": 1}}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleGetByID(client, "/api/documents/%d/share_links/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestDocumentHistory(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/history/", jsonHandler(t, 200, []map[string]any{{"action": "created"}}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleGetByID(client, "/api/documents/%d/history/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestDocumentEmail(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/documents/email/", jsonHandler(t, 200, map[string]any{"result": "ok"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentEmail(client), map[string]any{
		"documents": "[1, 2]",
		"to":        "user@example.com",
		"subject":   "Document",
	})
	assertNotError(t, result)
}

func TestDocumentEmailRequiresFields(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleDocumentEmail(client), map[string]any{})
	assertIsError(t, result)
	result = callTool(t, handleDocumentEmail(client), map[string]any{"documents": "[1]"})
	assertIsError(t, result)
}

func TestDocumentUploadRequiresFilePath(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleDocumentUpload(client), map[string]any{})
	assertIsError(t, result)
}

func TestDocumentUploadRejectsNonExistentPath(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleDocumentUpload(client), map[string]any{"file_path": "/nonexistent/file.pdf"})
	assertIsError(t, result)
}

func TestDocumentUploadRejectsDirectory(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleDocumentUpload(client), map[string]any{"file_path": t.TempDir()})
	assertIsError(t, result)
}

func TestDocumentUploadRejectsSymlink(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "target.pdf")
	if err := os.WriteFile(target, []byte("pdf"), 0o644); err != nil {
		t.Fatalf("failed to create target file: %v", err)
	}
	link := filepath.Join(tmp, "link.pdf")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("skipping: symlinks not supported: %v", err)
	}

	client := NewClient("http://unused", "unused")
	result := callTool(t, handleDocumentUpload(client), map[string]any{"file_path": link})
	assertIsError(t, result)
}

func TestAPIErrorHandling(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/999/", jsonHandler(t, 404, map[string]any{"detail": "Not found."}))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleGetByID(client, "/api/documents/%d/"), map[string]any{"id": float64(999)})
	assertIsError(t, result)

	m := resultJSON(t, result)
	if m["status_code"] != float64(404) {
		t.Errorf("status_code = %v, want 404", m["status_code"])
	}
}
