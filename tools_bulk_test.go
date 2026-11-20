package main

import (
	"reflect"
	"slices"
	"strings"
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

func TestDocumentSelectionDataFiltersZeroCounts(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/documents/selection_data/", jsonHandler(t, 200, map[string]any{
		"selected_correspondents": []any{
			map[string]any{"id": 1, "document_count": 2},
			map[string]any{"id": 2, "document_count": 0},
			map[string]any{"id": 3, "document_count": 0},
		},
		"selected_tags": []any{
			map[string]any{"id": 9, "document_count": 0},
		},
	}))

	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentSelectionData(client), map[string]any{
		"documents": "[1, 2]",
	})
	assertNotError(t, result)

	m := resultJSON(t, result)
	correspondents := m["selected_correspondents"].([]any)
	if len(correspondents) != 1 {
		t.Errorf("selected_correspondents length = %d, want 1", len(correspondents))
	}
	if id := correspondents[0].(map[string]any)["id"]; id != float64(1) {
		t.Errorf("remaining correspondent id = %v, want 1", id)
	}
	if tags := m["selected_tags"].([]any); len(tags) != 0 {
		t.Errorf("selected_tags length = %d, want 0", len(tags))
	}
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

func TestBulkEditObjectsInvalidatesCache(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/tags/", jsonHandler(t, 200, paginatedResponse([]any{}, 0)))
	rh.Handle("POST", "/api/bulk_edit_objects/", jsonHandler(t, 200, map[string]any{"result": "ok"}))

	client := testClientAndServer(t, rh)

	// Prime the tags cache.
	resp, err := client.Get(t.Context(), "/api/tags/", nil)
	if err != nil {
		t.Fatalf("prime cache: %s", err)
	}
	resp.Body.Close()
	if _, ok := client.cache.Get("/api/tags/"); !ok {
		t.Fatal("expected tags list to be cached")
	}

	result := callTool(t, handleBulkEditObjects(client), map[string]any{
		"object_type": "tags",
		"objects":     "[1, 2]",
		"operation":   "delete",
	})
	assertNotError(t, result)

	if _, ok := client.cache.Get("/api/tags/"); ok {
		t.Fatal("expected tags cache to be invalidated after bulk_edit_objects")
	}
}

func TestDocumentBulkEditMovedMethods(t *testing.T) {
	for method, tool := range bulkEditMovedMethods {
		t.Run(method, func(t *testing.T) {
			client := testClientAndServer(t, newRouteHandler(t))
			result := callTool(t, handleDocumentBulkEdit(client), map[string]any{
				"documents": []any{float64(1)}, "method": method, "parameters": map[string]any{},
			})
			assertIsError(t, result)
			if !strings.Contains(resultText(t, result), tool) {
				t.Errorf("error = %s, want it to name %s", resultText(t, result), tool)
			}
		})
	}
}

func TestDocumentBulkEditUnknownMethod(t *testing.T) {
	client := testClientAndServer(t, newRouteHandler(t))
	result := callTool(t, handleDocumentBulkEdit(client), map[string]any{
		"documents": []any{float64(1)}, "method": "frobnicate", "parameters": map[string]any{},
	})
	assertIsError(t, result)
	if !strings.Contains(resultText(t, result), "method must be one of") {
		t.Errorf("error = %s", resultText(t, result))
	}
}

func TestDocumentBulkEditMethodEnum(t *testing.T) {
	srv := NewServer(NewClient("http://unused", "unused"), nil)
	schema := srv.GetTool("document_bulk_edit").Tool.InputSchema.Properties["method"].(map[string]any)
	enum, _ := schema["enum"].([]string)
	if !reflect.DeepEqual(enum, bulkEditMethods) {
		t.Errorf("method enum = %v, want %v", schema["enum"], bulkEditMethods)
	}
	for moved := range bulkEditMovedMethods {
		if slices.Contains(bulkEditMethods, moved) {
			t.Errorf("%s is both a bulk_edit method and moved", moved)
		}
	}
}

func TestDocumentBulkEditWaitHasNoFollowOn(t *testing.T) {
	fastPolling(t)
	client, _ := bulkWaitServer(t, "pending", "success")
	result := callTool(t, handleDocumentBulkEdit(client), map[string]any{
		"documents": []any{float64(1)}, "method": "add_tag", "parameters": map[string]any{"tag": float64(1)}, "wait": true,
	})
	assertNotError(t, result)
	if m := resultJSON(t, result); m["follow_on"] != nil || m["task"].(map[string]any)["status"] != "success" {
		t.Errorf("result = %v, want the finished task and no follow_on", m)
	}
}
