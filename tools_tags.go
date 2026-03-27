package main

import (
	"context"
	"fmt"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerTagTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("tag_list",
			mcp.WithDescription("List all tags with optional filtering."),
			mcp.WithString("name", mcp.Description("Filter by name (icontains)")),
			mcp.WithBoolean("is_root", mcp.Description("Filter root tags only")),
			mcp.WithString("ordering", mcp.Description("Sort field")),
			withNumber("page", mcp.Description("Page number (default: 1)")),
			withNumber("page_size", mcp.Description("Results per page (default: 25)")),
		),
		handleTagList(client),
	)

	srv.AddTool(
		mcp.NewTool("tag_get",
			mcp.WithDescription("Get tag details."),
			withNumber("id", mcp.Description("Tag ID"), mcp.Required()),
		),
		handleGetByID(client, "/api/tags/%d/"),
	)

	srv.AddTool(
		mcp.NewTool("tag_create",
			mcp.WithDescription("Create a new tag."),
			mcp.WithString("name", mcp.Description("Tag name"), mcp.Required()),
			mcp.WithString("color", mcp.Description("Hex color (e.g., #a6cee3)")),
			mcp.WithBoolean("is_inbox_tag", mcp.Description("Whether this is an inbox tag")),
			withNumber("matching_algorithm", mcp.Description("Auto-matching algorithm")),
			mcp.WithString("match", mcp.Description("Match pattern")),
			mcp.WithBoolean("is_insensitive", mcp.Description("Case-insensitive matching")),
			withNullableNumber("parent", mcp.Description("Parent tag ID for hierarchy")),
		),
		handleTagCreate(client),
	)

	srv.AddTool(
		mcp.NewTool("tag_update",
			mcp.WithDescription("Update a tag."),
			withNumber("id", mcp.Description("Tag ID"), mcp.Required()),
			mcp.WithString("name", mcp.Description("Tag name")),
			mcp.WithString("color", mcp.Description("Hex color")),
			mcp.WithBoolean("is_inbox_tag", mcp.Description("Whether this is an inbox tag")),
			withNumber("matching_algorithm", mcp.Description("Auto-matching algorithm")),
			mcp.WithString("match", mcp.Description("Match pattern")),
			mcp.WithBoolean("is_insensitive", mcp.Description("Case-insensitive matching")),
			withNullableNumber("parent", mcp.Description("Parent tag ID")),
		),
		handleTagUpdate(client),
	)

	srv.AddTool(
		mcp.NewTool("tag_delete",
			mcp.WithDescription("Delete a tag."),
			withNumber("id", mcp.Description("Tag ID"), mcp.Required()),
		),
		handleDeleteByID(client, "/api/tags/%d/"),
	)
}

func handleTagList(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := url.Values{}
		addPaginationParams(params, request)
		addStringParam(params, request, "name", "name__icontains")
		addBoolParam(params, request, "is_root", "is_root")
		addStringParam(params, request, "ordering", "ordering")

		path := "/api/tags/"
		resp, err := client.Get(ctx, path, params)
		return doRequest(resp, err, "GET", path)
	}
}

func handleTagCreate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, errRes := getRequiredString(request, "name")
		if errRes != nil {
			return errRes, nil
		}

		body := map[string]any{"name": name}
		args := request.GetArguments()

		if v := request.GetString("color", ""); v != "" {
			body["color"] = v
		}
		if v := request.GetString("match", ""); v != "" {
			body["match"] = v
		}
		if _, ok := args["is_inbox_tag"]; ok {
			body["is_inbox_tag"] = request.GetBool("is_inbox_tag", false)
		}
		if _, ok := args["matching_algorithm"]; ok {
			body["matching_algorithm"] = int(request.GetFloat("matching_algorithm", 0))
		}
		if _, ok := args["is_insensitive"]; ok {
			body["is_insensitive"] = request.GetBool("is_insensitive", false)
		}
		if _, ok := args["parent"]; ok {
			body["parent"] = int(request.GetFloat("parent", 0))
		}

		path := "/api/tags/"
		resp, err := client.Post(ctx, path, body)
		return doRequest(resp, err, "POST", path)
	}
}

func handleTagUpdate(client *Client) server.ToolHandlerFunc {
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
		if v := request.GetString("color", ""); v != "" {
			body["color"] = v
		}
		if v := request.GetString("match", ""); v != "" {
			body["match"] = v
		}
		if _, ok := args["is_inbox_tag"]; ok {
			body["is_inbox_tag"] = request.GetBool("is_inbox_tag", false)
		}
		if _, ok := args["matching_algorithm"]; ok {
			body["matching_algorithm"] = int(request.GetFloat("matching_algorithm", 0))
		}
		if _, ok := args["is_insensitive"]; ok {
			body["is_insensitive"] = request.GetBool("is_insensitive", false)
		}
		setNullableInt(body, args, request, "parent")

		if len(body) == 0 {
			return errResult("no fields to update"), nil
		}

		path := fmt.Sprintf("/api/tags/%d/", id)
		resp, err := client.Patch(ctx, path, body)
		return doRequest(resp, err, "PATCH", path)
	}
}
