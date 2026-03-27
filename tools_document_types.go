package main

import (
	"context"
	"fmt"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerDocumentTypeTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("document_type_list",
			mcp.WithDescription("List document types with optional filtering."),
			mcp.WithString("name", mcp.Description("Filter by name (icontains)")),
			mcp.WithString("ordering", mcp.Description("Sort field")),
			mcp.WithNumber("page", mcp.Description("Page number (default: 1)")),
			mcp.WithNumber("page_size", mcp.Description("Results per page (default: 25)")),
		),
		handleDocumentTypeList(client),
	)

	srv.AddTool(
		mcp.NewTool("document_type_get",
			mcp.WithDescription("Get document type details."),
			mcp.WithNumber("id", mcp.Description("Document type ID"), mcp.Required()),
		),
		handleGetByID(client, "/api/document_types/%d/"),
	)

	srv.AddTool(
		mcp.NewTool("document_type_create",
			mcp.WithDescription("Create a new document type."),
			mcp.WithString("name", mcp.Description("Document type name"), mcp.Required()),
			mcp.WithNumber("matching_algorithm", mcp.Description("Auto-matching algorithm")),
			mcp.WithString("match", mcp.Description("Match pattern")),
			mcp.WithBoolean("is_insensitive", mcp.Description("Case-insensitive matching")),
		),
		handleDocumentTypeCreate(client),
	)

	srv.AddTool(
		mcp.NewTool("document_type_update",
			mcp.WithDescription("Update a document type."),
			mcp.WithNumber("id", mcp.Description("Document type ID"), mcp.Required()),
			mcp.WithString("name", mcp.Description("Document type name")),
			mcp.WithNumber("matching_algorithm", mcp.Description("Auto-matching algorithm")),
			mcp.WithString("match", mcp.Description("Match pattern")),
			mcp.WithBoolean("is_insensitive", mcp.Description("Case-insensitive matching")),
		),
		handleDocumentTypeUpdate(client),
	)

	srv.AddTool(
		mcp.NewTool("document_type_delete",
			mcp.WithDescription("Delete a document type."),
			mcp.WithNumber("id", mcp.Description("Document type ID"), mcp.Required()),
		),
		handleDeleteByID(client, "/api/document_types/%d/"),
	)
}

func handleDocumentTypeList(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := url.Values{}
		addPaginationParams(params, request)
		addStringParam(params, request, "name", "name__icontains")
		addStringParam(params, request, "ordering", "ordering")

		path := "/api/document_types/"
		resp, err := client.Get(ctx, path, params)
		return doRequest(resp, err, "GET", path)
	}
}

func handleDocumentTypeCreate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, errRes := getRequiredString(request, "name")
		if errRes != nil {
			return errRes, nil
		}

		body := map[string]any{"name": name}
		args := request.GetArguments()

		if v := request.GetString("match", ""); v != "" {
			body["match"] = v
		}
		if _, ok := args["matching_algorithm"]; ok {
			body["matching_algorithm"] = int(request.GetFloat("matching_algorithm", 0))
		}
		if _, ok := args["is_insensitive"]; ok {
			body["is_insensitive"] = request.GetBool("is_insensitive", false)
		}

		path := "/api/document_types/"
		resp, err := client.Post(ctx, path, body)
		return doRequest(resp, err, "POST", path)
	}
}

func handleDocumentTypeUpdate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}

		body := map[string]any{}
		args := request.GetArguments()

		if v := request.GetString("name", ""); v != "" {
			body["name"] = v
		}
		if v := request.GetString("match", ""); v != "" {
			body["match"] = v
		}
		if _, ok := args["matching_algorithm"]; ok {
			body["matching_algorithm"] = int(request.GetFloat("matching_algorithm", 0))
		}
		if _, ok := args["is_insensitive"]; ok {
			body["is_insensitive"] = request.GetBool("is_insensitive", false)
		}

		if len(body) == 0 {
			return errResult("no fields to update"), nil
		}

		path := fmt.Sprintf("/api/document_types/%d/", id)
		resp, err := client.Patch(ctx, path, body)
		return doRequest(resp, err, "PATCH", path)
	}
}
