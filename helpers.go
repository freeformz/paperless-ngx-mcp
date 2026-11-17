package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"unicode/utf8"

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

// doRequestJSON is like doRequest but decodes the response body as JSON and
// applies transform to the decoded value before returning it. Error responses
// and non-JSON bodies pass through unchanged.
func doRequestJSON(resp *http.Response, err error, method, path string, transform func(v any) any) (*mcp.CallToolResult, error) {
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

	if len(body) == 0 {
		return mcp.NewToolResultText("success"), nil
	}

	var v any
	if unmarshalErr := json.Unmarshal(body, &v); unmarshalErr != nil {
		return rawJSONResult(body)
	}
	return jsonResult(transform(v))
}

// defaultContentSnippetLen is the default maximum number of bytes of document
// content kept in list and search results.
const defaultContentSnippetLen = 500

// getContentSnippetLen extracts the content_snippet_bytes parameter.
// Returns the default when absent; 0 disables truncation.
func getContentSnippetLen(request mcp.CallToolRequest) (int, *mcp.CallToolResult) {
	n := int(request.GetFloat("content_snippet_bytes", defaultContentSnippetLen))
	if n < 0 {
		return 0, errResult("content_snippet_bytes must be a non-negative integer")
	}
	return n, nil
}

// truncateUTF8 shortens s to at most n bytes without splitting a UTF-8 rune.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// truncateContentFields truncates the "content" field of each object in items
// to limit bytes, marking truncated entries with content_truncated.
// A limit of 0 (or less) disables truncation.
func truncateContentFields(items any, limit int) {
	if limit <= 0 {
		return
	}
	arr, ok := items.([]any)
	if !ok {
		return
	}
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		s, ok := m["content"].(string)
		if !ok || len(s) <= limit {
			continue
		}
		m["content"] = truncateUTF8(s, limit)
		m["content_truncated"] = true
	}
}

// stripNotesFields replaces the "notes" array of each object in items with a
// notes_count field, keeping list results compact. Full notes are available
// via document_note_list or document_get.
func stripNotesFields(items any) {
	arr, ok := items.([]any)
	if !ok {
		return
	}
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		notes, ok := m["notes"].([]any)
		if !ok {
			continue
		}
		delete(m, "notes")
		if len(notes) > 0 {
			m["notes_count"] = len(notes)
		}
	}
}

// paginateArray slices a bare JSON array response into a paginated envelope.
// Used for endpoints (e.g., /api/tasks/) that ignore pagination parameters.
func paginateArray(v any, page, pageSize int) any {
	arr, ok := v.([]any)
	if !ok {
		return v
	}
	// page and pageSize are client-controlled; clamp overflow-to-negative
	// results to "past the end" so slicing can never panic.
	start := (page - 1) * pageSize
	if start < 0 || start > len(arr) {
		start = len(arr)
	}
	end := start + pageSize
	if end < start || end > len(arr) {
		end = len(arr)
	}
	out := map[string]any{
		"count":     len(arr),
		"page":      page,
		"page_size": pageSize,
		"results":   arr[start:end],
	}
	if end < len(arr) {
		out["next_page"] = page + 1
	}
	return out
}

// getPagination extracts page and page_size from the request with defaults.
func getPagination(request mcp.CallToolRequest) (page, pageSize int) {
	page = int(request.GetFloat("page", 1))
	if page < 1 {
		page = 1
	}
	pageSize = int(request.GetFloat("page_size", 25))
	if pageSize < 1 {
		pageSize = 25
	}
	return page, pageSize
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
// If the argument is not present, does nothing; otherwise the value is
// checked by parseNullableInt and an invalid one is returned as an error.
func setNullableInt(body map[string]any, args map[string]any, name string) error {
	val, ok := args[name]
	if !ok {
		return nil
	}
	v, err := parseNullableInt(name, val)
	if err != nil {
		return err
	}
	body[name] = v
	return nil
}

// parseNullableInt converts a raw nullable integer argument into an int or
// nil. JSON null clears the field. Some MCP clients send null as the string
// "null", which is treated the same way. Whole numbers and integer strings
// become the int value. Everything else (fractions, booleans, objects,
// arrays, other strings) is an error rather than being coerced to 0 or
// truncated, so an ID never reaches Paperless that the caller didn't name.
// The empty string is an error as well, not null: it is more likely an
// unset form field than a request to clear the field, and clearing is
// destructive (document_update's archive_serial_number shares this path).
// The accepted strings match the withNullableNumber schema pattern exactly.
func parseNullableInt(name string, val any) (any, error) {
	switch v := val.(type) {
	case nil:
		return nil, nil
	case float64:
		if v != math.Trunc(v) {
			return nil, fmt.Errorf("%s must be an integer or null, got %v", name, v)
		}
		return int(v), nil
	case int:
		return v, nil
	case string:
		if v == "null" {
			return nil, nil
		}
		// Atoi also takes a leading "+", which the schema pattern doesn't.
		n, err := strconv.Atoi(v)
		if err != nil || v[0] == '+' {
			return nil, fmt.Errorf("%s must be an integer or null, got %q", name, v)
		}
		return n, nil
	default:
		return nil, fmt.Errorf("%s must be an integer or null, got %v", name, v)
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

// withOwnership adds the owner and set_permissions parameters accepted by
// create/update tools of owned objects (tags, correspondents, document types,
// storage paths, saved views). Paperless makes the token user the owner on create unless
// owner is sent explicitly, so null is meaningful here.
func withOwnership() mcp.ToolOption {
	return func(t *mcp.Tool) {
		withNullableNumber("owner", mcp.Description("Owner user ID. null = no owner (visible to every user with the model permission). Omit on create to default to the API token's user."))(t)
		withJSONObject("set_permissions", mcp.Description(`JSON object granting object permissions, e.g. {"view":{"users":[],"groups":[2]},"change":{"users":[],"groups":[2]}}. Each users/groups list that is given replaces the current one for that action ([] clears it); omitted actions and lists stay unchanged. change implies view. Changing owner or permissions requires the token user to own the object, the object to be unowned, or a superuser token.`))(t)
	}
}

// setOwnership copies owner (nullable) and set_permissions (JSON object)
// from the request into body when present.
func setOwnership(body map[string]any, request mcp.CallToolRequest) error {
	if err := setNullableInt(body, request.GetArguments(), "owner"); err != nil {
		return err
	}
	return setJSONObjectField(body, request, "set_permissions")
}

// setJSONObjectField sets a JSON object parameter in a body map. MCP clients
// send it either as an object or as a JSON-encoded string, so both are
// accepted; null counts as absent. See parseJSONObject for what is refused.
func setJSONObjectField(body map[string]any, request mcp.CallToolRequest, name string) error {
	obj, err := parseJSONObject(name, request.GetArguments()[name])
	if err != nil || obj == nil {
		return err
	}
	body[name] = obj
	return nil
}

// parseJSONObject converts a raw argument into a JSON object: an object is
// taken as is and a string is parsed as JSON. nil returns (nil, nil) so the
// caller decides whether null is allowed. Anything else, including "" and an
// empty object, is an error instead of being dropped: Paperless treats an
// empty permissions object as "change nothing" and would still report
// success.
func parseJSONObject(name string, val any) (map[string]any, error) {
	var obj map[string]any
	switch v := val.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		obj = v
	case string:
		if err := json.Unmarshal([]byte(v), &obj); err != nil {
			return nil, fmt.Errorf("%s must be a JSON object: %w", name, err)
		}
		if obj == nil {
			return nil, fmt.Errorf("%s must be a JSON object, got %q", name, v)
		}
	default:
		return nil, fmt.Errorf("%s must be a JSON object, got %v", name, v)
	}
	if len(obj) == 0 {
		return nil, fmt.Errorf("%s must not be an empty object", name)
	}
	return obj, nil
}

// withFullPerms adds the full_perms parameter to read tools of owned objects.
func withFullPerms() mcp.ToolOption {
	return mcp.WithBoolean("full_perms", mcp.Description("Include the permissions field (view/change users and groups). Replaces user_can_change in the response."))
}

// addFullPerms sets full_perms=true when the caller asked for it and leaves
// the parameter out otherwise. Paperless-ngx 2.16 passes the raw query value
// on, so the non-empty string "false" counts as true there; and any query
// parameter bypasses the metadata cache in Client.Get.
func addFullPerms(params url.Values, request mcp.CallToolRequest) {
	if request.GetBool("full_perms", false) {
		params.Set("full_perms", "true")
	}
}

// withNumber defines a tool parameter that accepts both integer and string values.
// MCP clients may send numbers as strings; the handler uses GetFloat/GetInt
// which handle coercion automatically. Strings are constrained to integer format
// so invalid values like "abc" or "1.5" are rejected at schema validation.
func withNumber(name string, opts ...mcp.PropertyOption) mcp.ToolOption {
	allOpts := append([]mcp.PropertyOption{func(schema map[string]any) {
		schema["type"] = []string{"integer", "string"}
		schema["pattern"] = `^-?\d+$`
	}}, opts...)
	return mcp.WithAny(name, allOpts...)
}

// withNullableNumber defines a tool parameter that accepts integer, string, or null.
// Used for fields that can be cleared by sending null (e.g., correspondent, document_type).
// Strings are integers or "null", the form some MCP clients send for JSON null;
// the handler side is parseNullableInt.
func withNullableNumber(name string, opts ...mcp.PropertyOption) mcp.ToolOption {
	allOpts := append([]mcp.PropertyOption{func(schema map[string]any) {
		schema["type"] = []string{"integer", "string", "null"}
		schema["pattern"] = `^(-?\d+|null)$`
	}}, opts...)
	return mcp.WithAny(name, allOpts...)
}

// withJSONObject defines a tool parameter that takes a JSON object, either as
// an object or as a JSON-encoded string. See setJSONObjectField.
func withJSONObject(name string, opts ...mcp.PropertyOption) mcp.ToolOption {
	allOpts := append([]mcp.PropertyOption{func(schema map[string]any) {
		schema["type"] = []string{"object", "string"}
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

// stripAllIDs removes the unbounded "all" ID array Paperless includes
// alongside each page of a list response. Only document_list offers an
// opt-out (include_all_ids) for bulk-select workflows.
func stripAllIDs(v any) any {
	if m, ok := v.(map[string]any); ok {
		delete(m, "all")
	}
	return v
}

// paramHook adds tool-specific query parameters to a generic read handler,
// e.g. addFullPerms for owned objects.
type paramHook func(url.Values, mcp.CallToolRequest)

// handlePaginatedList returns a handler that GETs a paginated list endpoint.
// hooks add tool-specific query parameters after the pagination ones.
func handlePaginatedList(client *Client, path string, hooks ...paramHook) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := url.Values{}
		addPaginationParams(params, request)
		for _, hook := range hooks {
			hook(params, request)
		}
		resp, err := client.Get(ctx, path, params)
		return doRequestJSON(resp, err, "GET", path, stripAllIDs)
	}
}

// handleGetByID returns a handler that GETs a resource by integer ID.
// pathFmt must contain exactly one %d verb for the ID. hooks add
// tool-specific query parameters.
func handleGetByID(client *Client, pathFmt string, hooks ...paramHook) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		params := url.Values{}
		for _, hook := range hooks {
			hook(params, request)
		}
		path := fmt.Sprintf(pathFmt, id)
		resp, err := client.Get(ctx, path, params)
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
