package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	// chatMetadataDelimiter starts the trailer paperless-ngx appends to a
	// chat answer (paperless_ai/chat.py CHAT_METADATA_DELIMITER).
	chatMetadataDelimiter = "\n\n__PAPERLESS_CHAT_METADATA__"
	// maxChatQuestion is ChatStreamingSerializer's q max_length.
	maxChatQuestion = 4000
	aiOffMessage    = "AI is required for this feature"
	aiOffHintText   = "AI is off on the server: see ui_settings_get settings.ai_enabled; enable it with config_update ai_enabled or the PAPERLESS_AI_ENABLED setting."
)

// maxChatResponse caps a chat answer; any real answer is far smaller.
var maxChatResponse int64 = 1 << 20

func registerAITools(srv *server.MCPServer, client *Client) {
	srv.AddTool(
		mcp.NewTool("document_ai_suggestions",
			mcp.WithDescription("Get LLM suggestions for a document from paperless-ngx's AI (needs AI on the server). Returns title; tags, correspondents, document_types and storage_paths as ids of existing objects; suggested_tags, suggested_correspondents, suggested_document_types and suggested_storage_paths as names of objects that don't exist yet; and dates. Nothing is applied: set what you want with document_update or document_bulk_edit. Needs change permission on the document. It can take minutes (bounded by PAPERLESS_MCP_AI_TIMEOUT, default 300 s; the MCP host may end the call sooner). Results are cached about 50 minutes per document and user, and any save of the document clears them. For the classifier's suggestions (no LLM) use document_classifier_suggestions."),
			withNumber("id", mcp.Description("Document ID"), mcp.Required()),
		),
		handleDocumentAISuggestions(client),
	)

	srv.AddTool(
		mcp.NewTool("document_chat",
			mcp.WithDescription("Ask paperless-ngx's AI a question, answered from the documents you can view (retrieval over the server's LLM index), or about one document_id. Each call stands alone: there is no history. Returns {answer, references}: the answer may be markdown, and references lists up to 5 source documents as {id, title}. It can take minutes (bounded by PAPERLESS_MCP_AI_TIMEOUT, default 300 s; the MCP host may end the call sooner) and can't be resumed. Needs AI and an embedding backend on the server. If the answer ends with \"Sorry, something went wrong while generating a response.\" or is \"Sorry, I couldn't find any content to answer your question.\", check system_status llmindex_status and llmindex_error; task_run llm_index updates the index."),
			mcp.WithString("q", mcp.Description(fmt.Sprintf("The question, up to %d characters", maxChatQuestion)), mcp.Required()),
			withNumber("document_id", mcp.Description("Ask about this document only (a version id is mapped to its root document, which is what the index holds); omit to ask across all your documents")),
		),
		handleDocumentChat(client),
	)
}

// withAIOffHint adds a hint to the error result for upstream's "AI is off" 400.
func withAIOffHint(result *mcp.CallToolResult, status int, body []byte) *mcp.CallToolResult {
	if status == http.StatusBadRequest && bytes.Contains(body, []byte(aiOffMessage)) {
		return addErrorText(result, aiOffHintText)
	}
	return result
}

// aiRequestError describes a failed AI request, telling a timeout and a
// cancellation (by the host or at shutdown) apart from other errors.
// onTimeout is appended to a timeout's message, extra to every message.
func aiRequestError(ctx context.Context, client *Client, what string, err error, onTimeout, extra string) *mcp.CallToolResult {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return errResult(fmt.Sprintf("%s timed out after %s (PAPERLESS_MCP_AI_TIMEOUT)%s%s", what, client.AITimeout(), onTimeout, extra))
	case errors.Is(ctx.Err(), context.Canceled):
		return errResult(what + " cancelled" + extra)
	}
	return errResult(fmt.Sprintf("%s failed: %s%s", what, err, extra))
}

func handleDocumentAISuggestions(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, errRes := getRequiredInt(request, "id")
		if errRes != nil {
			return errRes, nil
		}
		ctx, cancel := context.WithTimeout(ctx, client.AITimeout())
		defer cancel()
		stop := progressTicker(ctx, request, client.AITimeout(), func() string { return "waiting for the AI backend" })
		defer stop()

		const what = "AI suggestions request"
		const retry = "; the server may still finish and cache the suggestions, so a retry can be fast"
		path := fmt.Sprintf("/api/documents/%d/ai_suggestions/", id)
		resp, err := client.GetLong(ctx, path)
		if err != nil {
			return aiRequestError(ctx, client, what, err, retry, ""), nil
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return aiRequestError(ctx, client, what, err, retry, ""), nil
		}
		if resp.StatusCode >= 400 {
			return withAIOffHint(apiErrorResult(resp.StatusCode, body, "GET", path), resp.StatusCode, body), nil
		}
		return rawJSONResult(body)
	}
}

// chatReference is one source document of a chat answer.
type chatReference struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

// splitChatResponse splits a chat body into the answer and its references.
// The trailer is the last delimiter followed by {"references": [...]}; text
// after that JSON (an error sentence upstream can append) joins the answer.
// ok is false when a trailer was there but couldn't be read.
func splitChatResponse(body string) (answer string, refs []chatReference, ok bool) {
	refs = []chatReference{}
	i := strings.LastIndex(body, chatMetadataDelimiter)
	if i < 0 {
		return body, refs, true
	}
	rest := body[i+len(chatMetadataDelimiter):]
	dec := json.NewDecoder(strings.NewReader(rest))
	var trailer struct {
		References *[]chatReference `json:"references"`
	}
	if err := dec.Decode(&trailer); err != nil || trailer.References == nil {
		return body, []chatReference{}, false
	}
	after := rest[dec.InputOffset():]
	answer = body[:i] + after
	if *trailer.References != nil {
		refs = *trailer.References
	}
	return answer, refs, true
}

// countingWriter counts the bytes written to it, for progress reports.
type countingWriter struct{ n atomic.Int64 }

func (w *countingWriter) Write(p []byte) (int, error) {
	w.n.Add(int64(len(p)))
	return len(p), nil
}

func handleDocumentChat(client *Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q, errRes := getRequiredString(request, "q")
		if errRes != nil {
			return errRes, nil
		}
		// Upstream trims q and counts characters (code points).
		q = strings.TrimSpace(q)
		if q == "" {
			return errResult("q must not be blank"), nil
		}
		if n := utf8.RuneCountInString(q); n > maxChatQuestion {
			return errResult(fmt.Sprintf("q must be at most %d characters, got %d", maxChatQuestion, n)), nil
		}
		body := map[string]any{"q": q}

		docID, given, errRes := optionalInt(request, "document_id")
		if errRes != nil {
			return errRes, nil
		}
		var note string
		if given {
			// Upstream reads 0 as no document, which would widen the
			// question to every document; refuse it instead.
			if docID < 1 {
				return errResult(fmt.Sprintf("document_id must be a positive document id, got %d", docID)), nil
			}
			// The LLM index holds root documents only; a version id would
			// find no content.
			root, errRes := getRootID(ctx, client, docID)
			if errRes != nil {
				return errRes, nil
			}
			if root != docID {
				note = fmt.Sprintf("id %d is a version; asked about its root document %d, which is what the LLM index holds.", docID, root)
			}
			body["document_id"] = root
		}

		ctx, cancel := context.WithTimeout(ctx, client.AITimeout())
		defer cancel()
		received := &countingWriter{}
		stop := progressTicker(ctx, request, client.AITimeout(), func() string {
			return fmt.Sprintf("%d bytes received", received.n.Load())
		})
		defer stop()

		const what = "chat request"
		path := "/api/documents/chat/"
		resp, err := client.PostStream(ctx, path, body)
		if err != nil {
			return aiRequestError(ctx, client, what, err, "", ""), nil
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
			return withAIOffHint(apiErrorResult(resp.StatusCode, errBody, "POST", path), resp.StatusCode, errBody), nil
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
			return errResult(fmt.Sprintf("POST %s: unexpected response (HTTP %d, Content-Type %q), not a chat stream; a proxy or login page in the way?", path, resp.StatusCode, ct)), nil
		}

		var buf bytes.Buffer
		_, err = io.Copy(io.MultiWriter(&buf, received), io.LimitReader(resp.Body, maxChatResponse+1))
		if err != nil {
			partial := ""
			if buf.Len() > 0 {
				partial = "; partial answer: " + buf.String()
			}
			return aiRequestError(ctx, client, what, fmt.Errorf("reading the answer after %d bytes: %w", buf.Len(), err), "", partial), nil
		}
		if int64(buf.Len()) > maxChatResponse {
			return errResult(fmt.Sprintf("chat answer exceeds %d bytes", maxChatResponse)), nil
		}

		answer, refs, ok := splitChatResponse(buf.String())
		result, err := jsonResultNoEscape(map[string]any{"answer": answer, "references": refs})
		if err != nil {
			return nil, err
		}
		if !ok {
			result = addText(result, "The answer's metadata trailer couldn't be read, so references is empty and the answer holds the whole response.")
		}
		if note != "" {
			result = addText(result, note)
		}
		return result, nil
	}
}

// jsonResultNoEscape is jsonResult without HTML escaping, so markdown such
// as <, > and & stays readable.
func jsonResultNoEscape(v any) (*mcp.CallToolResult, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}
	return mcp.NewToolResultText(strings.TrimSuffix(buf.String(), "\n")), nil
}
