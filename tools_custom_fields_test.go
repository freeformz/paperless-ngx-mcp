package main

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

func TestCustomFieldList(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/custom_fields/", jsonHandler(t, 200, paginatedResponse([]map[string]any{{"id": 1, "name": "Due Date"}}, 1)))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleCustomFieldList(client), nil)
	assertNotError(t, result)
}

func TestCustomFieldGet(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/custom_fields/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "Due Date", "data_type": "date"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleGetByID(client, "/api/custom_fields/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestCustomFieldCreate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/custom_fields/", jsonHandler(t, 201, map[string]any{"id": 1, "name": "Amount", "data_type": "monetary"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleCustomFieldCreate(client), map[string]any{"name": "Amount", "data_type": "monetary"})
	assertNotError(t, result)
}

func TestCustomFieldCreateDataTypes(t *testing.T) {
	for _, dataType := range customFieldDataTypes {
		t.Run(dataType, func(t *testing.T) {
			var capturedBody map[string]any
			rh := newRouteHandler(t)
			rh.Handle("POST", "/api/custom_fields/", func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				json.Unmarshal(body, &capturedBody)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(201)
				w.Write([]byte(`{"id":1}`))
			})
			client := testClientAndServer(t, rh)
			result := callTool(t, handleCustomFieldCreate(client), map[string]any{"name": "Field", "data_type": dataType})
			assertNotError(t, result)
			if capturedBody["data_type"] != dataType {
				t.Errorf("data_type = %v, want %q", capturedBody["data_type"], dataType)
			}
		})
	}
}

func TestCustomFieldCreateInvalidDataType(t *testing.T) {
	for _, dataType := range []string{"document_link", "long_text", "text", "String"} {
		t.Run(dataType, func(t *testing.T) {
			called := false
			rh := newRouteHandler(t)
			rh.Handle("POST", "/api/custom_fields/", func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(201)
			})
			client := testClientAndServer(t, rh)
			result := callTool(t, handleCustomFieldCreate(client), map[string]any{"name": "Field", "data_type": dataType})
			assertIsError(t, result)
			if called {
				t.Error("server should not be called when data_type is invalid")
			}
			msg := resultText(t, result)
			for _, want := range []string{"documentlink", "longtext"} {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q should name accepted value %q", msg, want)
				}
			}
		})
	}
}

func TestCustomFieldCreateDataTypeSchemaEnum(t *testing.T) {
	srv := server.NewMCPServer("test", "0")
	registerCustomFieldTools(srv, NewClient("http://unused", "unused"))
	tool := srv.GetTool("custom_field_create")
	if tool == nil {
		t.Fatal("custom_field_create not registered")
	}
	prop, ok := tool.Tool.InputSchema.Properties["data_type"].(map[string]any)
	if !ok {
		t.Fatalf("data_type property missing or wrong shape: %#v", tool.Tool.InputSchema.Properties["data_type"])
	}
	got, _ := prop["enum"].([]string)
	if !slices.Equal(got, customFieldDataTypes) {
		t.Errorf("data_type enum = %v, want %v", prop["enum"], customFieldDataTypes)
	}
}

func TestCustomFieldCreateRequiresFields(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleCustomFieldCreate(client), map[string]any{})
	assertIsError(t, result)
	result = callTool(t, handleCustomFieldCreate(client), map[string]any{"name": "test"})
	assertIsError(t, result)
}

func TestCustomFieldUpdate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/custom_fields/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "Updated"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleCustomFieldUpdate(client), map[string]any{"id": float64(1), "name": "Updated"})
	assertNotError(t, result)
}

func TestCustomFieldDelete(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("DELETE", "/api/custom_fields/1/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDeleteByID(client, "/api/custom_fields/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}
