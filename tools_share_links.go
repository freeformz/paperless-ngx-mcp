package main

import (
	"context"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerShareLinkTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("share_link_list",
			mcp.WithDescription("List share links with optional date filtering. Results have id, created, expiration (null: never expires), slug, document, file_version and document_title. The public URL of a link is {PAPERLESS_URL}/share/{slug}."),
			mcp.WithString("created_after", mcp.Description("Filter by created date (on or after, YYYY-MM-DD)")),
			mcp.WithString("created_before", mcp.Description("Filter by created date (on or before, YYYY-MM-DD)")),
			mcp.WithString("expiration_after", mcp.Description("Filter by expiration date (on or after, YYYY-MM-DD). Links that never expire are excluded.")),
			mcp.WithString("expiration_before", mcp.Description("Filter by expiration date (on or before, YYYY-MM-DD). Links that never expire are excluded.")),
			mcp.WithString("ordering", mcp.Description("Sort field: created, expiration or document__title; prefix - for descending")),
			withNumber("page", mcp.Description("Page number (default: 1)")),
			withNumber("page_size", mcp.Description("Results per page (default: 25)")),
		),
		handleShareLinkList(client),
	)

	srv.AddTool(
		mcp.NewTool("share_link_get",
			mcp.WithDescription("Get share link details. The public URL of a link is {PAPERLESS_URL}/share/{slug}."),
			withNumber("id", mcp.Description("Share link ID"), mcp.Required()),
		),
		handleGetByID(client, "/api/share_links/%d/"),
	)

	srv.AddTool(
		mcp.NewTool("share_link_create",
			mcp.WithDescription("Create a share link for a document. The server generates a random slug; the public URL is {PAPERLESS_URL}/share/{slug}. Share links can't be edited: to change one, delete it and create a new one, which gets a new slug."),
			withNumber("document", mcp.Description("Document ID"), mcp.Required()),
			mcp.WithString("expiration", mcp.Description("Expiration date (YYYY-MM-DD or ISO 8601); omit for a link that never expires")),
			mcp.WithBoolean("file_version_archive", mcp.Description("Share archived version (default: true)")),
		),
		handleShareLinkCreate(client),
	)

	srv.AddTool(
		mcp.NewTool("share_link_delete",
			mcp.WithDescription("Delete a share link. Its public URL stops working."),
			withNumber("id", mcp.Description("Share link ID"), mcp.Required()),
		),
		handleDeleteByID(client, "/api/share_links/%d/"),
	)
}

func handleShareLinkList(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := url.Values{}
		addPaginationParams(params, request)
		addStringParam(params, request, "created_after", "created__date__gte")
		addStringParam(params, request, "created_before", "created__date__lte")
		addStringParam(params, request, "expiration_after", "expiration__date__gte")
		addStringParam(params, request, "expiration_before", "expiration__date__lte")
		addStringParam(params, request, "ordering", "ordering")

		path := "/api/share_links/"
		resp, err := client.Get(ctx, path, params)
		return doRequest(resp, err, "GET", path)
	}
}

func handleShareLinkCreate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		docID, errRes := getRequiredInt(request, "document")
		if errRes != nil {
			return errRes, nil
		}

		body := map[string]any{"document": docID}
		args := request.GetArguments()

		if v := request.GetString("expiration", ""); v != "" {
			body["expiration"] = v
		}
		if _, ok := args["file_version_archive"]; ok {
			body["file_version"] = "archive"
			if !request.GetBool("file_version_archive", true) {
				body["file_version"] = "original"
			}
		}

		path := "/api/share_links/"
		resp, err := client.Post(ctx, path, body)
		return doRequest(resp, err, "POST", path)
	}
}
