package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestSavedViewList(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/saved_views/", jsonHandler(t, 200, paginatedResponse([]map[string]any{{"id": 1, "name": "Inbox"}}, 1)))
	client := testClientAndServer(t, rh)
	result := callTool(t, handlePaginatedList(client, "/api/saved_views/"), nil)
	assertNotError(t, result)
}

func TestSavedViewGet(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/saved_views/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "Inbox"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleGetByID(client, "/api/saved_views/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestSavedViewCreate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/saved_views/", jsonHandler(t, 201, map[string]any{"id": 1, "name": "Inbox"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleSavedViewCreate(client), map[string]any{"name": "Inbox"})
	assertNotError(t, result)
}

func TestSavedViewCreateRequiresName(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleSavedViewCreate(client), map[string]any{})
	assertIsError(t, result)
}

func TestSavedViewUpdate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/saved_views/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "Updated"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleSavedViewUpdate(client), map[string]any{"id": float64(1), "name": "Updated"})
	assertNotError(t, result)
}

func TestSavedViewUpdateNoFields(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleSavedViewUpdate(client), map[string]any{"id": float64(1)})
	assertIsError(t, result)
}

func TestSavedViewDelete(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("DELETE", "/api/saved_views/1/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDeleteByID(client, "/api/saved_views/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestSavedViewCreateSendsNullOwnerAndPermissions(t *testing.T) {
	var body map[string]any
	rh := newRouteHandler(t)
	handleServerVersionProbe(t, rh, "3.2.1")
	rh.Handle("POST", "/api/saved_views/", captureJSONBody(t, &body))

	client := testClientAndServer(t, rh)
	assertNotError(t, callTool(t, handleSavedViewCreate(client), map[string]any{
		"name":            "Inbox",
		"owner":           nil,
		"set_permissions": `{"view":{"users":[],"groups":[1,2]}}`,
	}))

	if v, ok := body["owner"]; !ok || v != nil {
		t.Errorf("owner = %v (present %v), want explicit null", v, ok)
	}
	if _, ok := body["set_permissions"].(map[string]any); !ok {
		t.Errorf("set_permissions = %v, want object", body["set_permissions"])
	}
}

func TestSavedViewCreateOmitsOwnerByDefault(t *testing.T) {
	var body map[string]any
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/saved_views/", captureJSONBody(t, &body))

	client := testClientAndServer(t, rh)
	assertNotError(t, callTool(t, handleSavedViewCreate(client), map[string]any{"name": "Inbox"}))

	if _, ok := body["owner"]; ok {
		t.Error("owner must be omitted when not given")
	}
}

func TestSavedViewUpdateOwnerOnly(t *testing.T) {
	var body map[string]any
	rh := newRouteHandler(t)
	handleServerVersionProbe(t, rh, "3.2.1")
	rh.Handle("PATCH", "/api/saved_views/4/", captureJSONBody(t, &body))

	client := testClientAndServer(t, rh)
	assertNotError(t, callTool(t, handleSavedViewUpdate(client), map[string]any{"id": float64(4), "owner": nil}))

	if v, ok := body["owner"]; !ok || v != nil || len(body) != 1 {
		t.Errorf("body = %v, want {owner: null}", body)
	}
}

func TestSavedViewUpdateRejectsNonObjectPermissions(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleSavedViewUpdate(client), map[string]any{
		"id":              float64(4),
		"set_permissions": `[1]`,
	})
	assertIsError(t, result)
}

func TestSavedViewFullPerms(t *testing.T) {
	var listQuery, getQuery string
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/saved_views/", func(w http.ResponseWriter, r *http.Request) {
		listQuery = r.URL.Query().Get("full_perms")
		jsonHandler(t, 200, paginatedResponse([]any{}, 0))(w, r)
	})
	rh.Handle("GET", "/api/saved_views/1/", func(w http.ResponseWriter, r *http.Request) {
		getQuery = r.URL.Query().Get("full_perms")
		jsonHandler(t, 200, map[string]any{"id": 1})(w, r)
	})

	client := testClientAndServer(t, rh)
	assertNotError(t, callTool(t, handlePaginatedList(client, "/api/saved_views/", addFullPerms), map[string]any{"full_perms": true}))
	assertNotError(t, callTool(t, handleGetByID(client, "/api/saved_views/%d/", addFullPerms), map[string]any{"id": float64(1), "full_perms": true}))

	if listQuery != "true" || getQuery != "true" {
		t.Errorf("full_perms list=%q get=%q, want true/true", listQuery, getQuery)
	}
}

func TestSavedViewUpdateOwnerNullString(t *testing.T) {
	var body map[string]any
	rh := newRouteHandler(t)
	handleServerVersionProbe(t, rh, "3.2.1")
	rh.Handle("PATCH", "/api/saved_views/4/", captureJSONBody(t, &body))

	client := testClientAndServer(t, rh)
	assertNotError(t, callTool(t, handleSavedViewUpdate(client), map[string]any{"id": float64(4), "owner": "null"}))

	if v, ok := body["owner"]; !ok || v != nil {
		t.Errorf("owner = %v (present %v), want JSON null", v, ok)
	}
}

// handleServerVersionProbe answers the server version probe with the given
// X-Version, as Paperless-ngx does on every authenticated response.
func handleServerVersionProbe(t *testing.T, rh *routeHandler, version string) {
	rh.Handle("GET", versionProbePath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Version", version)
		jsonHandler(t, 200, map[string]any{})(w, r)
	})
}

func TestSavedViewOwnershipRefusedOnOldServer(t *testing.T) {
	for name, tc := range map[string]struct {
		create bool
		args   map[string]any
	}{
		"create owner null":      {true, map[string]any{"name": "Inbox", "owner": nil}},
		"create set_permissions": {true, map[string]any{"name": "Inbox", "set_permissions": `{"view":{"groups":[2]}}`}},
		"update owner":           {false, map[string]any{"id": float64(4), "owner": float64(3)}},
		"update owner null str":  {false, map[string]any{"id": float64(4), "owner": "null"}},
		"update set_permissions": {false, map[string]any{"id": float64(4), "set_permissions": `{"view":{"groups":[2]}}`}},
	} {
		t.Run(name, func(t *testing.T) {
			// Only the probe is routed: a POST or PATCH would fail the test.
			rh := newRouteHandler(t)
			handleServerVersionProbe(t, rh, "2.20.15")
			client := testClientAndServer(t, rh)

			handler := handleSavedViewUpdate(client)
			tool := "saved_view_update"
			if tc.create {
				handler, tool = handleSavedViewCreate(client), "saved_view_create"
			}
			result := callTool(t, handler, tc.args)
			assertIsError(t, result)
			want := tool + " with owner or set_permissions requires Paperless-ngx 3.0.0 or later; the server runs 2.20.15"
			if got := resultText(t, result); got != want {
				t.Errorf("error = %q, want %q", got, want)
			}
		})
	}
}

func TestSavedViewOtherFieldsWorkOnOldServer(t *testing.T) {
	// Without owner or set_permissions there is no version check, so no probe.
	var body map[string]any
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/saved_views/4/", captureJSONBody(t, &body))

	client := testClientAndServer(t, rh)
	assertNotError(t, callTool(t, handleSavedViewUpdate(client), map[string]any{"id": float64(4), "name": "Renamed"}))
	if body["name"] != "Renamed" {
		t.Errorf("body = %v", body)
	}
}

func TestSavedViewOwnerMismatchIsError(t *testing.T) {
	for name, tc := range map[string]struct {
		create   bool
		args     map[string]any
		returned any
		want     string
	}{
		"create null ignored": {true, map[string]any{"name": "Inbox", "owner": nil}, 3,
			"saved view 7 was created with owner 3 instead of the requested owner null"},
		"update owner ignored": {false, map[string]any{"id": float64(7), "owner": float64(5)}, 3,
			"saved view 7 was updated with owner 3 instead of the requested owner 5"},
	} {
		t.Run(name, func(t *testing.T) {
			// Version unknown (no X-Version): the response check still catches it.
			rh := newRouteHandler(t)
			rh.Handle("GET", versionProbePath, jsonHandler(t, 200, map[string]any{}))
			reply := jsonHandler(t, 200, map[string]any{"id": 7, "name": "Inbox", "owner": tc.returned})
			rh.Handle("POST", "/api/saved_views/", reply)
			rh.Handle("PATCH", "/api/saved_views/7/", reply)
			client := testClientAndServer(t, rh)

			handler := handleSavedViewUpdate(client)
			if tc.create {
				handler = handleSavedViewCreate(client)
			}
			result := callTool(t, handler, tc.args)
			assertIsError(t, result)
			if got := resultText(t, result); !strings.HasPrefix(got, tc.want) {
				t.Errorf("error = %q, want prefix %q", got, tc.want)
			}
		})
	}
}

func TestSavedViewOwnerMatchPasses(t *testing.T) {
	for name, tc := range map[string]struct {
		owner    any
		returned any
	}{
		"null":    {nil, nil},
		"user id": {float64(5), 5},
		"string":  {"5", 5},
	} {
		t.Run(name, func(t *testing.T) {
			rh := newRouteHandler(t)
			handleServerVersionProbe(t, rh, "3.2.1")
			rh.Handle("POST", "/api/saved_views/", jsonHandler(t, 201, map[string]any{"id": 7, "name": "Inbox", "owner": tc.returned}))
			client := testClientAndServer(t, rh)

			assertNotError(t, callTool(t, handleSavedViewCreate(client), map[string]any{"name": "Inbox", "owner": tc.owner}))
		})
	}
}
