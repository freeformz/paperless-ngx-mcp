package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerBulkTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("document_bulk_edit",
			append([]mcp.ToolOption{
				mcp.WithDescription("Bulk edit document metadata. parameters by method: set_correspondent {correspondent: id or null}; set_document_type {document_type: id or null}; set_storage_path {storage_path: id or null}; add_tag / remove_tag {tag: id}; modify_tags {add_tags: [id], remove_tags: [id]} (both required, may be empty); modify_custom_fields {add_custom_fields: [id] or {id: value}, remove_custom_fields: [id]} (both required); set_permissions {set_permissions: {view?: {users?: [id], groups?: [id]}, change?: {...}} or null, owner?: id or null, merge?: bool} (needs ownership; without merge, leaving out owner clears it); run_workflow {workflow: id, filter_documents?: bool, default true} (needs view_workflow permission, an enabled workflow with a manual trigger, and ownership). Every method needs change permission on every document. The server queues a bulk task and returns its task_id at once; errors show up in that task. Pass wait: true to wait for it; for these methods the bulk task finishing means the change is done. File operations have their own tools: rotate -> document_rotate, merge -> document_merge, edit_pdf / split / delete_pages -> document_edit_pdf, remove_password -> document_remove_password, reprocess -> document_reprocess, delete -> document_bulk_delete."),
				withJSONArray("documents", mcp.Description("Array of document IDs"), mcp.Required()),
				mcp.WithString("method", mcp.Description("Operation to perform"), mcp.Required(), mcp.Enum(bulkEditMethods...)),
				withJSONObject("parameters", mcp.Description("Object with method-specific parameters, see the description"), mcp.Required()),
			}, waitToolOptions()...)...,
		),
		handleDocumentBulkEdit(client),
	)

	srv.AddTool(
		mcp.NewTool("document_selection_data",
			mcp.WithDescription("Get aggregated metadata counts for a selection of documents. Useful for previewing bulk changes. Objects with zero matching documents are omitted."),
			withJSONArray("documents", mcp.Description("Array of document IDs"), mcp.Required()),
		),
		handleDocumentSelectionData(client),
	)

	srv.AddTool(
		mcp.NewTool("bulk_edit_objects",
			mcp.WithDescription("Bulk permissions/delete for tags, correspondents, document types, or storage paths."),
			mcp.WithString("object_type", mcp.Description("Object type: tags, correspondents, document_types, storage_paths"), mcp.Required()),
			withJSONArray("objects", mcp.Description("Array of object IDs"), mcp.Required()),
			mcp.WithString("operation", mcp.Description("Operation: set_permissions, delete"), mcp.Required()),
			withJSONObject("parameters", mcp.Description("Object with operation-specific parameters")),
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

		method, errRes := getRequiredString(request, "method")
		if errRes != nil {
			return errRes, nil
		}
		if tool, moved := bulkEditMovedMethods[method]; moved {
			return errResult(fmt.Sprintf("document_bulk_edit no longer runs %s; use %s", method, tool)), nil
		}
		if !slices.Contains(bulkEditMethods, method) {
			return errResult("method must be one of: " + strings.Join(bulkEditMethods, ", ")), nil
		}
		body["method"] = method

		if err := setJSONField(body, request, "parameters"); err != nil {
			return errResult(err.Error()), nil
		}
		if _, ok := body["parameters"]; !ok {
			return errResult("parameters is required"), nil
		}

		wait, timeout, errRes := getWaitOptions(request)
		if errRes != nil {
			return errRes, nil
		}

		path := "/api/documents/bulk_edit/"
		resp, err := client.Post(ctx, path, body)
		// Metadata methods finish inside the bulk task: nothing follows on.
		return respondWithWait(ctx, client, request, wait, timeout, resp, err, "POST", path, nil)
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
		resp, err := client.Post(ctx, path, body)
		// The endpoint returns every object in the instance, almost all with
		// document_count: 0 — keep only objects present in the selection.
		return doRequestJSON(resp, err, "POST", path, func(v any) any {
			m, ok := v.(map[string]any)
			if !ok {
				return v
			}
			for key, val := range m {
				if !strings.HasPrefix(key, "selected_") {
					continue
				}
				arr, ok := val.([]any)
				if !ok {
					continue
				}
				filtered := make([]any, 0, len(arr))
				for _, item := range arr {
					if obj, ok := item.(map[string]any); ok {
						if count, ok := obj["document_count"].(float64); ok && count == 0 {
							continue
						}
					}
					filtered = append(filtered, item)
				}
				m[key] = filtered
			}
			return v
		})
	}
}

func handleBulkEditObjects(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		objectType, errRes := getRequiredString(request, "object_type")
		if errRes != nil {
			return errRes, nil
		}
		operation, errRes := getRequiredString(request, "operation")
		if errRes != nil {
			return errRes, nil
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

		// This endpoint mutates metadata objects without going through their
		// resource paths, so Post's path-based invalidation misses their cache.
		if client.cache != nil {
			if prefix := cachePrefix("/api/" + objectType + "/"); prefix != "" {
				client.cache.Invalidate(prefix)
			}
		}

		path := "/api/bulk_edit_objects/"
		resp, err := client.Post(ctx, path, body)
		return doRequest(resp, err, "POST", path)
	}
}

// bulkEditMethods are the document_bulk_edit methods: metadata changes and
// run_workflow. File operations moved to the document action tools.
var bulkEditMethods = []string{
	"set_correspondent", "set_document_type", "set_storage_path", "add_tag", "remove_tag",
	"modify_tags", "modify_custom_fields", "set_permissions", "run_workflow",
}

// bulkEditMovedMethods maps the bulk_edit file methods paperless-ngx still
// accepts (deprecated, until API v9 is dropped) to the tool that replaces them.
var bulkEditMovedMethods = map[string]string{
	"rotate":          "document_rotate",
	"merge":           "document_merge",
	"edit_pdf":        "document_edit_pdf",
	"split":           "document_edit_pdf",
	"delete_pages":    "document_edit_pdf",
	"remove_password": "document_remove_password",
	"reprocess":       "document_reprocess",
	"delete":          "document_bulk_delete",
}
