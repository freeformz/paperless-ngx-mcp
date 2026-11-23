package main

import (
	"net/http"
	"reflect"
	"testing"
)

// Padded "" and null leave the version parameters out.
func TestVersionParamsPadding(t *testing.T) {
	var got []string
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/1/", func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.RawQuery)
		statusRoute(200, `{"id": 1}`)(w, r)
	})
	rh.Handle("GET", "/api/documents/1/metadata/", func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.RawQuery)
		statusRoute(200, `{}`)(w, r)
	})
	client := testClientAndServer(t, rh)
	for _, pad := range []any{nil, "", "null"} {
		assertNotError(t, callThroughMiddleware(t, client, "document_get", map[string]any{"id": float64(1), "version": pad}))
		assertNotError(t, callThroughMiddleware(t, client, "document_metadata", map[string]any{"id": float64(1), "version": pad}))
	}
	for _, q := range got {
		if q != "" {
			t.Errorf("query = %q, want none", q)
		}
	}

	// document_update: a padded version with a title is an ordinary title update.
	uclient, body := captureJSONBody(t, "PATCH", "/api/documents/1/")
	assertNotError(t, callThroughMiddleware(t, uclient, "document_update", map[string]any{"id": float64(1), "title": "T", "version": ""}))
	if !reflect.DeepEqual(*body, map[string]any{"title": "T"}) {
		t.Errorf("update body = %v", *body)
	}

	// document_merge_as_versions: a padded version_label isn't sent.
	mclient, mbody, _ := actionServer(t, documentActionPaths["merge_as_versions"])
	assertNotError(t, callThroughMiddleware(t, mclient, "document_merge_as_versions", map[string]any{
		"documents": []any{float64(1), float64(2), float64(3)}, "root_document_id": float64(1), "version_label": nil,
	}))
	if _, ok := (*mbody)["version_label"]; ok {
		t.Errorf("merge body = %v, want no version_label", *mbody)
	}
}
