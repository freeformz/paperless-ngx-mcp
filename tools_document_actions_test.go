package main

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// actionServer captures the JSON body POSTed to path and answers with a task
// id; task lookups step through statuses.
func actionServer(t *testing.T, path string, statuses ...string) (*Client, *map[string]any, *taskSequence) {
	t.Helper()
	var got map[string]any
	seq := &taskSequence{statuses: append([]string{}, statuses...)}
	if len(seq.statuses) == 0 {
		seq.statuses = []string{"success"}
	}
	rh := newRouteHandler(t)
	rh.Handle("POST", path, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"result": "OK", "task_id": testTaskUUID})
	})
	rh.Handle("GET", "/api/tasks/", seq.handle)
	return testClientAndServer(t, rh), &got, seq
}

func TestDocumentActionBodies(t *testing.T) {
	tests := []struct {
		name string
		op   string
		args map[string]any
		want map[string]any
	}{
		{"rotate", "rotate",
			map[string]any{"documents": []any{float64(1), float64(2)}, "degrees": float64(-90), "source_mode": "explicit_selection"},
			map[string]any{"documents": []any{float64(1), float64(2)}, "degrees": float64(-90), "source_mode": "explicit_selection"}},
		{"rotate json string documents", "rotate",
			map[string]any{"documents": "[3]", "degrees": "180"},
			map[string]any{"documents": []any{float64(3)}, "degrees": float64(180)}},
		{"merge", "merge",
			map[string]any{"documents": []any{float64(4), float64(5)}, "metadata_document_id": float64(5), "delete_originals": true, "archive_fallback": false},
			map[string]any{"documents": []any{float64(4), float64(5)}, "metadata_document_id": float64(5), "delete_originals": true, "archive_fallback": false}},
		{"merge null metadata", "merge",
			map[string]any{"documents": []any{float64(4), float64(5)}, "metadata_document_id": nil},
			map[string]any{"documents": []any{float64(4), float64(5)}, "metadata_document_id": nil}},
		{"edit_pdf", "edit_pdf",
			map[string]any{"documents": []any{float64(7)}, "operations": []any{
				map[string]any{"page": float64(1)}, map[string]any{"page": float64(3), "rotate": float64(90), "doc": float64(1)},
			}, "include_metadata": false},
			map[string]any{"documents": []any{float64(7)}, "operations": []any{
				map[string]any{"page": float64(1)}, map[string]any{"page": float64(3), "rotate": float64(90), "doc": float64(1)},
			}, "include_metadata": false}},
		{"remove_password", "remove_password",
			map[string]any{"documents": []any{float64(8)}, "password": "s3cret", "update_document": true},
			map[string]any{"documents": []any{float64(8)}, "password": "s3cret", "update_document": true}},
		{"reprocess", "reprocess",
			map[string]any{"documents": []any{float64(9)}, "remote_ocr": true},
			map[string]any{"documents": []any{float64(9)}, "remote_ocr": true}},
		{"delete", "delete",
			map[string]any{"documents": []any{float64(10), float64(11)}},
			map[string]any{"documents": []any{float64(10), float64(11)}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, got, _ := actionServer(t, documentActionPaths[tt.op])
			result := callTool(t, documentActionHandlerFor(client, tt.op), tt.args)
			assertNotError(t, result)
			if !reflect.DeepEqual(*got, tt.want) {
				t.Errorf("body = %v, want %v", *got, tt.want)
			}
			if _, ok := (*got)["from_webui"]; ok {
				t.Error("from_webui must not be sent")
			}
			if resultJSON(t, result)["task_id"] != testTaskUUID {
				t.Errorf("result = %s, want the task id", resultText(t, result))
			}
		})
	}
}

// documentActionHandlerFor builds the handler for a document action, as
// registerDocumentActionTools does.
func documentActionHandlerFor(client *Client, op string) server.ToolHandlerFunc {
	builders := map[string]func(mcp.CallToolRequest, []int) (map[string]any, *mcp.CallToolResult){
		"rotate": buildRotateBody, "merge": buildMergeBody, "edit_pdf": buildEditPDFBody,
		"remove_password": buildRemovePasswordBody, "reprocess": buildReprocessBody, "delete": buildDeleteBody,
	}
	return handleDocumentAction(client, op, builders[op])
}

func TestDocumentActionValidation(t *testing.T) {
	op := func(m map[string]any) []any { return []any{m} }
	tests := []struct {
		op   string
		args map[string]any
		want string
	}{
		{"rotate", map[string]any{"degrees": float64(90)}, "documents is required"},
		{"rotate", map[string]any{"documents": []any{}, "degrees": float64(90)}, "documents is required"},
		{"rotate", map[string]any{"documents": []any{float64(1), float64(1)}, "degrees": float64(90)}, "twice"},
		{"rotate", map[string]any{"documents": []any{float64(1)}}, "degrees is required"},
		{"rotate", map[string]any{"documents": []any{float64(1)}, "degrees": float64(45)}, "multiple of 90"},
		{"rotate", map[string]any{"documents": []any{float64(1)}, "degrees": float64(90), "source_mode": "newest"}, "source_mode"},
		{"merge", map[string]any{"documents": []any{float64(1)}}, "at least 2"},
		{"merge", map[string]any{"documents": []any{float64(1), float64(2)}, "metadata_document_id": float64(3)}, "one of documents"},
		{"edit_pdf", map[string]any{"documents": []any{float64(1), float64(2)}, "operations": op(map[string]any{"page": float64(1)})}, "exactly one"},
		{"edit_pdf", map[string]any{"documents": []any{float64(1)}}, "operations is required"},
		{"edit_pdf", map[string]any{"documents": []any{float64(1)}, "operations": []any{}}, "operations is required"},
		{"edit_pdf", map[string]any{"documents": []any{float64(1)}, "operations": op(map[string]any{"page": float64(0)})}, "page must be 1"},
		{"edit_pdf", map[string]any{"documents": []any{float64(1)}, "operations": op(map[string]any{"rotate": float64(90)})}, "page is required"},
		{"edit_pdf", map[string]any{"documents": []any{float64(1)}, "operations": op(map[string]any{"page": float64(1), "rotate": float64(45)})}, "multiple of 90"},
		{"edit_pdf", map[string]any{"documents": []any{float64(1)}, "operations": op(map[string]any{"page": float64(1), "rotation": float64(90)})}, "unknown key"},
		{"edit_pdf", map[string]any{"documents": []any{float64(1)}, "operations": op(map[string]any{"page": 1.5})}, "whole number"},
		{"edit_pdf", map[string]any{"documents": []any{float64(1)}, "operations": op(map[string]any{"page": float64(1), "doc": float64(-1)})}, "doc must be 0"},
		{"edit_pdf", map[string]any{"documents": []any{float64(1)}, "operations": []any{
			map[string]any{"page": float64(1)}, map[string]any{"page": float64(2), "doc": float64(2)},
		}}, "without gaps"},
		{"edit_pdf", map[string]any{"documents": []any{float64(1)}, "update_document": true, "operations": []any{
			map[string]any{"page": float64(1)}, map[string]any{"page": float64(2), "doc": float64(1)},
		}}, "doc 0"},
		{"edit_pdf", map[string]any{"documents": []any{float64(1)}, "operations": []any{float64(1)}}, "must be an object"},
		{"remove_password", map[string]any{"documents": []any{float64(1)}}, "password is required"},
		{"delete", map[string]any{"documents": []any{"1"}}, "array of integers"},
		{"reprocess", map[string]any{"documents": []any{float64(1)}, "timeout": float64(10)}, "only applies with wait"},
	}
	for _, tt := range tests {
		t.Run(tt.op+" "+tt.want, func(t *testing.T) {
			// No routes registered: any request the handler makes fails the test.
			client := testClientAndServer(t, newRouteHandler(t))
			result := callTool(t, documentActionHandlerFor(client, tt.op), tt.args)
			assertIsError(t, result)
			if !strings.Contains(resultText(t, result), tt.want) {
				t.Errorf("error = %s, want it to mention %q", resultText(t, result), tt.want)
			}
		})
	}
}

func TestDocumentActionWaitFollowOn(t *testing.T) {
	fastPolling(t)
	tests := []struct {
		op   string
		args map[string]any
		want []any // nil: no follow_on
	}{
		{"rotate", map[string]any{"documents": []any{float64(1)}, "degrees": float64(90)}, []any{"consume_file"}},
		{"merge", map[string]any{"documents": []any{float64(1), float64(2)}}, []any{"consume_file"}},
		{"merge", map[string]any{"documents": []any{float64(1), float64(2)}, "delete_originals": true}, []any{"consume_file", "bulk_delete"}},
		{"edit_pdf", map[string]any{"documents": []any{float64(1)}, "operations": []any{map[string]any{"page": float64(1)}}, "delete_original": true}, []any{"consume_file", "bulk_delete"}},
		{"edit_pdf", map[string]any{"documents": []any{float64(1)}, "operations": []any{map[string]any{"page": float64(1)}}, "delete_original": true, "update_document": true}, []any{"consume_file"}},
		{"remove_password", map[string]any{"documents": []any{float64(1)}, "password": "x", "delete_original": true}, []any{"consume_file", "bulk_delete"}},
		{"reprocess", map[string]any{"documents": []any{float64(1)}}, []any{"reprocess_document"}},
		{"delete", map[string]any{"documents": []any{float64(1)}}, nil},
	}
	for _, tt := range tests {
		b, _ := json.Marshal(tt.args)
		t.Run(tt.op+string(b), func(t *testing.T) {
			client, _, _ := actionServer(t, documentActionPaths[tt.op], "pending", "success")
			args := map[string]any{"wait": true}
			for k, v := range tt.args {
				args[k] = v
			}
			result := callTool(t, documentActionHandlerFor(client, tt.op), args)
			assertNotError(t, result)
			m := resultJSON(t, result)
			if m["response"].(map[string]any)["task_id"] != testTaskUUID || m["task"].(map[string]any)["status"] != "success" {
				t.Fatalf("result = %v", m)
			}
			fo, has := m["follow_on"].(map[string]any)
			if tt.want == nil {
				if has {
					t.Errorf("follow_on = %v, want none", fo)
				}
				return
			}
			if !has || !equalAny(fo["task_types"], tt.want) {
				t.Fatalf("follow_on = %v, want task_types %v", m["follow_on"], tt.want)
			}
			if fo["task_list"].(map[string]any)["date_created_after"] != "2026-11-19T10:00:00-08:00" {
				t.Errorf("follow_on should point at the bulk task's date_created: %v", fo)
			}
		})
	}
}

func TestDocumentActionWaitTimeoutKeepsFollowOn(t *testing.T) {
	fastPolling(t)
	client, _, _ := actionServer(t, documentActionPaths["rotate"], "pending")
	result := callTool(t, documentActionHandlerFor(client, "rotate"), map[string]any{
		"documents": []any{float64(1)}, "degrees": float64(90), "wait": true, "timeout": float64(1),
	})
	assertNotError(t, result)
	if m := resultJSON(t, result); m["timed_out"] != true || m["follow_on"] == nil {
		t.Errorf("result = %v, want timed_out with follow_on", m)
	}
}

func TestDocumentActionToolsRegistered(t *testing.T) {
	srv := NewServer(NewClient("http://unused", "unused"), nil)
	for _, name := range []string{"document_rotate", "document_merge", "document_edit_pdf", "document_remove_password", "document_reprocess", "document_bulk_delete"} {
		tool := srv.GetTool(name)
		if tool == nil {
			t.Fatalf("%s not registered", name)
		}
		for _, p := range []string{"documents", "wait", "timeout"} {
			if _, ok := tool.Tool.InputSchema.Properties[p]; !ok {
				t.Errorf("%s has no %s parameter", name, p)
			}
		}
	}
}
