package main

import (
	"context"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerSearchTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("search_autocomplete",
			mcp.WithDescription("Autocomplete search terms. Returns suggested completions for partial queries."),
			mcp.WithString("term", mcp.Description("Partial search term to autocomplete"), mcp.Required()),
			withNumber("limit", mcp.Description("Maximum number of suggestions")),
		),
		handleSearchAutocomplete(client),
	)

	srv.AddTool(
		mcp.NewTool("search_global",
			mcp.WithDescription("Global search across all object types (documents, tags, correspondents, etc.). Document content is truncated to a snippet; use document_get or full_content for the complete text."),
			mcp.WithString("query", mcp.Description("Search query"), mcp.Required()),
			withNumber("page", mcp.Description("Page number (default: 1)")),
			withNumber("page_size", mcp.Description("Results per page (default: 25)")),
			mcp.WithBoolean("full_content", mcp.Description("Include full document content in results instead of a truncated snippet (default: false)")),
			withNumber("content_snippet_bytes", mcp.Description("Max bytes of document content per result (default: 500; 0 disables truncation)")),
			mcp.WithBoolean("include_notes", mcp.Description("Include full note objects in document results (default: false — notes are replaced with notes_count; use document_note_list for full notes)")),
		),
		handleSearchGlobal(client),
	)

	srv.AddTool(
		mcp.NewTool("statistics",
			mcp.WithDescription("Get system statistics including document counts, inbox status, and storage usage."),
		),
		handleSimpleGet(client, "/api/statistics/"),
	)
}

func handleSearchAutocomplete(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		term, errRes := getRequiredString(request, "term")
		if errRes != nil {
			return errRes, nil
		}

		params := url.Values{"term": {term}}
		addIntParam(params, request, "limit", "limit")

		path := "/api/search/autocomplete/"
		resp, err := client.Get(ctx, path, params)
		return doRequest(resp, err, "GET", path)
	}
}

func handleSearchGlobal(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, errRes := getRequiredString(request, "query")
		if errRes != nil {
			return errRes, nil
		}

		params := url.Values{"query": {query}}
		addPaginationParams(params, request)

		snippetLen, errRes := getContentSnippetLen(request)
		if errRes != nil {
			return errRes, nil
		}

		path := "/api/search/"
		resp, err := client.Get(ctx, path, params)
		return doRequestJSON(resp, err, "GET", path, func(v any) any {
			m, ok := v.(map[string]any)
			if !ok {
				return v
			}
			if !request.GetBool("full_content", false) {
				truncateContentFields(m["documents"], snippetLen)
			}
			if !request.GetBool("include_notes", false) {
				stripNotesFields(m["documents"])
			}
			return v
		})
	}
}
