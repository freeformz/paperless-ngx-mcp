package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestGetJSONArg(t *testing.T) {
	tests := []struct {
		name        string
		args        map[string]any
		want        any
		wantPresent bool
		wantErr     bool
	}{
		{"absent", map[string]any{}, nil, false, false},
		{"null", map[string]any{"v": nil}, nil, false, false},
		{"empty string", map[string]any{"v": ""}, nil, false, false},
		{"native array", map[string]any{"v": []any{float64(1), float64(2)}}, []any{float64(1), float64(2)}, true, false},
		{"native object", map[string]any{"v": map[string]any{"a": "b"}}, map[string]any{"a": "b"}, true, false},
		{"json array string", map[string]any{"v": "[1, 2]"}, []any{float64(1), float64(2)}, true, false},
		{"json object string", map[string]any{"v": `{"a": "b"}`}, map[string]any{"a": "b"}, true, false},
		{"json null string", map[string]any{"v": "null"}, nil, true, false},
		{"invalid json", map[string]any{"v": "[1,"}, nil, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, present, err := getJSONArg(makeRequest(tt.args), "v")
			if (err != nil) != tt.wantErr || present != tt.wantPresent || !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getJSONArg = (%v, %v, %v), want (%v, %v, err %v)", got, present, err, tt.want, tt.wantPresent, tt.wantErr)
			}
		})
	}
}

func TestSetJSONFieldNative(t *testing.T) {
	body := map[string]any{}
	if err := setJSONField(body, makeRequest(map[string]any{"tags": []any{float64(3)}}), "tags"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(body["tags"], []any{float64(3)}) {
		t.Errorf("tags = %v, want [3]", body["tags"])
	}
}

func TestGetJSONInts(t *testing.T) {
	for name, tt := range map[string]struct {
		v       any
		want    []int
		wantErr bool
	}{
		"native":       {[]any{float64(1), float64(2)}, []int{1, 2}, false},
		"string":       {"[1, 2]", []int{1, 2}, false},
		"empty":        {[]any{}, []int{}, false},
		"fraction":     {[]any{1.5}, nil, true},
		"string items": {[]any{"1"}, nil, true},
		"object":       {map[string]any{"a": float64(1)}, nil, true},
		"scalar":       {float64(5), nil, true},
	} {
		t.Run(name, func(t *testing.T) {
			got, present, err := getJSONInts(makeRequest(map[string]any{"ids": tt.v}), "ids")
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && (!present || !reflect.DeepEqual(got, tt.want)) {
				t.Errorf("got (%v, %v), want %v", got, present, tt.want)
			}
		})
	}
}

// TestStructuredParamsAcceptNativeValues checks every tool parameter that
// documents an array or object accepts it natively as well as a JSON string,
// so a schema-validating client can send either.
func TestStructuredParamsAcceptNativeValues(t *testing.T) {
	srv := NewServer(NewClient("http://unused", "unused"), nil)
	checked := 0
	for name, tool := range srv.ListTools() {
		for prop, raw := range tool.Tool.InputSchema.Properties {
			schema, _ := raw.(map[string]any)
			desc, _ := schema["description"].(string)
			var want string
			switch {
			case strings.HasPrefix(desc, "Array of"):
				want = "array"
			case strings.HasPrefix(desc, "Object with"):
				want = "object"
			default:
				continue
			}
			checked++
			types, _ := schema["type"].([]string)
			if !slices.Contains(types, want) || !slices.Contains(types, "string") {
				t.Errorf("%s.%s: type = %v, want it to include %q and \"string\"", name, prop, schema["type"], want)
			}
		}
	}
	if checked < 30 {
		t.Errorf("checked %d structured parameters, want at least 30", checked)
	}
}

// capturePOSTOrPATCH records the decoded JSON body of one request.
func captureJSONBody(t *testing.T, method, path string) (*Client, *map[string]any) {
	t.Helper()
	var got map[string]any
	rh := newRouteHandler(t)
	rh.Handle(method, path, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id": 1}`))
	})
	return testClientAndServer(t, rh), &got
}

func TestNativeArrayDocumentUpdateTags(t *testing.T) {
	client, got := captureJSONBody(t, "PATCH", "/api/documents/1/")
	assertNotError(t, callTool(t, handleDocumentUpdate(client), map[string]any{
		"id": float64(1), "tags": []any{float64(3), float64(7)}, "custom_fields": []any{map[string]any{"field": float64(2), "value": "x"}},
	}))
	if !reflect.DeepEqual((*got)["tags"], []any{float64(3), float64(7)}) {
		t.Errorf("tags = %v, want [3 7]", (*got)["tags"])
	}
	if cf, ok := (*got)["custom_fields"].([]any); !ok || len(cf) != 1 {
		t.Errorf("custom_fields = %v, want one assignment", (*got)["custom_fields"])
	}
}

func TestNativeObjectMailRuleBody(t *testing.T) {
	client, got := captureJSONBody(t, "POST", "/api/mail_rules/")
	assertNotError(t, callTool(t, handleMailRuleCreate(client), map[string]any{
		"name": "Invoices", "account": float64(1), "body": map[string]any{"folder": "INBOX"},
	}))
	if (*got)["folder"] != "INBOX" || (*got)["name"] != "Invoices" {
		t.Errorf("body = %v, want folder merged in with name", *got)
	}
}

func TestNativeObjectWorkflowBody(t *testing.T) {
	client, got := captureJSONBody(t, "POST", "/api/workflows/")
	assertNotError(t, callTool(t, handleWorkflowCreate(client), map[string]any{
		"name": "Auto-tag", "body": map[string]any{"enabled": true},
	}))
	if (*got)["enabled"] != true {
		t.Errorf("body = %v, want enabled: true", *got)
	}
}

func TestNativeArrayTaskAcknowledge(t *testing.T) {
	client, got := captureJSONBody(t, "POST", "/api/tasks/acknowledge/")
	assertNotError(t, callTool(t, handleTaskAcknowledge(client), map[string]any{"tasks": []any{float64(4)}}))
	if !reflect.DeepEqual((*got)["tasks"], []any{float64(4)}) {
		t.Errorf("tasks = %v, want [4]", (*got)["tasks"])
	}
}

func TestNativeArrayDocumentUploadTags(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "doc.pdf")
	if err := os.WriteFile(filePath, []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}
	var tags []string
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/documents/post_document/", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		tags = r.MultipartForm.Value["tags"]
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`"task-uuid"`))
	})
	assertNotError(t, callTool(t, handleDocumentUpload(testClientAndServer(t, rh)), map[string]any{
		"file_path": filePath, "tags": []any{float64(3), float64(7)},
	}))
	if !reflect.DeepEqual(tags, []string{"3", "7"}) {
		t.Errorf("tags form fields = %v, want [3 7]", tags)
	}
}

func TestNativeArrayDocumentDownloadIDs(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/download/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="a.pdf"`)
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("%PDF-1.4"))
	})
	result := callTool(t, handleDocumentDownload(testClientAndServer(t, rh), testDownloader(t, 1)), map[string]any{"ids": []any{float64(1)}})
	assertNotError(t, result)
	if results := resultJSON(t, result)["results"].([]any); len(results) != 1 {
		t.Errorf("results = %v, want one download", results)
	}
	// Items that aren't integers are rejected before any request.
	bad := callTool(t, handleDocumentDownload(testClientAndServer(t, newRouteHandler(t)), testDownloader(t, 1)), map[string]any{"ids": []any{"1"}})
	assertIsError(t, bad)
}

func TestNativeArrayCleanupDownloadsFiles(t *testing.T) {
	dl := testDownloader(t, 1)
	p := filepath.Join(dl.Dir(), "a.pdf")
	if err := os.WriteFile(p, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	dl.TrackFile(p)
	result := callTool(t, handleCleanupDownloads(dl), map[string]any{"files": []any{p}})
	assertNotError(t, result)
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("file still exists after cleanup: %v", err)
	}
}

func TestNativeArrayPageImageRegion(t *testing.T) {
	client := pdfDocumentServer(t, "1454", "application/pdf", makeTestPDF(t, 1))
	result := callTool(t, handleDocumentPageImage(client), map[string]any{
		"id": 1454, "region": []any{0.25, 0.25, 0.75, 0.75},
	})
	assertNotError(t, result)
	if summary := resultText(t, result); !strings.Contains(summary, "region [0.25,0.25,0.75,0.75]") {
		t.Errorf("summary %q missing region info", summary)
	}
	// Pixel-looking native values still get the fractions hint.
	bad := callTool(t, handleDocumentPageImage(NewClient("http://unused", "unused")), map[string]any{
		"id": 1, "region": []any{float64(0), float64(0), float64(1000), float64(700)},
	})
	assertIsError(t, bad)
	if !strings.Contains(resultText(t, bad), "fractions") {
		t.Errorf("error %q should explain fractions", resultText(t, bad))
	}
}
