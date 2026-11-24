package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
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

func TestDocumentListTruncatesContent(t *testing.T) {
	longContent := strings.Repeat("x", 2000)
	docs := map[string]any{
		"count":    1,
		"next":     nil,
		"previous": nil,
		"results":  []map[string]any{{"id": 1, "title": "Invoice", "content": longContent}},
	}

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/", jsonHandler(t, 200, docs))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentList(client), nil)
	assertNotError(t, result)

	m := resultJSON(t, result)
	doc := m["results"].([]any)[0].(map[string]any)
	if got := len(doc["content"].(string)); got != defaultContentSnippetLen {
		t.Errorf("content length = %d, want %d", got, defaultContentSnippetLen)
	}
	if doc["content_truncated"] != true {
		t.Error("content_truncated should be true")
	}
}

func TestDocumentListFullContent(t *testing.T) {
	longContent := strings.Repeat("x", 2000)
	docs := map[string]any{
		"count":   1,
		"results": []map[string]any{{"id": 1, "content": longContent}},
	}

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/", jsonHandler(t, 200, docs))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentList(client), map[string]any{
		"full_content": true,
	})
	assertNotError(t, result)

	m := resultJSON(t, result)
	doc := m["results"].([]any)[0].(map[string]any)
	if got := len(doc["content"].(string)); got != 2000 {
		t.Errorf("content length = %d, want 2000 (untruncated)", got)
	}
	if _, ok := doc["content_truncated"]; ok {
		t.Error("content_truncated should be absent with full_content=true")
	}
}

func TestDocumentListCustomSnippetBytes(t *testing.T) {
	docs := paginatedResponse([]map[string]any{{"id": 1, "content": strings.Repeat("x", 100)}}, 1)

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/", jsonHandler(t, 200, docs))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentList(client), map[string]any{"content_snippet_bytes": float64(10)})
	assertNotError(t, result)

	m := resultJSON(t, result)
	doc := m["results"].([]any)[0].(map[string]any)
	if got := len(doc["content"].(string)); got != 10 {
		t.Errorf("content length = %d, want 10", got)
	}
	if doc["content_truncated"] != true {
		t.Error("content_truncated should be true")
	}
}

func TestDocumentListZeroSnippetBytesDisablesTruncation(t *testing.T) {
	docs := paginatedResponse([]map[string]any{{"id": 1, "content": strings.Repeat("x", 2000)}}, 1)

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/", jsonHandler(t, 200, docs))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentList(client), map[string]any{"content_snippet_bytes": float64(0)})
	assertNotError(t, result)

	m := resultJSON(t, result)
	doc := m["results"].([]any)[0].(map[string]any)
	if got := len(doc["content"].(string)); got != 2000 {
		t.Errorf("content length = %d, want 2000 (untruncated)", got)
	}
}

func TestDocumentListNegativeSnippetBytesRejected(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleDocumentList(client), map[string]any{"content_snippet_bytes": float64(-5)})
	assertIsError(t, result)
}

func TestDocumentListShortContentNotMarkedTruncated(t *testing.T) {
	docs := paginatedResponse([]map[string]any{{"id": 1, "content": "short"}}, 1)

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/", jsonHandler(t, 200, docs))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentList(client), nil)
	assertNotError(t, result)

	m := resultJSON(t, result)
	doc := m["results"].([]any)[0].(map[string]any)
	if doc["content"] != "short" {
		t.Errorf("content = %v, want short", doc["content"])
	}
	if _, ok := doc["content_truncated"]; ok {
		t.Error("content_truncated should be absent for short content")
	}
}

func TestDocumentListStripsNotes(t *testing.T) {
	docs := paginatedResponse([]map[string]any{
		{"id": 1, "notes": []map[string]any{{"id": 10, "note": "Review: long note text"}}},
		{"id": 2, "notes": []map[string]any{}},
	}, 2)

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/", jsonHandler(t, 200, docs))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentList(client), nil)
	assertNotError(t, result)

	m := resultJSON(t, result)
	doc := m["results"].([]any)[0].(map[string]any)
	if _, ok := doc["notes"]; ok {
		t.Error("notes should be stripped by default")
	}
	if doc["notes_count"] != float64(1) {
		t.Errorf("notes_count = %v, want 1", doc["notes_count"])
	}

	noNotes := m["results"].([]any)[1].(map[string]any)
	if _, ok := noNotes["notes_count"]; ok {
		t.Error("notes_count should be absent for documents without notes")
	}
}

func TestDocumentListIncludeNotes(t *testing.T) {
	docs := paginatedResponse([]map[string]any{
		{"id": 1, "notes": []map[string]any{{"id": 10, "note": "keep me"}}},
	}, 1)

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/", jsonHandler(t, 200, docs))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentList(client), map[string]any{"include_notes": true})
	assertNotError(t, result)

	m := resultJSON(t, result)
	doc := m["results"].([]any)[0].(map[string]any)
	notes, ok := doc["notes"].([]any)
	if !ok || len(notes) != 1 {
		t.Fatalf("notes = %v, want 1 full note with include_notes=true", doc["notes"])
	}
	if _, ok := doc["notes_count"]; ok {
		t.Error("notes_count should be absent with include_notes=true")
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
		"id":    float64(1),
		"clear": []any{"correspondent"},
	})
	assertNotError(t, result)

	if _, ok := capturedBody["correspondent"]; !ok {
		t.Error("expected correspondent in body")
	}
	if capturedBody["correspondent"] != nil {
		t.Errorf("correspondent = %v, want nil", capturedBody["correspondent"])
	}
}

func TestDocumentUpdateContent(t *testing.T) {
	var capturedBody map[string]any

	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/documents/1/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"id":1,"content":"new content"}`))
	})

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentUpdate(client), map[string]any{
		"id":      float64(1),
		"content": "new content",
	})
	assertNotError(t, result)

	if capturedBody["content"] != "new content" {
		t.Errorf("content = %v, want 'new content'", capturedBody["content"])
	}
}

func TestDocumentUpdateContentEmpty(t *testing.T) {
	var capturedBody map[string]any

	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/documents/1/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"id":1,"content":""}`))
	})

	client := testClientAndServer(t, rh)
	// "" is not given (padding); clear: ["content"] empties the text.
	result := callTool(t, handleDocumentUpdate(client), map[string]any{
		"id":      float64(1),
		"content": "",
	})
	assertIsError(t, result)
	result = callTool(t, handleDocumentUpdate(client), map[string]any{
		"id":    float64(1),
		"clear": []any{"content"},
	})
	assertNotError(t, result)
	assertIsError(t, callTool(t, handleDocumentUpdate(client), map[string]any{"id": float64(1), "content": "x", "clear": []any{"content"}}))

	if _, ok := capturedBody["content"]; !ok {
		t.Error("expected content in body")
	}
	if capturedBody["content"] != "" {
		t.Errorf("content = %v, want empty string", capturedBody["content"])
	}
}

func TestDocumentUpdateOwner(t *testing.T) {
	var capturedBody map[string]any

	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/documents/1/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"id":1,"owner":5}`))
	})

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentUpdate(client), map[string]any{
		"id":    float64(1),
		"owner": float64(5),
	})
	assertNotError(t, result)

	if capturedBody["owner"] != float64(5) {
		t.Errorf("owner = %v, want 5", capturedBody["owner"])
	}
}

func TestDocumentUpdateClearOwner(t *testing.T) {
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
		"id":    float64(1),
		"clear": `["owner"]`, // a JSON string works too
	})
	assertNotError(t, result)

	if _, ok := capturedBody["owner"]; !ok {
		t.Error("expected owner in body")
	}
	if capturedBody["owner"] != nil {
		t.Errorf("owner = %v, want nil", capturedBody["owner"])
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
	rootRoute(rh, "1", 1)
	rh.Handle("GET", "/api/documents/1/notes/", jsonHandler(t, 200, notes))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentNoteList(client), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestDocumentNoteAdd(t *testing.T) {
	rh := newRouteHandler(t)
	rootRoute(rh, "1", 1)
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
	rootRoute(rh, "1", 1)
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

func TestDocumentClassifierSuggestions(t *testing.T) {
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
	var capturedBody map[string]any
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/documents/email/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"result":"ok"}`))
	})
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentEmail(client), map[string]any{
		"documents": "[1, 2]",
		"to":        "user@example.com",
		"subject":   "Document",
		"body":      "Please find attached.",
	})
	assertNotError(t, result)

	if capturedBody["addresses"] != "user@example.com" {
		t.Errorf("addresses = %v, want user@example.com", capturedBody["addresses"])
	}
	if capturedBody["subject"] != "Document" {
		t.Errorf("subject = %v, want Document", capturedBody["subject"])
	}
	if capturedBody["message"] != "Please find attached." {
		t.Errorf("message = %v, want 'Please find attached.'", capturedBody["message"])
	}
	if _, ok := capturedBody["to"]; ok {
		t.Error("body should not contain 'to' — the API field is 'addresses'")
	}
	if _, ok := capturedBody["body"]; ok {
		t.Error("body should not contain 'body' — the API field is 'message'")
	}
}

func TestDocumentEmailRequiresFields(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleDocumentEmail(client), map[string]any{})
	assertIsError(t, result)
	result = callTool(t, handleDocumentEmail(client), map[string]any{"documents": "[1]"})
	assertIsError(t, result)
	result = callTool(t, handleDocumentEmail(client), map[string]any{"documents": "[1]", "to": "user@example.com"})
	assertIsError(t, result)
	result = callTool(t, handleDocumentEmail(client), map[string]any{"documents": "[1]", "to": "user@example.com", "subject": "Doc"})
	assertIsError(t, result)
}

func TestDocumentUploadTagsAsRepeatedFields(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "doc.pdf")
	if err := os.WriteFile(filePath, []byte("pdf"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	var capturedTags []string
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/documents/post_document/", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		capturedTags = r.MultipartForm.Value["tags"]
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`"task-uuid"`))
	})

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentUpload(client), map[string]any{
		"file_path": filePath,
		"tags":      "[3, 7]",
	})
	assertNotError(t, result)

	if len(capturedTags) != 2 || capturedTags[0] != "3" || capturedTags[1] != "7" {
		t.Errorf("tags form fields = %v, want [3 7]", capturedTags)
	}
}

func TestDocumentUploadRejectsInvalidTagsJSON(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "doc.pdf")
	if err := os.WriteFile(filePath, []byte("pdf"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	client := NewClient("http://unused", "unused")
	result := callTool(t, handleDocumentUpload(client), map[string]any{
		"file_path": filePath,
		"tags":      "not-json",
	})
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

func TestDocumentHistoryAuditLogOffHint(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/history/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(400)
		w.Write([]byte("Audit log is disabled"))
	})
	rh.Handle("GET", "/api/documents/2/history/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte(`{"detail":"Insufficient permissions"}`))
	})
	rh.Handle("GET", "/api/documents/3/history/", jsonHandler(t, 200, []any{}))
	client := testClientAndServer(t, rh)
	h := handleDocumentHistory(client)

	blocks := textBlocks(callTool(t, h, map[string]any{"id": float64(1)}))
	if len(blocks) != 2 || !strings.Contains(blocks[0], "Audit log is disabled") || blocks[1] != auditLogOffHint.text {
		t.Fatalf("audit log off: blocks = %q", blocks)
	}
	result := callTool(t, h, map[string]any{"id": float64(2)})
	assertIsError(t, result)
	if blocks := textBlocks(result); len(blocks) != 1 {
		t.Errorf("403: want no hint, got %q", blocks)
	}
	result = callTool(t, h, map[string]any{"id": float64(3)})
	assertNotError(t, result)
	if blocks := textBlocks(result); len(blocks) != 1 {
		t.Errorf("success: want one block, got %q", blocks)
	}
}

func TestDocumentEmailFailureHint(t *testing.T) {
	status, reply := 500, "Error emailing documents, check logs for more detail."
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/documents/email/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		w.Write([]byte(reply))
	})
	client := testClientAndServer(t, rh)
	args := map[string]any{"documents": "[1]", "to": "user@example.com", "subject": "Doc", "body": "Hi"}

	result := callTool(t, handleDocumentEmail(client), args)
	assertIsError(t, result)
	if blocks := textBlocks(result); len(blocks) != 2 || blocks[1] != emailFailedHint.text {
		t.Fatalf("send failure: blocks = %q", blocks)
	}
	// Another 500 (a proxy, an unhandled server error) gets no hint.
	reply = "<html>Internal Server Error</html>"
	if blocks := textBlocks(callTool(t, handleDocumentEmail(client), args)); len(blocks) != 1 {
		t.Errorf("other 500: want no hint, got %q", blocks)
	}
	// Nor does the same text with another status.
	status, reply = 400, `{"detail":"Error emailing documents"}`
	if blocks := textBlocks(callTool(t, handleDocumentEmail(client), args)); len(blocks) != 1 {
		t.Errorf("400: want no hint, got %q", blocks)
	}
}

func TestWithAPIHints(t *testing.T) {
	hints := []apiHint{{400, "alpha", "hint A"}, {400, "beta", "hint B"}, {500, "alpha", "hint C"}}
	errRes := func() *mcp.CallToolResult { return apiErrorResult(400, []byte("alpha beta"), "GET", "/x/") }

	if blocks := textBlocks(withAPIHints(errRes(), 400, []byte("alpha beta"), hints)); len(blocks) != 3 || blocks[1] != "hint A" || blocks[2] != "hint B" {
		t.Errorf("two matching hints: blocks = %q", blocks)
	}
	if blocks := textBlocks(withAPIHints(errRes(), 400, []byte("gamma"), hints)); len(blocks) != 1 {
		t.Errorf("no phrase match: blocks = %q", blocks)
	}
	if blocks := textBlocks(withAPIHints(errRes(), 404, []byte("alpha"), hints)); len(blocks) != 1 {
		t.Errorf("status mismatch: blocks = %q", blocks)
	}
	ok := mcp.NewToolResultText("fine")
	if blocks := textBlocks(withAPIHints(ok, 400, []byte("alpha"), hints)); len(blocks) != 1 {
		t.Errorf("non-error result changed: blocks = %q", blocks)
	}
	if withAPIHints(nil, 400, []byte("alpha"), hints) != nil {
		t.Error("nil result should stay nil")
	}
}
