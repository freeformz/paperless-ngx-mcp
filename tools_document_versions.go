package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// paperless-ngx 3 keeps several file versions per document. The root
// document holds the metadata; each version has its own file, content and
// page count. A version id isn't a document id: /api/documents/{version}/
// is a 404, while download, thumb, preview, metadata, notes, root and the
// versions endpoints accept one.

// maxVersionLabel is DocumentVersionSerializer's max_length, counted after
// trimming, in characters.
const maxVersionLabel = 64

const versionIDNote = "A version id (from document_version_list or document_get's versions) also works as id."

// withVersion declares the version parameter of the tools that read or
// edit one version of a document.
func withVersion(desc string) mcp.ToolOption {
	return withNumber("version", mcp.Description(desc))
}

// optionalVersion reads version: 0 when not given, else a positive id.
func optionalVersion(request mcp.CallToolRequest) (int, *mcp.CallToolResult) {
	v, given, errRes := optionalInt(request, "version")
	if errRes != nil {
		return 0, errRes
	}
	if given && v < 1 {
		return 0, errResult("version must be a version id (a positive whole number)")
	}
	return v, nil
}

// versionParams returns ?version=N, or nil for 0.
func versionParams(version int) url.Values {
	if version == 0 {
		return nil
	}
	return url.Values{"version": {strconv.Itoa(version)}}
}

// versionLabel reads an optional version_label: trimmed, blank means none,
// and at most maxVersionLabel characters.
func versionLabel(request mcp.CallToolRequest) (string, *mcp.CallToolResult) {
	label := strings.TrimSpace(request.GetString("version_label", ""))
	if n := utf8.RuneCountInString(label); n > maxVersionLabel {
		return "", errResult(fmt.Sprintf("version_label must be at most %d characters, got %d", maxVersionLabel, n))
	}
	return label, nil
}

// getRootID asks paperless-ngx for the root of a document or version. A
// failed lookup returns its API error unchanged.
func getRootID(ctx context.Context, client *Client, id int) (int, *mcp.CallToolResult) {
	path := fmt.Sprintf("/api/documents/%d/root/", id)
	resp, err := client.Get(ctx, path, nil)
	body, errRes := doRequestBody(resp, err, "GET", path)
	if errRes != nil {
		return 0, errRes
	}
	var out struct {
		RootID *int `json:"root_id"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.RootID == nil {
		return 0, errResult(fmt.Sprintf("GET %s: unexpected response %s", path, body))
	}
	return *out.RootID, nil
}

// handleGetByIDWithVersion returns a handler that GETs pathFmt (one %d for the
// id), passing version through as ?version=.
func handleGetByIDWithVersion(client *Client, pathFmt string) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		version, errRes := optionalVersion(request)
		if errRes != nil {
			return errRes, nil
		}
		path := fmt.Sprintf(pathFmt, id)
		resp, err := client.Get(ctx, path, versionParams(version))
		return doRequest(resp, err, "GET", path)
	}
}

func registerDocumentVersionTools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("document_version_upload",
			append([]mcp.ToolOption{
				mcp.WithDescription("Add a file as a new version of a document. The version becomes the latest: document_get's content and page_count, downloads and page images then use it, while the metadata stays on the root document. The file goes through the normal consume; the call returns the consume task's UUID at once, and with wait: true the outcome (finished means the version exists). Needs the global change-document permission and change permission on the root."),
				withNumber("id", mcp.Description("Root document ID, or one of its version ids"), mcp.Required()),
				mcp.WithString("file_path", mcp.Description("Local path to the file to upload"), mcp.Required()),
				mcp.WithString("version_label", mcp.Description(fmt.Sprintf("Label for the new version, at most %d characters", maxVersionLabel))),
			}, waitToolOptions()...)...,
		),
		handleDocumentVersionUpload(client),
	)

	srv.AddTool(
		mcp.NewTool("document_version_list",
			mcp.WithDescription("List a document's versions, newest first and including the root (the original file): {root_id, versions: [{id, added, version_label, checksum, is_root}]}. Takes the root id or a version id. Trashed versions aren't listed, even when you pass a trashed version's id. A 404 means the document doesn't exist, isn't visible to you, or is in the trash."),
			withNumber("id", mcp.Description("Root document ID, or one of its version ids"), mcp.Required()),
		),
		handleDocumentVersionList(client),
	)

	srv.AddTool(
		mcp.NewTool("document_version_label",
			mcp.WithDescription("Set or clear a version's label (the root version can be labelled too). Returns the version entry. Needs change permission on the root. A trashed version is a 404."),
			withNumber("id", mcp.Description("Root document ID, or one of its version ids"), mcp.Required()),
			withNumber("version_id", mcp.Description("The version to label"), mcp.Required()),
			mcp.WithString("version_label", mcp.Description(fmt.Sprintf("New label, at most %d characters", maxVersionLabel))),
			withClear("version_label"),
		),
		handleDocumentVersionLabel(client),
	)

	srv.AddTool(
		mcp.NewTool("document_version_delete",
			mcp.WithDescription("Move one version of a document to the trash; trash_action restore brings it back until the trash is emptied. The root version (the original) can't be deleted this way (a 400): delete the whole document with document_delete. Returns {result, current_version_id}, the latest version left. Needs delete permission on the root. A trashed version is a 404."),
			withNumber("id", mcp.Description("Root document ID, or one of its version ids"), mcp.Required()),
			withNumber("version_id", mcp.Description("The version to delete"), mcp.Required()),
		),
		handleDocumentVersionDelete(client),
	)

	srv.AddTool(
		mcp.NewTool("document_root",
			mcp.WithDescription("Find the root document of a version id: {root_id}. A root id returns itself. Works for trashed documents too. Needs view permission on the root."),
			withNumber("id", mcp.Description("Document or version ID"), mcp.Required()),
		),
		handleGetByID(client, "/api/documents/%d/root/"),
	)
}

func handleDocumentVersionUpload(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		filePath, errRes := getRequiredString(request, "file_path")
		if errRes != nil {
			return errRes, nil
		}
		label, errRes := versionLabel(request)
		if errRes != nil {
			return errRes, nil
		}
		wait, timeout, errRes := getWaitOptions(request)
		if errRes != nil {
			return errRes, nil
		}
		filePath, errRes = validateUploadPath(filePath)
		if errRes != nil {
			return errRes, nil
		}
		fields := url.Values{}
		if label != "" {
			fields.Set("version_label", label)
		}
		path := fmt.Sprintf("/api/documents/%d/update_version/", id)
		resp, err := client.PostMultipart(ctx, path, fields, filePath)
		return respondWithWait(ctx, client, request, wait, timeout, resp, err, "POST", path, nil)
	}
}

func handleDocumentVersionList(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		body, status, errRes := getVersions(ctx, client, id)
		if status == http.StatusNotFound {
			// A version id: list its root's versions. A failed lookup, or a
			// root that is its own answer (a trashed document), keeps the 404.
			root, rootErr := getRootID(ctx, client, id)
			if rootErr != nil || root == id {
				return errRes, nil
			}
			body, _, errRes = getVersions(ctx, client, root)
		}
		if errRes != nil {
			return errRes, nil
		}
		var doc struct {
			ID       int             `json:"id"`
			Versions json.RawMessage `json:"versions"`
		}
		if err := json.Unmarshal(body, &doc); err != nil || len(doc.Versions) == 0 || string(doc.Versions) == "null" {
			return errResult("the document response has no versions field"), nil
		}
		return jsonResult(map[string]any{"root_id": doc.ID, "versions": doc.Versions})
	}
}

// getVersions fetches a document's id and versions. status is the HTTP
// status of a failed request (0 otherwise).
func getVersions(ctx context.Context, client *Client, id int) ([]byte, int, *mcp.CallToolResult) {
	path := fmt.Sprintf("/api/documents/%d/", id)
	resp, err := client.Get(ctx, path, url.Values{"fields": {"id,versions"}})
	status := 0
	if err == nil && resp.StatusCode >= 400 {
		status = resp.StatusCode
	}
	body, errRes := doRequestBody(resp, err, "GET", path)
	return body, status, errRes
}

func handleDocumentVersionLabel(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		versionID, errRes := getRequiredInt(request, "version_id")
		if errRes != nil {
			return errRes, nil
		}
		label, errRes := versionLabel(request)
		if errRes != nil {
			return errRes, nil
		}
		body := map[string]any{}
		if label != "" {
			body["version_label"] = label
		}
		if errRes := applyClear(body, request, []string{"version_label"}); errRes != nil {
			return errRes, nil
		}
		if len(body) == 0 {
			return errResult("no fields to update; give version_label, or clear: [\"version_label\"] to remove it"), nil
		}
		path := fmt.Sprintf("/api/documents/%d/versions/%d/", id, versionID)
		resp, err := client.Patch(ctx, path, body)
		return doRequest(resp, err, "PATCH", path)
	}
}

func handleDocumentVersionDelete(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		versionID, errRes := getRequiredInt(request, "version_id")
		if errRes != nil {
			return errRes, nil
		}
		path := fmt.Sprintf("/api/documents/%d/versions/%d/", id, versionID)
		resp, err := client.Delete(ctx, path, nil)
		return doRequest(resp, err, "DELETE", path)
	}
}

// --- Notes: a document's notes live on its root ---

// notesTarget maps id to the document whose notes paperless-ngx shows: the
// root. mapped is true when id was a version.
func notesTarget(ctx context.Context, client *Client, id int) (root int, mapped bool, errRes *mcp.CallToolResult) {
	root, errRes = getRootID(ctx, client, id)
	if errRes != nil {
		return 0, false, errRes
	}
	return root, root != id, nil
}

func mappedNote(id, root int) string {
	return fmt.Sprintf("id %d is a version; used its root document %d, where paperless-ngx keeps the document's notes.", id, root)
}

// addText appends a text block to a successful result.
func addText(result *mcp.CallToolResult, text string) *mcp.CallToolResult {
	if result != nil && !result.IsError {
		result.Content = append(result.Content, mcp.NewTextContent(text))
	}
	return result
}

func handleDocumentNoteList(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		root, mapped, errRes := notesTarget(ctx, client, id)
		if errRes != nil {
			return errRes, nil
		}
		path := fmt.Sprintf("/api/documents/%d/notes/", root)
		resp, err := client.Get(ctx, path, nil)
		result, err := doRequest(resp, err, "GET", path)
		if err != nil || !mapped {
			return result, err
		}
		text := mappedNote(id, root)
		// Notes stored on the version itself (added through its id, or kept
		// from a document merged as a version) don't show anywhere else.
		vpath := fmt.Sprintf("/api/documents/%d/notes/", id)
		vresp, verr := client.Get(ctx, vpath, nil)
		if own, errRes := doRequestBody(vresp, verr, "GET", vpath); errRes == nil {
			var notes []any
			if json.Unmarshal(own, &notes) == nil && len(notes) > 0 {
				text += fmt.Sprintf(" Version %d also has %d note(s) of its own, not shown in the web UI or search: %s", id, len(notes), strings.TrimSpace(string(own)))
			}
		}
		return addText(result, text), nil
	}
}

func handleDocumentNoteAdd(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		note, errRes := getRequiredString(request, "note")
		if errRes != nil {
			return errRes, nil
		}
		root, mapped, errRes := notesTarget(ctx, client, id)
		if errRes != nil {
			return errRes, nil
		}
		path := fmt.Sprintf("/api/documents/%d/notes/", root)
		resp, err := client.Post(ctx, path, map[string]any{"note": note})
		result, err := doRequest(resp, err, "POST", path)
		if err != nil || !mapped {
			return result, err
		}
		return addText(result, mappedNote(id, root)), nil
	}
}

func handleDocumentNoteDelete(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		noteID, errRes := getRequiredInt(request, "note_id")
		if errRes != nil {
			return errRes, nil
		}
		root, mapped, errRes := notesTarget(ctx, client, id)
		if errRes != nil {
			return errRes, nil
		}
		params := url.Values{"id": {strconv.Itoa(noteID)}}
		path := fmt.Sprintf("/api/documents/%d/notes/", root)
		resp, err := client.Delete(ctx, path, params)
		if mapped && err == nil && resp.StatusCode == http.StatusNotFound {
			// The note may be stored on the version itself.
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			vpath := fmt.Sprintf("/api/documents/%d/notes/", id)
			vresp, verr := client.Delete(ctx, vpath, params)
			result, err := doRequest(vresp, verr, "DELETE", vpath)
			if err != nil {
				return result, err
			}
			return addText(result, fmt.Sprintf("Note %d was stored on version %d itself, not on its root document %d.", noteID, id, root)), nil
		}
		result, err := doRequest(resp, err, "DELETE", path)
		if err != nil || !mapped {
			return result, err
		}
		return addText(result, mappedNote(id, root)), nil
	}
}
