package main

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerBulkTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("document_bulk_edit",
			mcp.WithDescription("Bulk edit documents. Methods: set_correspondent, set_document_type, set_storage_path, add_tag, remove_tag, modify_tags, delete, reprocess, set_permissions, modify_custom_fields, rotate, delete_pages, split, merge, edit_pdf."),
			mcp.WithString("documents", mcp.Description("JSON array of document IDs"), mcp.Required()),
			mcp.WithString("method", mcp.Description("Operation to perform"), mcp.Required()),
			mcp.WithString("parameters", mcp.Description("JSON object with method-specific parameters"), mcp.Required()),
		),
		handleDocumentBulkEdit(client),
	)

	srv.AddTool(
		mcp.NewTool("document_selection_data",
			mcp.WithDescription("Get aggregated metadata counts for a selection of documents. Useful for previewing bulk changes."),
			mcp.WithString("documents", mcp.Description("JSON array of document IDs"), mcp.Required()),
		),
		handleDocumentSelectionData(client),
	)

	srv.AddTool(
		mcp.NewTool("bulk_edit_objects",
			mcp.WithDescription("Bulk permissions/delete for tags, correspondents, document types, or storage paths."),
			mcp.WithString("object_type", mcp.Description("Object type: tags, correspondents, document_types, storage_paths"), mcp.Required()),
			mcp.WithString("objects", mcp.Description("JSON array of object IDs"), mcp.Required()),
			mcp.WithString("operation", mcp.Description("Operation: set_permissions, delete"), mcp.Required()),
			mcp.WithString("parameters", mcp.Description("JSON object with operation-specific parameters")),
		),
		handleBulkEditObjects(client),
	)
}

func handleDocumentBulkEdit(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		body := map[string]any{}

		if err := setJSONField(body, request, "documents"); err != nil {
			return errResult(err.Error()), nil
		}
		if _, ok := body["documents"]; !ok {
			return errResult("documents is required"), nil
		}

		method := request.GetString("method", "")
		if method == "" {
			return errResult("method is required"), nil
		}
		body["method"] = method

		if err := setJSONField(body, request, "parameters"); err != nil {
			return errResult(err.Error()), nil
		}
		if _, ok := body["parameters"]; !ok {
			return errResult("parameters is required"), nil
		}

		path := "/api/documents/bulk_edit/"
		resp, err := client.Post(path, body)
		return doRequest(resp, err, "POST", path)
	}
}

func handleDocumentSelectionData(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		body := map[string]any{}

		if err := setJSONField(body, request, "documents"); err != nil {
			return errResult(err.Error()), nil
		}
		if _, ok := body["documents"]; !ok {
			return errResult("documents is required"), nil
		}

		path := "/api/documents/selection_data/"
		resp, err := client.Post(path, body)
		return doRequest(resp, err, "POST", path)
	}
}

func handleBulkEditObjects(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		objectType := request.GetString("object_type", "")
		if objectType == "" {
			return errResult("object_type is required"), nil
		}

		operation := request.GetString("operation", "")
		if operation == "" {
			return errResult("operation is required"), nil
		}

		body := map[string]any{
			"object_type": objectType,
			"operation":   operation,
		}

		if err := setJSONField(body, request, "objects"); err != nil {
			return errResult(err.Error()), nil
		}
		if _, ok := body["objects"]; !ok {
			return errResult("objects is required"), nil
		}

		if err := setJSONField(body, request, "parameters"); err != nil {
			return errResult(err.Error()), nil
		}

		path := "/api/bulk_edit_objects/"
		resp, err := client.Post(path, body)
		return doRequest(resp, err, "POST", path)
	}
}
