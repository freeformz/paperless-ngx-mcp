package main

import (
	"context"
	"fmt"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerCustomFieldTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("custom_field_list",
			mcp.WithDescription("List custom field definitions."),
			mcp.WithString("name", mcp.Description("Filter by name (icontains)")),
			mcp.WithString("ordering", mcp.Description("Sort field")),
			withNumber("page", mcp.Description("Page number (default: 1)")),
			withNumber("page_size", mcp.Description("Results per page (default: 25)")),
		),
		handleCustomFieldList(client),
	)

	srv.AddTool(
		mcp.NewTool("custom_field_get",
			mcp.WithDescription("Get custom field details."),
			withNumber("id", mcp.Description("Custom field ID"), mcp.Required()),
		),
		handleGetByID(client, "/api/custom_fields/%d/"),
	)

	srv.AddTool(
		mcp.NewTool("custom_field_create",
			mcp.WithDescription("Create a new custom field definition."),
			mcp.WithString("name", mcp.Description("Field name"), mcp.Required()),
			mcp.WithString("data_type", mcp.Description("Field data type (string, url, date, boolean, integer, float, monetary, document_link, select)"), mcp.Required()),
			mcp.WithString("extra_data", mcp.Description("JSON object with type-specific configuration (e.g., select options)")),
		),
		handleCustomFieldCreate(client),
	)

	srv.AddTool(
		mcp.NewTool("custom_field_update",
			mcp.WithDescription("Update a custom field definition."),
			withNumber("id", mcp.Description("Custom field ID"), mcp.Required()),
			mcp.WithString("name", mcp.Description("Field name")),
			mcp.WithString("extra_data", mcp.Description("JSON object with type-specific configuration")),
		),
		handleCustomFieldUpdate(client),
	)

	srv.AddTool(
		mcp.NewTool("custom_field_delete",
			mcp.WithDescription("Delete a custom field definition."),
			withNumber("id", mcp.Description("Custom field ID"), mcp.Required()),
		),
		handleDeleteByID(client, "/api/custom_fields/%d/"),
	)
}

func handleCustomFieldList(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := url.Values{}
		addPaginationParams(params, request)
		addStringParam(params, request, "name", "name__icontains")
		addStringParam(params, request, "ordering", "ordering")

		path := "/api/custom_fields/"
		resp, err := client.Get(ctx, path, params)
		return doRequest(resp, err, "GET", path)
	}
}

func handleCustomFieldCreate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, errRes := getRequiredString(request, "name")
		if errRes != nil {
			return errRes, nil
		}
		dataType, errRes := getRequiredString(request, "data_type")
		if errRes != nil {
			return errRes, nil
		}

		body := map[string]any{"name": name, "data_type": dataType}

		if err := setJSONField(body, request, "extra_data"); err != nil {
			return errResult(err.Error()), nil
		}

		path := "/api/custom_fields/"
		resp, err := client.Post(ctx, path, body)
		return doRequest(resp, err, "POST", path)
	}
}

func handleCustomFieldUpdate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}

		body := map[string]any{}
		if v := request.GetString("name", ""); v != "" {
			body["name"] = v
		}
		if err := setJSONField(body, request, "extra_data"); err != nil {
			return errResult(err.Error()), nil
		}

		if len(body) == 0 {
			return errResult("no fields to update"), nil
		}

		path := fmt.Sprintf("/api/custom_fields/%d/", id)
		resp, err := client.Patch(ctx, path, body)
		return doRequest(resp, err, "PATCH", path)
	}
}
