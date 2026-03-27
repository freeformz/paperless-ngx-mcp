package main

import (
	"testing"
)

func TestDocumentBulkEdit(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/documents/bulk_edit/", jsonHandler(t, 200, map[string]any{"result": "ok"}))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentBulkEdit(client), map[string]any{
		"documents":  "[1, 2, 3]",
		"method":     "add_tag",
		"parameters": `{"tag": 5}`,
	})
	assertNotError(t, result)
}

func TestDocumentBulkEditRequiresDocuments(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleDocumentBulkEdit(client), map[string]any{
		"method":     "add_tag",
		"parameters": `{"tag": 5}`,
	})
	assertIsError(t, result)
}

func TestDocumentSelectionData(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/documents/selection_data/", jsonHandler(t, 200, map[string]any{
		"selected_correspondents": []any{map[string]any{"id": 1, "document_count": 5}},
	}))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentSelectionData(client), map[string]any{
		"documents": "[1, 2, 3]",
	})
	assertNotError(t, result)
}

func TestBulkEditObjects(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/bulk_edit_objects/", jsonHandler(t, 200, map[string]any{"result": "ok"}))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleBulkEditObjects(client), map[string]any{
		"object_type": "tags",
		"objects":     "[1, 2]",
		"operation":   "delete",
	})
	assertNotError(t, result)
}
