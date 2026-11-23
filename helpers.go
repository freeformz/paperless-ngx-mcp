package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
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

// doRequestBody is like doRequest but returns the raw response body for the
// caller to use, or an error result for a failed request.
func doRequestBody(resp *http.Response, err error, method, path string) ([]byte, *mcp.CallToolResult) {
	if err != nil {
		return nil, errResult(fmt.Sprintf("request failed: %s", err))
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, errResult(fmt.Sprintf("read response: %s", readErr))
	}
	if resp.StatusCode >= 400 {
		return nil, apiErrorResult(resp.StatusCode, body, method, path)
	}
	return body, nil
}

// httpResponse names *http.Response's type for files that don't import net/http.
type httpResponse = http.Response

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
	n, given, errRes := optionalInt(request, "content_snippet_bytes")
	if errRes != nil {
		return 0, errRes
	}
	if !given {
		n = defaultContentSnippetLen
	}
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

// truncateUTF8Tail keeps the last n bytes of s without splitting a UTF-8 rune.
func truncateUTF8Tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	start := len(s) - max(n, 0)
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
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
// via document_note_list or document_get (both read the root document's notes).
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

// addMultiParam adds a multi-value filter as repeated query parameters
// (?name=a&name=b), which is how django-filter's MultipleChoiceFilter reads
// them. The argument may be an array of strings, a JSON-encoded array, or a
// single string (one value). Every
// value must be in allowed; otherwise an error result is returned and no
// parameter is added.
func addMultiParam(params url.Values, request mcp.CallToolRequest, name string, allowed []string) *mcp.CallToolResult {
	raw := request.GetArguments()[name]
	if !argIsSet(raw) {
		return nil
	}
	var vals []string
	switch v := raw.(type) {
	case string:
		if strings.HasPrefix(strings.TrimSpace(v), "[") {
			// A JSON-encoded array, as for every other array parameter.
			var arr []string
			if err := json.Unmarshal([]byte(v), &arr); err != nil {
				return errResult(name + " must be an array of strings")
			}
			vals = arr
		} else if v != "" {
			vals = []string{v}
		}
	case []any:
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				return errResult(name + " must be an array of strings")
			}
			vals = append(vals, s)
		}
	case []string:
		vals = v
	default:
		return errResult(name + " must be an array of strings")
	}
	for _, s := range vals {
		if !slices.Contains(allowed, s) {
			return errResult(fmt.Sprintf("%s: invalid value %q; must be one of: %s", name, s, strings.Join(allowed, ", ")))
		}
	}
	for _, s := range vals {
		params.Add(name, s)
	}
	return nil
}

// addPaginationParams adds page and page_size query parameters if provided in the request.
func addPaginationParams(params url.Values, request mcp.CallToolRequest) *mcp.CallToolResult {
	for _, name := range []string{"page", "page_size"} {
		n, given, errRes := optionalInt(request, name)
		if errRes != nil {
			return errRes
		}
		// 0 is not given, as some clients pad numbers with it.
		if !given || n == 0 {
			continue
		}
		if n < 0 {
			return errResult(name + " must be at least 1")
		}
		params.Set(name, strconv.Itoa(n))
	}
	return nil
}

// optionalInt reads an optional whole-number argument (see setOptionalInt).
func optionalInt(request mcp.CallToolRequest, name string) (n int, given bool, errRes *mcp.CallToolResult) {
	v := map[string]any{}
	if errRes := setOptionalInt(v, request.GetArguments(), name); errRes != nil {
		return 0, false, errRes
	}
	n, given = v[name].(int)
	return n, given, nil
}

// optionalBool reads an optional boolean argument (see setOptionalBool),
// returning def when it isn't given.
func optionalBool(request mcp.CallToolRequest, name string, def bool) (bool, *mcp.CallToolResult) {
	v := map[string]any{}
	if errRes := setOptionalBool(v, request, name); errRes != nil {
		return def, errRes
	}
	if b, ok := v[name].(bool); ok {
		return b, nil
	}
	return def, nil
}

// setOptionalJSONObject is setJSONField for an optional object parameter: an
// empty object ({}, as some clients pad unused params) is not given.
func setOptionalJSONObject(body map[string]any, request mcp.CallToolRequest, name string) error {
	v, present, err := getJSONArg(request, name)
	if err != nil || !present {
		return err
	}
	if m, ok := v.(map[string]any); ok && len(m) == 0 {
		return nil
	}
	body[name] = v
	return nil
}

// addStringParam adds a string query parameter if the value is non-empty.
func addStringParam(params url.Values, request mcp.CallToolRequest, mcpName, apiName string) {
	if v := request.GetString(mcpName, ""); v != "" {
		params.Set(apiName, v)
	}
}

// addIntParam adds an integer query parameter if explicitly provided in the
// request. null (and "null" or "") means not given; a value that isn't a
// whole number is an error, not a silent 0.
func addIntParam(params url.Values, request mcp.CallToolRequest, mcpName, apiName string) *mcp.CallToolResult {
	v := map[string]any{}
	if errRes := setOptionalInt(v, request.GetArguments(), mcpName); errRes != nil {
		return errRes
	}
	if n, ok := v[mcpName].(int); ok {
		params.Set(apiName, strconv.Itoa(n))
	}
	return nil
}

// addBoolParam adds a boolean query parameter if explicitly provided in the
// request. null (and "null" or "") means not given; a value that isn't a
// boolean is an error, not a silent false.
func addBoolParam(params url.Values, request mcp.CallToolRequest, mcpName, apiName string) *mcp.CallToolResult {
	v := map[string]any{}
	if errRes := setOptionalBool(v, request, mcpName); errRes != nil {
		return errRes
	}
	if b, ok := v[mcpName].(bool); ok {
		params.Set(apiName, strconv.FormatBool(b))
	}
	return nil
}

// getRequiredInt extracts a required integer parameter from the request.
// Returns the value and nil on success, or 0 and an error result if missing.
func getRequiredInt(request mcp.CallToolRequest, name string) (int, *mcp.CallToolResult) {
	args := request.GetArguments()
	v, ok := args[name]
	if !ok || !argIsSet(v) {
		return 0, errResult(name + " is required")
	}
	n, whole := wholeNumber(v)
	if !whole {
		return 0, errResult(wholeNumberError(name, v, maxWholeNumber))
	}
	return n, nil
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

// setOptionalInt copies an integer argument into body when it has a value.
// null, "null" and "" mean not given (clearing a field goes through clear);
// anything else that isn't a whole number is an error.
func setOptionalInt(body map[string]any, args map[string]any, name string) *mcp.CallToolResult {
	val := args[name]
	if !argIsSet(val) {
		return nil
	}
	limit, ok := intParamLimits[name]
	if !ok {
		limit = maxWholeNumber
	}
	n, whole := wholeNumberUpTo(val, limit)
	if !whole {
		return errResult(wholeNumberError(name, val, limit))
	}
	body[name] = n
	return nil
}

// intParamLimits holds the fields whose range is wider than maxWholeNumber:
// paperless-ngx allows archive serial numbers up to 0xFFFFFFFF.
var intParamLimits = map[string]float64{"archive_serial_number": 0xFFFFFFFF}

// getJSONArg reads a structured (array or object) parameter. Agents may send
// the value natively or as a JSON-encoded string; both give the same result.
// An absent argument, null, "" and a string that decodes to JSON null (such
// as "null") report present = false.
func getJSONArg(request mcp.CallToolRequest, name string) (v any, present bool, err error) {
	raw, ok := request.GetArguments()[name]
	if !ok || raw == nil {
		return nil, false, nil
	}
	s, isString := raw.(string)
	if !isString {
		return raw, true, nil
	}
	if s == "" {
		return nil, false, nil
	}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, false, fmt.Errorf("invalid %s JSON: %w", name, err)
	}
	return v, v != nil, nil
}

// setJSONField sets a structured parameter in a body map, accepting either a
// native JSON value or a JSON-encoded string (see getJSONArg). Used for
// array/object fields such as tags, custom_fields and workflow bodies.
func setJSONField(body map[string]any, request mcp.CallToolRequest, name string) error {
	v, present, err := getJSONArg(request, name)
	if err != nil || !present {
		return err
	}
	body[name] = v
	return nil
}

// getJSONInts reads an array-of-integers parameter given natively or as a
// JSON-encoded string.
func getJSONInts(request mcp.CallToolRequest, name string) (ids []int, present bool, err error) {
	v, present, err := getJSONArg(request, name)
	if err != nil || !present || v == nil {
		return nil, false, err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, true, fmt.Errorf("invalid %s: %w", name, err)
	}
	if err := json.Unmarshal(b, &ids); err != nil {
		return nil, true, fmt.Errorf("%s must be an array of integers", name)
	}
	return ids, true, nil
}

// getJSONArgText returns a structured parameter as JSON text, re-encoding a
// native value, for handlers that parse the text themselves. "" means absent.
func getJSONArgText(request mcp.CallToolRequest, name string) (string, error) {
	raw, ok := request.GetArguments()[name]
	if !ok || raw == nil {
		return "", nil
	}
	if s, isString := raw.(string); isString {
		return s, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return "", fmt.Errorf("invalid %s: %w", name, err)
	}
	return string(b), nil
}

// withEnumArray defines a multi-value filter: an array of enum strings, a
// JSON-encoded array, or a single value. Read it with addMultiParam.
func withEnumArray(name string, values []string, opts ...mcp.PropertyOption) mcp.ToolOption {
	allOpts := append([]mcp.PropertyOption{func(schema map[string]any) {
		schema["type"] = []string{"array", "string"}
		schema["items"] = map[string]any{"type": "string", "enum": values}
	}}, opts...)
	return mcp.WithAny(name, allOpts...)
}

// withJSONArray defines a parameter that takes an array, natively or as a
// JSON-encoded string. Read it with getJSONArg or setJSONField.
func withJSONArray(name string, opts ...mcp.PropertyOption) mcp.ToolOption {
	allOpts := append([]mcp.PropertyOption{func(schema map[string]any) {
		schema["type"] = []string{"array", "string"}
	}}, opts...)
	return mcp.WithAny(name, allOpts...)
}

// withJSONObject defines a parameter that takes an object, natively or as a
// JSON-encoded string. Read it with getJSONArg or setJSONField.
func withJSONObject(name string, opts ...mcp.PropertyOption) mcp.ToolOption {
	allOpts := append([]mcp.PropertyOption{func(schema map[string]any) {
		schema["type"] = []string{"object", "string"}
	}}, opts...)
	return mcp.WithAny(name, allOpts...)
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
		if errRes := addPaginationParams(params, request); errRes != nil {
			return errRes, nil
		}
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
