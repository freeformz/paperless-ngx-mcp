package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// errResult returns an MCP error result with the given message.
func errResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{mcp.NewTextContent(msg)},
		IsError: true,
	}
}

// jsonResult marshals a Go value to indented JSON and returns it as an MCP tool result.
func jsonResult(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	return mcp.NewToolResultText(string(b)), nil
}

// rawJSONResult pretty-prints raw JSON bytes and returns them as an MCP tool result.
func rawJSONResult(data []byte) (*mcp.CallToolResult, error) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, data, "", "  "); err != nil {
		return mcp.NewToolResultText(string(data)), nil
	}
	return mcp.NewToolResultText(buf.String()), nil
}

// apiErrorResult creates a structured MCP error result from an HTTP error response.
func apiErrorResult(statusCode int, body []byte, method, path string) *mcp.CallToolResult {
	var detail struct {
		Detail string `json:"detail"`
	}
	detailStr := string(body)
	if json.Unmarshal(body, &detail) == nil && detail.Detail != "" {
		detailStr = detail.Detail
	}

	errResp := map[string]any{
		"error":       true,
		"status_code": statusCode,
		"detail":      detailStr,
		"endpoint":    method + " " + path,
	}
	b, _ := json.MarshalIndent(errResp, "", "  ")
	return &mcp.CallToolResult{
		Content: []mcp.Content{mcp.NewTextContent(string(b))},
		IsError: true,
	}
}

// doRequest handles the common HTTP response pattern: read body, check status, return result.
// It closes the response body. Returns (result, nil) in all cases — tool-level errors
// are returned as MCP error results, not Go errors.
func doRequest(resp *http.Response, err error, method, path string) (*mcp.CallToolResult, error) {
	if err != nil {
		return errResult(fmt.Sprintf("request failed: %s", err)), nil
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return errResult(fmt.Sprintf("read response: %s", readErr)), nil
	}

	if resp.StatusCode >= 400 {
		return apiErrorResult(resp.StatusCode, body, method, path), nil
	}

	// Handle empty responses (e.g., 204 No Content)
	if len(body) == 0 {
		return mcp.NewToolResultText("success"), nil
	}

	return rawJSONResult(body)
}

// addPaginationParams adds page and page_size query parameters if provided in the request.
func addPaginationParams(params url.Values, request mcp.CallToolRequest) {
	if page := request.GetFloat("page", 0); page > 0 {
		params.Set("page", strconv.Itoa(int(page)))
	}
	if pageSize := request.GetFloat("page_size", 0); pageSize > 0 {
		params.Set("page_size", strconv.Itoa(int(pageSize)))
	}
}

// addStringParam adds a string query parameter if the value is non-empty.
func addStringParam(params url.Values, request mcp.CallToolRequest, mcpName, apiName string) {
	if v := request.GetString(mcpName, ""); v != "" {
		params.Set(apiName, v)
	}
}

// addIntParam adds an integer query parameter if explicitly provided in the request.
func addIntParam(params url.Values, request mcp.CallToolRequest, mcpName, apiName string) {
	args := request.GetArguments()
	if _, ok := args[mcpName]; ok {
		params.Set(apiName, strconv.Itoa(int(request.GetFloat(mcpName, 0))))
	}
}

// addBoolParam adds a boolean query parameter if explicitly provided in the request.
func addBoolParam(params url.Values, request mcp.CallToolRequest, mcpName, apiName string) {
	args := request.GetArguments()
	if _, ok := args[mcpName]; ok {
		params.Set(apiName, strconv.FormatBool(request.GetBool(mcpName, false)))
	}
}

// getRequiredInt extracts a required integer parameter from the request.
// Returns the value and nil on success, or 0 and an error result if missing.
func getRequiredInt(request mcp.CallToolRequest, name string) (int, *mcp.CallToolResult) {
	args := request.GetArguments()
	if _, ok := args[name]; !ok {
		return 0, errResult(name + " is required")
	}
	return int(request.GetFloat(name, 0)), nil
}

// getRequiredString extracts a required string parameter from the request.
// Returns the value and nil on success, or "" and an error result if missing or empty.
func getRequiredString(request mcp.CallToolRequest, name string) (string, *mcp.CallToolResult) {
	v := request.GetString(name, "")
	if v == "" {
		return "", errResult(name + " is required")
	}
	return v, nil
}

// setNullableInt sets a nullable integer field in a body map for PATCH requests.
// If the argument is present and nil, sets the field to nil (clears it).
// If the argument is present and a number, sets the field to the int value.
// If the argument is not present, does nothing.
func setNullableInt(body map[string]any, args map[string]any, request mcp.CallToolRequest, name string) {
	val, ok := args[name]
	if !ok {
		return
	}
	if val == nil {
		body[name] = nil
	} else {
		body[name] = int(request.GetFloat(name, 0))
	}
}

// setJSONField parses a JSON string parameter and sets it in a body map.
// Used for array/object fields passed as JSON strings (e.g., tags, custom_fields).
func setJSONField(body map[string]any, request mcp.CallToolRequest, name string) error {
	s := request.GetString(name, "")
	if s == "" {
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return fmt.Errorf("invalid %s JSON: %w", name, err)
	}
	body[name] = v
	return nil
}

// withNumber defines a tool parameter that accepts both number and string values.
// MCP clients may send numbers as strings; the handler uses GetFloat/GetInt
// which handle coercion automatically. Strings are constrained to numeric format
// so invalid values like "abc" are rejected at schema validation.
func withNumber(name string, opts ...mcp.PropertyOption) mcp.ToolOption {
	allOpts := append([]mcp.PropertyOption{func(schema map[string]any) {
		schema["type"] = []string{"number", "string"}
		schema["pattern"] = `^-?\d+(\.\d+)?$`
	}}, opts...)
	return mcp.WithAny(name, allOpts...)
}

// withNullableNumber defines a tool parameter that accepts number, string, or null.
// Used for fields that can be cleared by sending null (e.g., correspondent, document_type).
func withNullableNumber(name string, opts ...mcp.PropertyOption) mcp.ToolOption {
	allOpts := append([]mcp.PropertyOption{func(schema map[string]any) {
		schema["type"] = []string{"number", "string", "null"}
		schema["pattern"] = `^-?\d+(\.\d+)?$`
	}}, opts...)
	return mcp.WithAny(name, allOpts...)
}

// --- Generic CRUD handlers ---

// handleSimpleGet returns a handler that GETs a fixed path with no parameters.
func handleSimpleGet(client *Client, path string) server.ToolHandlerFunc {
	return func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		resp, err := client.Get(ctx, path, nil)
		return doRequest(resp, err, "GET", path)
	}
}

// handlePaginatedList returns a handler that GETs a paginated list endpoint.
func handlePaginatedList(client *Client, path string) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := url.Values{}
		addPaginationParams(params, request)
		resp, err := client.Get(ctx, path, params)
		return doRequest(resp, err, "GET", path)
	}
}

// handleGetByID returns a handler that GETs a resource by integer ID.
// pathFmt must contain exactly one %d verb for the ID.
func handleGetByID(client *Client, pathFmt string) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		path := fmt.Sprintf(pathFmt, id)
		resp, err := client.Get(ctx, path, nil)
		return doRequest(resp, err, "GET", path)
	}
}

// handleDeleteByID returns a handler that DELETEs a resource by integer ID.
// pathFmt must contain exactly one %d verb for the ID.
func handleDeleteByID(client *Client, pathFmt string) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		path := fmt.Sprintf(pathFmt, id)
		resp, err := client.Delete(ctx, path, nil)
		return doRequest(resp, err, "DELETE", path)
	}
}
