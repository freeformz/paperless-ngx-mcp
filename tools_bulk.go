package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
			mcp.WithDescription("Get aggregated metadata counts for a selection of documents. Useful for previewing bulk changes. Objects with zero matching documents are omitted."),
			mcp.WithString("documents", mcp.Description("JSON array of document IDs"), mcp.Required()),
		),
		handleDocumentSelectionData(client),
	)

	srv.AddTool(
		mcp.NewTool("bulk_edit_objects",
			mcp.WithDescription("Bulk permissions/delete for tags, correspondents, document types, or storage paths. set_permissions requires owner and/or permissions; owner, permissions, merge and parameters only apply to set_permissions, and delete refuses them. Fails as a whole (403) unless every selected object is owned by the token user or unowned."),
			mcp.WithString("object_type", mcp.Description("Object type: tags, correspondents, document_types, storage_paths"), mcp.Required()),
			mcp.WithString("objects", mcp.Description("JSON array of object IDs"), mcp.Required()),
			mcp.WithString("operation", mcp.Description("Operation: set_permissions, delete"), mcp.Required()),
			withNullableNumber("owner", mcp.Description("set_permissions: new owner user ID, null to remove the owner")),
			withJSONObject("permissions", mcp.Description(`set_permissions: JSON object, e.g. {"view":{"users":[],"groups":[2]},"change":{"users":[],"groups":[2]}}. Each users/groups list that is given replaces the current one for that action ([] clears it) unless merge is true; omitted actions and lists stay unchanged. change implies view.`)),
			mcp.WithBoolean("merge", mcp.Description("set_permissions: merge with existing permissions and only set owner on unowned objects instead of replacing (default: false). Cannot be combined with owner: null.")),
			withJSONObject("parameters", mcp.Description("Deprecated: JSON object with owner/permissions/merge; its keys are sent at the top level. Prefer the dedicated parameters.")),
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

		path := "/api/documents/bulk_edit/"
		resp, err := client.Post(ctx, path, body)
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

// bulkObjectPermissionArgs are the bulk_edit_objects arguments that only
// apply to set_permissions.
var bulkObjectPermissionArgs = []string{"owner", "permissions", "merge", "parameters"}

// setBulkObjectPermissions copies owner, permissions and merge for a
// set_permissions call into body and rejects combinations that Paperless
// would accept as a no-op.
func setBulkObjectPermissions(body map[string]any, request mcp.CallToolRequest) error {
	// Paperless reads owner, permissions and merge from the top level of the
	// body and silently ignores anything nested, so flatten the legacy
	// parameters object and let the dedicated arguments override it.
	legacy, err := parseJSONObject("parameters", request.GetArguments()["parameters"])
	if err != nil {
		return err
	}
	if legacy != nil {
		if err := setLegacyBulkObjectParameters(body, legacy); err != nil {
			return err
		}
	}
	if err := setNullableInt(body, request.GetArguments(), "owner"); err != nil {
		return err
	}
	if err := setJSONObjectField(body, request, "permissions"); err != nil {
		return err
	}
	if _, ok := request.GetArguments()["merge"]; ok {
		body["merge"] = request.GetBool("merge", false)
	}

	// Without owner or permissions Paperless performs no change yet still
	// answers OK, so reject the no-op instead of reporting success. With
	// merge, Paperless also skips a null owner.
	owner, hasOwner := body["owner"]
	_, hasPerms := body["permissions"]
	if hasOwner && owner == nil && body["merge"] == true {
		if !hasPerms {
			return errors.New("owner: null has no effect with merge: true; omit merge to remove the owner")
		}
		return errors.New("owner: null is ignored with merge: true; send it without merge in a separate call")
	}
	if !hasOwner && !hasPerms {
		return errors.New("set_permissions requires owner and/or permissions")
	}
	return nil
}

// setLegacyBulkObjectParameters copies owner, permissions and merge from the
// legacy parameters object into body. The values go through the same checks
// as the dedicated arguments, so a string "null" or "true" can't reach
// Paperless as a string and a null or non-object permissions can't pass the
// no-op guard. Unknown keys are an error.
func setLegacyBulkObjectParameters(body map[string]any, legacy map[string]any) error {
	for k, v := range legacy {
		name := "parameters." + k
		switch k {
		case "owner":
			owner, err := parseNullableInt(name, v)
			if err != nil {
				return err
			}
			body[k] = owner
		case "permissions":
			perms, err := parseJSONObject(name, v)
			if err != nil {
				return err
			}
			if perms == nil {
				return fmt.Errorf("%s must be a JSON object, got null", name)
			}
			body[k] = perms
		case "merge":
			merge, ok := v.(bool)
			if !ok {
				got, _ := json.Marshal(v)
				return fmt.Errorf("%s must be true or false, got %s", name, got)
			}
			body[k] = merge
		default:
			return fmt.Errorf("unknown key %q in parameters; allowed: owner, permissions, merge", k)
		}
	}
	return nil
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

		if operation == "set_permissions" {
			if err := setBulkObjectPermissions(body, request); err != nil {
				return errResult(err.Error()), nil
			}
		} else {
			// Paperless ignores these for any other operation, so a delete with
			// owner or permissions would run and report success without them.
			for _, k := range bulkObjectPermissionArgs {
				if _, ok := request.GetArguments()[k]; ok {
					return errResult(fmt.Sprintf("owner, permissions, merge and parameters only apply to set_permissions, not %s", operation)), nil
				}
			}
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
