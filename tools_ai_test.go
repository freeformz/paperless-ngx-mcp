package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// chatStream answers the chat endpoint with body as a text/event-stream and
// records the request body.
func chatStream(t *testing.T, rh *routeHandler, body string, got *map[string]any) {
	t.Helper()
	rh.Handle("POST", "/api/documents/chat/", func(w http.ResponseWriter, r *http.Request) {
		if got != nil {
			*got = nil
			if err := json.NewDecoder(r.Body).Decode(got); err != nil {
				t.Errorf("decode chat body: %v", err)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(body))
	})
}

func chatResult(t *testing.T, result *mcp.CallToolResult) (string, []any) {
	t.Helper()
	assertNotError(t, result)
	m := resultJSON(t, result)
	refs, ok := m["references"].([]any)
	if !ok {
		t.Fatalf("references = %#v, want an array", m["references"])
	}
	return m["answer"].(string), refs
}

func TestDocumentAISuggestions(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/7/ai_suggestions/", jsonHandler(t, 200, map[string]any{"title": "Invoice", "tags": []int{3}, "suggested_tags": []string{"Utilities"}}))
	client := testClientAndServer(t, rh)
	m := resultJSON(t, callTool(t, handleDocumentAISuggestions(client), map[string]any{"id": float64(7)}))
	if m["title"] != "Invoice" || len(m["suggested_tags"].([]any)) != 1 {
		t.Errorf("result = %v, want the response passed through", m)
	}
}

func TestAIOffHint(t *testing.T) {
	aiOff := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(aiOffMessage))
	}
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/7/ai_suggestions/", aiOff)
	rh.Handle("POST", "/api/documents/chat/", aiOff)
	client := testClientAndServer(t, rh)
	for name, result := range map[string]*mcp.CallToolResult{
		"suggestions": callTool(t, handleDocumentAISuggestions(client), map[string]any{"id": float64(7)}),
		"chat":        callTool(t, handleDocumentChat(client), map[string]any{"q": "what?"}),
	} {
		assertIsError(t, result)
		blocks := textBlocks(result)
		if len(blocks) != 2 || !strings.Contains(blocks[0], aiOffMessage) || blocks[1] != aiOffHintText {
			t.Errorf("%s: blocks = %q, want the error and the hint", name, blocks)
		}
	}
}

func TestDocumentAISuggestionsOtherErrorsPassThrough(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/7/ai_suggestions/", jsonHandler(t, 503, map[string]any{"ai": []string{"AI backend request timed out."}}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentAISuggestions(client), map[string]any{"id": float64(7)})
	assertIsError(t, result)
	if blocks := textBlocks(result); len(blocks) != 1 || !strings.Contains(blocks[0], "503") {
		t.Errorf("blocks = %q, want the 503 alone", blocks)
	}
}

func TestDocumentChat(t *testing.T) {
	var got map[string]any
	rh := newRouteHandler(t)
	chatStream(t, rh, "Paid <b>twice</b> & late.\n\n__PAPERLESS_CHAT_METADATA__{\"references\":[{\"id\":12,\"title\":\"Invoice\"}]}", &got)
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDocumentChat(client), map[string]any{"q": "  Was it paid?  "})
	answer, refs := chatResult(t, result)
	if answer != "Paid <b>twice</b> & late." {
		t.Errorf("answer = %q", answer)
	}
	if len(refs) != 1 || refs[0].(map[string]any)["id"] != float64(12) || refs[0].(map[string]any)["title"] != "Invoice" {
		t.Errorf("references = %v", refs)
	}
	if len(got) != 1 || got["q"] != "Was it paid?" {
		t.Errorf("body = %v, want q trimmed and no document_id", got)
	}
	if text := textBlocks(result)[0]; !strings.Contains(text, "<b>twice</b> & late") {
		t.Errorf("result escapes HTML: %s", text)
	}
	if n := len(result.Content); n != 1 {
		t.Errorf("content blocks = %d, want 1", n)
	}
}

func TestDocumentChatSplit(t *testing.T) {
	const d = chatMetadataDelimiter
	const errSentence = "Sorry, something went wrong while generating a response."
	tests := []struct {
		name, body, answer string
		refs               int
		note               bool
	}{
		{"no trailer", "Just text.", "Just text.", 0, false},
		{"failure sentence", "Sorry, I couldn't find any content to answer your question.", "Sorry, I couldn't find any content to answer your question.", 0, false},
		{"delimiter in the answer", "a" + d + "not json" + d + "{\"references\":[{\"id\":1,\"title\":\"x\"}]}", "a" + d + "not json", 1, false},
		{"error after the trailer", "partial" + d + "{\"references\":[]}" + errSentence, "partial" + errSentence, 0, false},
		{"bad trailer", "a" + d + "{broken", "a" + d + "{broken", 0, true},
		{"wrong shape", "a" + d + "{\"references\":\"x\"}", "a" + d + "{\"references\":\"x\"}", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rh := newRouteHandler(t)
			chatStream(t, rh, tt.body, nil)
			client := testClientAndServer(t, rh)
			result := callTool(t, handleDocumentChat(client), map[string]any{"q": "q?"})
			answer, refs := chatResult(t, result)
			if answer != tt.answer || len(refs) != tt.refs {
				t.Errorf("answer = %q, %d refs; want %q, %d", answer, len(refs), tt.answer, tt.refs)
			}
			if hasNote := len(result.Content) == 2; hasNote != tt.note {
				t.Errorf("note block = %v, want %v (%q)", hasNote, tt.note, textBlocks(result))
			}
		})
	}
}

func TestDocumentChatMapsVersionToRoot(t *testing.T) {
	var got map[string]any
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/9/root/", jsonHandler(t, 200, map[string]any{"root_id": 4}))
	rh.Handle("GET", "/api/documents/4/root/", jsonHandler(t, 200, map[string]any{"root_id": 4}))
	chatStream(t, rh, "ok", &got)
	client := testClientAndServer(t, rh)

	result := callTool(t, handleDocumentChat(client), map[string]any{"q": "q?", "document_id": float64(9)})
	chatResult(t, result)
	if got["document_id"] != float64(4) {
		t.Errorf("document_id sent = %v, want the root 4", got["document_id"])
	}
	if blocks := textBlocks(result); len(blocks) != 2 || !strings.Contains(blocks[1], "id 9 is a version") {
		t.Errorf("blocks = %q, want a mapping note", blocks)
	}

	result = callTool(t, handleDocumentChat(client), map[string]any{"q": "q?", "document_id": "4"})
	chatResult(t, result)
	if got["document_id"] != float64(4) || len(result.Content) != 1 {
		t.Errorf("root id: document_id = %v, %d blocks; want 4 and no note", got["document_id"], len(result.Content))
	}
}

func TestDocumentChatRootLookupErrorPassesThrough(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/9/root/", jsonHandler(t, 404, map[string]any{"detail": "No Document matches the given query."}))
	client := testClientAndServer(t, rh) // no chat route: chat must not be called
	assertErrorContains(t, callTool(t, handleDocumentChat(client), map[string]any{"q": "q?", "document_id": float64(9)}), "404")
}

func TestDocumentChatRefusesBadInput(t *testing.T) {
	client := testClientAndServer(t, newRouteHandler(t))
	tests := []struct {
		args map[string]any
		want string
	}{
		{map[string]any{}, "q"},
		{map[string]any{"q": "   "}, "q must not be blank"},
		{map[string]any{"q": strings.Repeat("é", maxChatQuestion+1)}, "at most 4000 characters, got 4001"},
		{map[string]any{"q": "q?", "document_id": float64(0)}, "document_id must be a positive document id"},
		{map[string]any{"q": "q?", "document_id": float64(-1)}, "document_id must be a positive document id"},
		{map[string]any{"q": "q?", "document_id": 1.5}, "document_id"},
	}
	for _, tt := range tests {
		assertErrorContains(t, callTool(t, handleDocumentChat(client), tt.args), tt.want)
	}
	// 4000 characters of a multi-byte letter is within the limit.
	rh := newRouteHandler(t)
	chatStream(t, rh, "ok", nil)
	assertNotError(t, callTool(t, handleDocumentChat(testClientAndServer(t, rh)), map[string]any{"q": strings.Repeat("é", maxChatQuestion)}))
}

func TestDocumentChatRefusesNonStream(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/documents/chat/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>login</html>"))
	})
	client := testClientAndServer(t, rh)
	assertErrorContains(t, callTool(t, handleDocumentChat(client), map[string]any{"q": "q?"}), "not a chat stream")
}

func TestDocumentChatTooLarge(t *testing.T) {
	old := maxChatResponse
	maxChatResponse = 10
	t.Cleanup(func() { maxChatResponse = old })
	rh := newRouteHandler(t)
	chatStream(t, rh, strings.Repeat("x", 11), nil)
	assertErrorContains(t, callTool(t, handleDocumentChat(testClientAndServer(t, rh)), map[string]any{"q": "q?"}), "exceeds 10 bytes")
}

func TestDocumentChatTruncatedStream(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/documents/chat/", func(w http.ResponseWriter, r *http.Request) {
		// Promise more than is sent, then drop the connection.
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(200)
		w.Write([]byte("half an answer"))
		w.(http.Flusher).Flush()
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		conn.Close()
	})
	result := callTool(t, handleDocumentChat(testClientAndServer(t, rh)), map[string]any{"q": "q?"})
	assertErrorContains(t, result, "after 14 bytes")
	assertErrorContains(t, result, "partial answer: half an answer")
}

func TestAITimeout(t *testing.T) {
	slow := func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/7/ai_suggestions/", slow)
	rh.Handle("POST", "/api/documents/chat/", slow)
	client := testClientAndServer(t, rh)
	client.SetAITimeout(50 * time.Millisecond)

	result := callTool(t, handleDocumentAISuggestions(client), map[string]any{"id": float64(7)})
	assertErrorContains(t, result, "timed out after 50ms (PAPERLESS_MCP_AI_TIMEOUT)")
	assertErrorContains(t, result, "a retry can be fast")
	assertErrorContains(t, callTool(t, handleDocumentChat(client), map[string]any{"q": "q?"}), "timed out after 50ms")

	// A cancelled call says so rather than reporting a timeout.
	client.SetAITimeout(time.Minute)
	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(50*time.Millisecond, cancel)
	res, err := handleDocumentChat(client)(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"q": "q?"}}})
	if err != nil {
		t.Fatal(err)
	}
	assertErrorContains(t, res, "chat request cancelled")
}

func TestAITimeoutDoesNotAffectOtherCalls(t *testing.T) {
	client := NewClient("http://unused", "unused")
	if client.httpClient.Timeout != 30*time.Second || client.longHTTPClient.Timeout != 0 || client.AITimeout() != defaultAITimeout {
		t.Errorf("timeouts = %s, %s, %s", client.httpClient.Timeout, client.longHTTPClient.Timeout, client.AITimeout())
	}
}

func TestAITimeoutFromEnv(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"": defaultAITimeout, "  ": defaultAITimeout, "$" + "{user_config.ai_timeout}": defaultAITimeout,
		"60": time.Minute, "300.0": 300 * time.Second, "3600": time.Hour, " 1 ": time.Second,
	} {
		got, err := aiTimeoutFromEnv(in)
		if err != nil || got != want {
			t.Errorf("aiTimeoutFromEnv(%q) = %s, %v; want %s", in, got, err, want)
		}
	}
	for _, in := range []string{"0", "-5", "abc", "1.5", "3601", "4000", "1e9"} {
		if _, err := aiTimeoutFromEnv(in); err == nil || !strings.Contains(err.Error(), "PAPERLESS_MCP_AI_TIMEOUT must be whole seconds from 1 to 3600") {
			t.Errorf("aiTimeoutFromEnv(%q) err = %v, want the range error", in, err)
		}
	}
}

func TestClassifierSuggestionsRenamed(t *testing.T) {
	srv := NewServer(NewClient("http://unused", "unused"), nil)
	tools := srv.ListTools()
	if _, ok := tools["document_suggestions"]; ok {
		t.Error("document_suggestions is still registered")
	}
	for _, name := range []string{"document_classifier_suggestions", "document_ai_suggestions", "document_chat"} {
		if _, ok := tools[name]; !ok {
			t.Errorf("%s is not registered", name)
		}
	}
}

// fakeSession is a ClientSession that collects notifications.
type fakeSession struct{ ch chan mcp.JSONRPCNotification }

func (s *fakeSession) Initialize()                                         {}
func (s *fakeSession) Initialized() bool                                   { return true }
func (s *fakeSession) NotificationChannel() chan<- mcp.JSONRPCNotification { return s.ch }
func (s *fakeSession) SessionID() string                                   { return "test" }

var _ server.ClientSession = (*fakeSession)(nil)

// callInProcess sends one message through srv's message handling, with a
// session collecting notifications.
func callInProcess(t *testing.T, srv *server.MCPServer, sess *fakeSession, msg string) mcp.JSONRPCMessage {
	t.Helper()
	ctx := srv.WithContext(t.Context(), sess)
	return srv.HandleMessage(ctx, json.RawMessage(msg))
}

func TestDocumentChatNullQuestionIsText(t *testing.T) {
	var got map[string]any
	rh := newRouteHandler(t)
	chatStream(t, rh, "ok", &got)
	srv := NewServer(testClientAndServer(t, rh), nil)
	sess := &fakeSession{ch: make(chan mcp.JSONRPCNotification, 10)}
	callInProcess(t, srv, sess, "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"document_chat\",\"arguments\":{\"q\":\"null\"}}}")
	if got["q"] != "null" {
		t.Errorf("q sent = %v, want the text \"null\"", got["q"])
	}
}

func TestAIProgressNotifications(t *testing.T) {
	old := aiProgressInterval
	aiProgressInterval = 10 * time.Millisecond
	t.Cleanup(func() { aiProgressInterval = old })

	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/documents/chat/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.Write([]byte("thinking"))
		w.(http.Flusher).Flush()
		time.Sleep(100 * time.Millisecond)
		w.Write([]byte(" done"))
	})
	srv := NewServer(testClientAndServer(t, rh), nil)
	sess := &fakeSession{ch: make(chan mcp.JSONRPCNotification, 100)}
	resp := callInProcess(t, srv, sess, "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"document_chat\",\"arguments\":{\"q\":\"q?\"},\"_meta\":{\"progressToken\":\"tok\"}}}")
	if _, ok := resp.(mcp.JSONRPCResponse); !ok {
		t.Fatalf("response = %#v", resp)
	}
	n := len(sess.ch)
	if n == 0 {
		t.Fatal("no progress notifications")
	}
	first := <-sess.ch
	if first.Method != "notifications/progress" || first.Params.AdditionalFields["progressToken"] != "tok" {
		t.Errorf("notification = %+v", first)
	}
	if m, _ := first.Params.AdditionalFields["message"].(string); !strings.HasSuffix(m, "bytes received") {
		t.Errorf("message = %q", m)
	}
	time.Sleep(50 * time.Millisecond)
	if after := len(sess.ch); after != n-1 {
		t.Errorf("%d notifications arrived after the response", after-(n-1))
	}
}
