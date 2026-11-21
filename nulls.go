package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Some MCP clients send every parameter a tool declares, filling the unused
// ones with null. A top-level null therefore always means "not given", and a
// field is cleared only when it is named in the tool's clear parameter.

// dropNullArgs is tool middleware that removes top-level null arguments
// before any handler runs. The string "null" is removed too, except where it
// is real text and can't be padding: search text and required free-text
// parameters (see keepNullText).
func dropNullArgs(srv *server.MCPServer) server.ToolHandlerMiddleware {
	return func(next server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args, ok := request.Params.Arguments.(map[string]any)
			if !ok {
				return next(ctx, request)
			}
			var props map[string]any
			var required []string
			if tool := srv.GetTool(request.Params.Name); tool != nil {
				props = tool.Tool.InputSchema.Properties
				required = tool.Tool.InputSchema.Required
			}
			clean := make(map[string]any, len(args))
			for k, v := range args {
				if v == nil || (isNullText(v) && !keepNullText(request.Params.Name, k, props[k], required)) {
					continue
				}
				clean[k] = v
			}
			// The arguments map is shared with the caller; replace it rather
			// than editing it, and drop the raw copy so nothing reads nulls back.
			request.Params.Arguments = clean
			request.Params.RawArguments = nil
			return next(ctx, request)
		}
	}
}

// isNullText reports whether v is the string "null", whitespace aside.
func isNullText(v any) bool {
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) == "null"
}

// freeTextParams names the string parameters that hold free text: a title, a
// note, a search query, a name.
var freeTextParams = map[string]bool{
	"body": true, "content": true, "first_name": true, "last_name": true,
	"match": true, "name": true, "note": true, "password": true, "path": true,
	"query": true, "result": true, "subject": true, "term": true, "title": true,
	"username": true,
}

// searchTextParams are free-text parameters that only search, so "null" there
// is harmless: at worst a search for the word.
var searchTextParams = map[string]bool{"query": true, "term": true, "result": true}

// listTextFilters are the free-text filters of the list tools.
var listTextFilters = map[string]bool{"name": true, "title": true, "content": true, "path": true}

// keepNullText reports whether the string "null" is kept as text: for search
// text (search_* params and the list tools' text filters), and for required
// free-text parameters, which a client never fills in as padding. Everywhere
// else, including optional fields that a write would overwrite (a password, a
// title, OCR text), "null" means not given.
func keepNullText(tool, name string, schema any, required []string) bool {
	m, ok := schema.(map[string]any)
	if !ok || m["type"] != "string" || !freeTextParams[name] {
		return false
	}
	switch {
	case searchTextParams[name]:
		return true
	case (strings.HasSuffix(tool, "_list") || tool == "task_stats") && listTextFilters[name]:
		return true
	}
	return slices.Contains(required, name)
}

// clearHint is the guidance for tools that have a clear parameter.
const clearHint = "null values are ignored; to clear a field, name it in clear"

// nullsIgnoredHint is the guidance for update tools without one.
const nullsIgnoredHint = "null values are ignored"

// withClear declares a tool's clear parameter: the fields it may set to null.
func withClear(fields ...string) mcp.ToolOption {
	return withEnumArray("clear", fields, mcp.Description("Array of fields to clear (remove the value): "+strings.Join(fields, ", ")+". A null value elsewhere in the call is ignored, not cleared."))
}

// applyClear reads clear and sets each named field to null in body. A field
// that is also given a value (see argIsSet) is an error.
func applyClear(body map[string]any, request mcp.CallToolRequest, allowed []string) *mcp.CallToolResult {
	fields, errRes := getClearFields(request, allowed)
	if errRes != nil {
		return errRes
	}
	args := request.GetArguments()
	for _, f := range fields {
		if argIsSet(args[f]) {
			return errResult(fmt.Sprintf("%s is both given a value and listed in clear; pick one", f))
		}
		body[f] = nil
	}
	return nil
}

// argIsSet reports whether an argument carries a value: not absent, null,
// "null", "", an empty array (which for display_fields also means clear) or an
// empty object (as some clients pad unused object params).
func argIsSet(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		c := strings.Join(strings.Fields(x), "")
		return c != "" && c != "null" && c != "[]" && c != "{}"
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

// getClearFields reads clear: an array of field names, a JSON-encoded array,
// or a single name. Every name must be in allowed.
func getClearFields(request mcp.CallToolRequest, allowed []string) ([]string, *mcp.CallToolResult) {
	raw := request.GetArguments()["clear"]
	if !argIsSet(raw) {
		return nil, nil
	}
	var names []string
	switch v := raw.(type) {
	case string:
		s := strings.TrimSpace(v)
		switch {
		case s == "" || s == "null":
		case strings.HasPrefix(s, "["):
			if err := json.Unmarshal([]byte(s), &names); err != nil {
				return nil, errResult("clear must be an array of field names")
			}
		default:
			names = []string{s}
		}
	case []any:
		for _, e := range v {
			s, ok := e.(string)
			if !ok {
				return nil, errResult("clear must be an array of field names")
			}
			names = append(names, s)
		}
	default:
		return nil, errResult("clear must be an array of field names")
	}
	for _, n := range names {
		if !slices.Contains(allowed, n) {
			return nil, errResult(fmt.Sprintf("clear: %q can't be cleared here; clearable fields: %s", n, strings.Join(allowed, ", ")))
		}
	}
	return names, nil
}
