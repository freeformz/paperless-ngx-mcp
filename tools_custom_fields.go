package main

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// customFieldDataTypes are the values of CustomField.FieldDataType in
// paperless-ngx (src/documents/models.py).
var customFieldDataTypes = []string{
	"string", "url", "date", "boolean", "integer", "float",
	"monetary", "documentlink", "select", "longtext",
}

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
			mcp.WithDescription("Create a new custom field definition. Choose the data type carefully: paperless-ngx treats it as fixed, and custom_field_update does not change it."),
			mcp.WithString("name", mcp.Description("Field name (must be unique)"), mcp.Required()),
			mcp.WithString("data_type", mcp.Description("Field data type: "+strings.Join(customFieldDataTypes, ", ")), mcp.Required(), mcp.Enum(customFieldDataTypes...)),
			mcp.WithString("extra_data", mcp.Description(`JSON object with type-specific configuration. Required for select: {"select_options": [{"label": "Option A"}, ...]} (ids are generated when omitted). Optional for monetary: {"default_currency": "EUR"} (3-letter code).`)),
		),
		handleCustomFieldCreate(client),
	)

	srv.AddTool(
		mcp.NewTool("custom_field_update",
			mcp.WithDescription("Update a custom field's name or extra_data. The data type is not changed."),
			withNumber("id", mcp.Description("Custom field ID"), mcp.Required()),
			mcp.WithString("name", mcp.Description("Field name")),
			mcp.WithString("extra_data", mcp.Description(`JSON object with type-specific configuration. For select fields, paperless-ngx requires extra_data with the full select_options list on every update, even a rename: pass every option, keeping each existing option's id so documents keep their values. For monetary: {"default_currency": "EUR"}.`)),
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
		return doRequestJSON(resp, err, "GET", path, stripAllIDs)
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

		if !slices.Contains(customFieldDataTypes, dataType) {
			return errResult(fmt.Sprintf("data_type must be one of: %s", strings.Join(customFieldDataTypes, ", "))), nil
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
