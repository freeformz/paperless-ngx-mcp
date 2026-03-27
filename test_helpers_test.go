package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// testClientAndServer creates a test HTTP server and a Client pointing at it.
// The server is automatically closed when the test finishes.
func testClientAndServer(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	return NewClient(ts.URL, "test-token")
}

// jsonHandler returns an http.HandlerFunc that validates auth headers and
// responds with the given status code and JSON-encoded body.
func jsonHandler(t *testing.T, status int, body any) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Token test-token" {
			t.Errorf("expected Authorization: Token test-token, got %s", auth)
		}
		if accept := r.Header.Get("Accept"); accept != "application/json; version=9" {
			t.Errorf("expected Accept: application/json; version=9, got %s", accept)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(body)
	}
}

// paginatedResponse creates a standard Paperless-ngx paginated response envelope.
func paginatedResponse(results any, count int) map[string]any {
	return map[string]any{
		"count":    count,
		"next":     nil,
		"previous": nil,
		"results":  results,
	}
}

// callTool invokes a tool handler with the given arguments and returns the result.
// Fails the test if the handler returns a Go-level error.
func callTool(t *testing.T, handler server.ToolHandlerFunc, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := handler(t.Context(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: args,
		},
	})
	if err != nil {
		t.Fatalf("handler returned error: %s", err)
	}
	return result
}

// resultText extracts the text content from an MCP tool result.
func resultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("no content in result")
	}
	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	return tc.Text
}

// resultJSON extracts the text content from an MCP tool result and parses it as JSON.
func resultJSON(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	text := resultText(t, result)
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("unmarshal result JSON: %s\ntext: %s", err, text)
	}
	return m
}

// resultJSONArray extracts the text content from an MCP tool result and parses it as a JSON array.
func resultJSONArray(t *testing.T, result *mcp.CallToolResult) []any {
	t.Helper()
	text := resultText(t, result)
	var arr []any
	if err := json.Unmarshal([]byte(text), &arr); err != nil {
		t.Fatalf("unmarshal result JSON array: %s\ntext: %s", err, text)
	}
	return arr
}

// assertNotError asserts that the result is not an error.
func assertNotError(t *testing.T, result *mcp.CallToolResult) {
	t.Helper()
	if result.IsError {
		t.Fatalf("expected success, got error: %s", resultText(t, result))
	}
}

// assertIsError asserts that the result is an error.
func assertIsError(t *testing.T, result *mcp.CallToolResult) {
	t.Helper()
	if !result.IsError {
		t.Fatalf("expected error, got success: %s", resultText(t, result))
	}
}

// routeHandler routes requests to different handlers based on method and path.
type routeHandler struct {
	t      *testing.T
	routes map[string]http.HandlerFunc
}

// newRouteHandler creates a new routeHandler for test request routing.
func newRouteHandler(t *testing.T) *routeHandler {
	return &routeHandler{
		t:      t,
		routes: make(map[string]http.HandlerFunc),
	}
}

// Handle registers a handler for a specific method+path combination.
func (rh *routeHandler) Handle(method, path string, handler http.HandlerFunc) {
	rh.routes[method+" "+path] = handler
}

// makeRequest creates an mcp.CallToolRequest with the given arguments.
func makeRequest(args map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: args,
		},
	}
}

func (rh *routeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.Method + " " + r.URL.Path
	if handler, ok := rh.routes[key]; ok {
		handler(w, r)
		return
	}
	rh.t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
	w.WriteHeader(http.StatusNotFound)
}
