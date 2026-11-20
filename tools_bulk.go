package main

import (
	"context"
	"slices"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerBulkTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("document_bulk_edit",
			append([]mcp.ToolOption{
				mcp.WithDescription("Bulk edit documents. Methods: set_correspondent, set_document_type, set_storage_path, add_tag, remove_tag, modify_tags, delete, reprocess, set_permissions, modify_custom_fields, rotate, delete_pages, split, merge, edit_pdf. The server queues a bulk task and returns its task_id at once, before anything changes; errors such as missing permissions on a document or a wrong PDF password show up only in that task. Pass wait: true to wait for the bulk task and get its outcome. For the metadata methods, the bulk task finishing means the change is done. rotate, merge, split, delete_pages, edit_pdf and remove_password then run consume_file tasks of their own, reprocess runs reprocess_document tasks, and merge or split with delete_originals also run a bulk_delete task; with wait, follow_on says how to find them."),
				withJSONArray("documents", mcp.Description("Array of document IDs"), mcp.Required()),
				mcp.WithString("method", mcp.Description("Operation to perform"), mcp.Required()),
				withJSONObject("parameters", mcp.Description("Object with method-specific parameters"), mcp.Required()),
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

		params, _ := body["parameters"].(map[string]any)
		path := "/api/documents/bulk_edit/"
		resp, err := client.Post(ctx, path, body)
		return respondWithWait(ctx, client, request, wait, timeout, resp, err, "POST", path, func(task map[string]any) map[string]any {
			return bulkEditFollowOn(method, params, task)
		})
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

// bulkEditPDFMethods are the bulk_edit methods whose work continues in
// consume_file tasks scheduled by the bulk task (paperless-ngx bulk_edit.py).
var bulkEditPDFMethods = []string{"rotate", "merge", "split", "delete_pages", "edit_pdf", "remove_password"}

// bulkEditFollowOn describes the tasks a finished (or still running) bulk task
// schedules, which the bulk task record doesn't link to. It returns nil for
// methods that complete inside the bulk task.
func bulkEditFollowOn(method string, params map[string]any, task map[string]any) map[string]any {
	var types []string
	switch {
	case slices.Contains(bulkEditPDFMethods, method):
		types = append(types, "consume_file")
	case method == "reprocess":
		types = append(types, "reprocess_document")
	default:
		return nil
	}
	if (method == "merge" || method == "split") && params["delete_originals"] == true {
		types = append(types, "bulk_delete")
	}
	created, _ := task["date_created"].(string)
	return map[string]any{
		"task_types": types,
		"note":       "The bulk task schedules these tasks separately and doesn't link to them. Find them with task_list, filtering task_type and date_created_after, then wait on each with task_wait.",
		"task_list":  map[string]any{"task_type": types, "date_created_after": created},
	}
}
