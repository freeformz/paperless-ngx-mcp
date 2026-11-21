package main

import (
	"context"
	"fmt"
	"net/url"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerSearchTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("search_autocomplete",
			mcp.WithDescription("Autocomplete search terms. Returns suggested completions for partial queries, ordered by how many of your documents contain the word."),
			mcp.WithString("term", mcp.Description("Partial search term to autocomplete"), mcp.Required()),
			withNumber("limit", mcp.Description("Maximum number of suggestions")),
		),
		handleSearchAutocomplete(client),
	)

	srv.AddTool(
		mcp.NewTool("search_global",
			mcp.WithDescription("Quick lookup across object types (documents, saved views, tags, correspondents, document types, storage paths, users, groups, mail rules, mail accounts, workflows, custom fields). Returns at most 3 results per type and is not paginated. Documents carry only id, title and created; use document_get for a document's details, and document_list with text or query for real document search."),
			mcp.WithString("query", mcp.Description("Search query, 3 to 4096 characters"), mcp.Required()),
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
		if errRes := addIntParam(params, request, "limit", "limit"); errRes != nil {
			return errRes, nil
		}

		path := "/api/search/autocomplete/"
		resp, err := client.Get(ctx, path, params)
		return doRequest(resp, err, "GET", path)
	}
}

// Query length limits of paperless-ngx's GlobalSearchView.
const (
	minGlobalSearchQueryLen = 3
	maxGlobalSearchQueryLen = 4096
)

func handleSearchGlobal(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, errRes := getRequiredString(request, "query")
		if errRes != nil {
			return errRes, nil
		}

		// paperless-ngx counts characters (code points), not bytes, and
		// returns a 400 for queries outside this range.
		if n := utf8.RuneCountInString(query); n < minGlobalSearchQueryLen || n > maxGlobalSearchQueryLen {
			return errResult(fmt.Sprintf("query must be %d to %d characters, got %d", minGlobalSearchQueryLen, maxGlobalSearchQueryLen, n)), nil
		}

		params := url.Values{"query": {query}}
		path := "/api/search/"
		resp, err := client.Get(ctx, path, params)
		return doRequest(resp, err, "GET", path)
	}
}
