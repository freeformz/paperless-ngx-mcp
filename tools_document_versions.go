package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// minVersionDocumentVersions is the first Paperless-ngx release with document
// file versions (POST /api/documents/{id}/update_version/, the versions field,
// and PATCH /api/documents/{id}/versions/{version_id}/). They first appeared in
// v3.0.0-beta.rc1; v2.20.15, the last 2.x release, has none of them.
const minVersionDocumentVersions = "3.0.0"

// maxVersionLabelLen mirrors the max_length of version_label in the
// Paperless-ngx DocumentVersionSerializer / DocumentVersionLabelSerializer.
const maxVersionLabelLen = 64

var positiveIntPattern = regexp.MustCompile(`^[1-9]\d*$`)

// withVersionLabel defines the version_label parameter, which accepts a string
// or null (null, "null", and blank strings mean no label).
func withVersionLabel(opts ...mcp.PropertyOption) mcp.ToolOption {
	allOpts := append([]mcp.PropertyOption{func(schema map[string]any) {
		schema["type"] = []string{"string", "null"}
	}}, opts...)
	return mcp.WithAny("version_label", allOpts...)
}

// registerDocumentVersionTools registers the document version tools. They need
// Paperless-ngx minVersionDocumentVersions (3.0.0) or later; each handler checks
// the server version first and fails with a clear error on older servers.
func registerDocumentVersionTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("document_update_version",
			mcp.WithDescription("Upload a new file version of an existing document (requires Paperless-ngx 3.0.0+). The document keeps its ID and metadata; its content and page count come from the newest version once the file is consumed (asynchronously). Every version is owned by the root document's owner, not by the uploading user. Passing a version's ID adds the version to its root document. Returns a task UUID for tracking consumption status."),
			withNumber("id", mcp.Description("Document ID (root document or any of its versions)"), mcp.Required()),
			mcp.WithString("file_path", mcp.Description("Local path to the file to upload"), mcp.Required()),
			withVersionLabel(mcp.Description("Optional label for the new version (max 64 characters); null or blank means no label")),
		),
		handleDocumentUpdateVersion(client),
	)

	srv.AddTool(
		mcp.NewTool("document_version_list",
			mcp.WithDescription("List the file versions of a document (requires Paperless-ngx 3.0.0+), newest first. Each entry has id, added, version_label, checksum, and is_root (the original upload)."),
			withNumber("id", mcp.Description("Root document ID"), mcp.Required()),
		),
		handleDocumentVersionList(client),
	)

	srv.AddTool(
		mcp.NewTool("document_version_label_update",
			mcp.WithDescription("Set or clear the label of a document version (requires Paperless-ngx 3.0.0+). Use document_version_list to find version IDs."),
			withNumber("id", mcp.Description("Document ID (root document)"), mcp.Required()),
			withNumber("version_id", mcp.Description("Version ID (from document_version_list; the root document's own ID labels the original)"), mcp.Required()),
			withVersionLabel(mcp.Description("New label (max 64 characters); null or blank clears it"), mcp.Required()),
		),
		handleDocumentVersionLabelUpdate(client),
	)
}

// getRequiredID extracts a required document or version ID. Unlike
// getRequiredInt it rejects fractions, non-numeric values, and IDs below 1
// instead of truncating them, so a version never lands on the wrong document.
func getRequiredID(request mcp.CallToolRequest, name string) (int, *mcp.CallToolResult) {
	val, ok := request.GetArguments()[name]
	if !ok || val == nil {
		return 0, errResult(name + " is required")
	}
	switch v := val.(type) {
	case float64:
		if v >= 1 && v == math.Trunc(v) && v <= 1<<53 {
			return int(v), nil
		}
	case string:
		if s := strings.TrimSpace(v); positiveIntPattern.MatchString(s) {
			if n, err := strconv.Atoi(s); err == nil {
				return n, nil
			}
		}
	}
	return 0, errResult(fmt.Sprintf("%s must be a positive integer, got %v", name, val))
}

// getVersionLabel extracts the optional version_label argument. It returns nil
// for "no label" (absent, null, "null" in any case, or blank) and the trimmed label
// otherwise. Any other type is an error rather than being silently dropped.
func getVersionLabel(request mcp.CallToolRequest) (*string, *mcp.CallToolResult) {
	switch v := request.GetArguments()["version_label"].(type) {
	case nil:
		return nil, nil
	case string:
		s := strings.TrimSpace(v)
		if s == "" || strings.EqualFold(s, "null") {
			return nil, nil
		}
		if len([]rune(s)) > maxVersionLabelLen {
			return nil, errResult(fmt.Sprintf("version_label must be at most %d characters", maxVersionLabelLen))
		}
		return &s, nil
	default:
		return nil, errResult(fmt.Sprintf("version_label must be a string or null, got %T", v))
	}
}

func handleDocumentUpdateVersion(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredID(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		filePath, errRes := getRequiredString(request, "file_path")
		if errRes != nil {
			return errRes, nil
		}
		label, errRes := getVersionLabel(request)
		if errRes != nil {
			return errRes, nil
		}

		// Validate file path: resolve to absolute, check it's a regular file
		absPath, err := filepath.Abs(filePath)
		if err != nil {
			return errResult(fmt.Sprintf("invalid file path: %s", err)), nil
		}
		filePath = absPath

		info, err := os.Lstat(filePath)
		if err != nil {
			return errResult(fmt.Sprintf("file not accessible: %s", err)), nil
		}
		if !info.Mode().IsRegular() {
			return errResult("file_path must be a regular file"), nil
		}

		if errRes := requireServerVersion(ctx, client, minVersionDocumentVersions, "document_update_version"); errRes != nil {
			return errRes, nil
		}

		fields := url.Values{}
		if label != nil {
			fields.Set("version_label", *label)
		}

		path := fmt.Sprintf("/api/documents/%d/update_version/", id)
		resp, err := client.PostMultipart(ctx, path, fields, filePath)
		return doRequest(resp, err, "POST", path)
	}
}

func handleDocumentVersionList(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredID(request, "id")
		if errRes != nil {
			return errRes, nil
		}

		if errRes := requireServerVersion(ctx, client, minVersionDocumentVersions, "document_version_list"); errRes != nil {
			return errRes, nil
		}

		// There is no dedicated list endpoint: versions are a field of the
		// document, so fetch only that field via the sparse-fieldset param.
		path := fmt.Sprintf("/api/documents/%d/", id)
		params := url.Values{"fields": {"id,versions"}}
		resp, err := client.Get(ctx, path, params)
		result, _ := doRequest(resp, err, "GET", path)
		if result.IsError {
			return result, nil
		}

		// Servers before 3.0.0 ignore the unknown field instead of failing. If the
		// version check could not tell, don't report that as "no versions".
		var doc map[string]json.RawMessage
		if text, ok := result.Content[0].(mcp.TextContent); ok && json.Unmarshal([]byte(text.Text), &doc) == nil {
			if _, ok := doc["versions"]; ok {
				return result, nil
			}
		}
		return errResult(fmt.Sprintf("the server returned no versions field for document %d; document versions require Paperless-ngx %s or later", id, minVersionDocumentVersions)), nil
	}
}

func handleDocumentVersionLabelUpdate(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredID(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		versionID, errRes := getRequiredID(request, "version_id")
		if errRes != nil {
			return errRes, nil
		}
		if _, ok := request.GetArguments()["version_label"]; !ok {
			return errResult("version_label is required (null or blank clears it)"), nil
		}
		label, errRes := getVersionLabel(request)
		if errRes != nil {
			return errRes, nil
		}

		if errRes := requireServerVersion(ctx, client, minVersionDocumentVersions, "document_version_label_update"); errRes != nil {
			return errRes, nil
		}

		// Paperless-ngx normalizes blank labels to null; send null directly.
		var body any
		if label != nil {
			body = *label
		}
		path := fmt.Sprintf("/api/documents/%d/versions/%d/", id, versionID)
		resp, err := client.Patch(ctx, path, map[string]any{"version_label": body})
		return doRequest(resp, err, "PATCH", path)
	}
}
