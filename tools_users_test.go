package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestUserList(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/users/", jsonHandler(t, 200, paginatedResponse([]map[string]any{{"id": 1, "username": "admin"}}, 1)))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleUserList(client), nil)
	assertNotError(t, result)
}

func TestUserListStripsInheritedPermissions(t *testing.T) {
	users := []map[string]any{{
		"id":                    1,
		"username":              "admin",
		"inherited_permissions": []string{"documents.view_document", "documents.change_document"},
	}}

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/users/", jsonHandler(t, 200, paginatedResponse(users, 1)))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleUserList(client), nil)
	assertNotError(t, result)

	m := resultJSON(t, result)
	user := m["results"].([]any)[0].(map[string]any)
	if _, ok := user["inherited_permissions"]; ok {
		t.Error("inherited_permissions should be stripped by default")
	}
	if user["username"] != "admin" {
		t.Errorf("username = %v, want admin", user["username"])
	}
}

func TestUserListIncludePermissions(t *testing.T) {
	users := []map[string]any{{
		"id":                    1,
		"username":              "admin",
		"inherited_permissions": []string{"documents.view_document"},
	}}

	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/users/", jsonHandler(t, 200, paginatedResponse(users, 1)))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleUserList(client), map[string]any{"include_permissions": true})
	assertNotError(t, result)

	m := resultJSON(t, result)
	user := m["results"].([]any)[0].(map[string]any)
	if _, ok := user["inherited_permissions"]; !ok {
		t.Error("inherited_permissions should be present with include_permissions=true")
	}
}

func TestUserGet(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/users/1/", jsonHandler(t, 200, map[string]any{"id": 1, "username": "admin"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleGetByID(client, "/api/users/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestUserCreate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/users/", jsonHandler(t, 201, map[string]any{"id": 2, "username": "newuser"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleUserCreate(client), map[string]any{"username": "newuser", "password": "secret123"})
	assertNotError(t, result)
}

func TestUserCreateRequiresFields(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleUserCreate(client), map[string]any{})
	assertIsError(t, result)
}

func TestUserUpdate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/users/1/", jsonHandler(t, 200, map[string]any{"id": 1, "username": "updated"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleUserUpdate(client), map[string]any{"id": float64(1), "username": "updated"})
	assertNotError(t, result)
}

func TestUserUpdateNoFields(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleUserUpdate(client), map[string]any{"id": float64(1)})
	assertIsError(t, result)
}

func TestUserDelete(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("DELETE", "/api/users/1/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDeleteByID(client, "/api/users/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestUserDeactivateTotp(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/users/1/deactivate_totp/", jsonHandler(t, 200, map[string]any{"result": "ok"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleUserDeactivateTotp(client), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestGroupList(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/groups/", jsonHandler(t, 200, paginatedResponse([]map[string]any{{"id": 1, "name": "editors"}}, 1)))
	client := testClientAndServer(t, rh)
	result := callTool(t, handlePaginatedList(client, "/api/groups/"), nil)
	assertNotError(t, result)
}

func TestGroupGet(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/groups/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "editors"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleGetByID(client, "/api/groups/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestGroupCreate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/groups/", jsonHandler(t, 201, map[string]any{"id": 1, "name": "viewers"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleGroupCreate(client), map[string]any{"name": "viewers"})
	assertNotError(t, result)
}

func TestGroupCreateRequiresName(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleGroupCreate(client), map[string]any{})
	assertIsError(t, result)
}

func TestGroupUpdate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/groups/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "updated"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleGroupUpdate(client), map[string]any{"id": float64(1), "name": "updated"})
	assertNotError(t, result)
}

func TestGroupUpdateNoFields(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleGroupUpdate(client), map[string]any{"id": float64(1)})
	assertIsError(t, result)
}

func TestGroupDelete(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("DELETE", "/api/groups/1/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDeleteByID(client, "/api/groups/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestProfileGet(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/profile/", jsonHandler(t, 200, map[string]any{"email": "admin@example.com"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleProfileGet(client), nil)
	assertNotError(t, result)
}

func TestProfileGetRedactsAuthToken(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/profile/", jsonHandler(t, 200, map[string]any{
		"email":      "admin@example.com",
		"auth_token": "super-secret-token",
	}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleProfileGet(client), nil)
	assertNotError(t, result)

	text := resultText(t, result)
	if strings.Contains(text, "super-secret-token") {
		t.Error("auth_token leaked into result")
	}
	m := resultJSON(t, result)
	if m["auth_token"] != "[redacted]" {
		t.Errorf("auth_token = %v, want [redacted]", m["auth_token"])
	}
}

func TestProfileUpdate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/profile/", jsonHandler(t, 200, map[string]any{"email": "new@example.com"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleProfileUpdate(client), map[string]any{"email": "new@example.com"})
	assertNotError(t, result)
}

func TestProfileUpdateNoFields(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleProfileUpdate(client), map[string]any{})
	assertIsError(t, result)
}


func TestUserCreateWithoutPassword(t *testing.T) {
	var got captured
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/users/", captureHandler(t, &got, 201, map[string]any{"id": 2}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleUserCreate(client), map[string]any{
		"username": "nopass", "user_permissions": []any{"view_document"},
	})
	assertNotError(t, result)
	if _, ok := got.body["password"]; ok {
		t.Errorf("body has password: %v", got.body)
	}
	if perms, _ := got.body["user_permissions"].([]any); len(perms) != 1 || perms[0] != "view_document" {
		t.Errorf("user_permissions = %v, want [view_document]", got.body["user_permissions"])
	}
}

func TestUserUpdateUserPermissionsAndClear(t *testing.T) {
	var got captured
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/users/1/", captureHandler(t, &got, 200, map[string]any{"id": 1}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleUserUpdate(client), map[string]any{
		"id": float64(1), "user_permissions": []any{}, "clear": []any{"first_name", "email"},
	})
	assertNotError(t, result)
	if perms, ok := got.body["user_permissions"].([]any); !ok || len(perms) != 0 {
		t.Errorf("user_permissions = %v, want []", got.body["user_permissions"])
	}
	if got.body["first_name"] != "" || got.body["email"] != "" {
		t.Errorf("body = %v, want first_name and email blank", got.body)
	}
	if _, ok := got.body["last_name"]; ok {
		t.Errorf("last_name sent though not cleared: %v", got.body)
	}
}

func TestUserUpdateClearConflict(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleUserUpdate(client), map[string]any{
		"id": float64(1), "first_name": "Ann", "clear": []any{"first_name"},
	})
	assertIsError(t, result)
}

func TestProfileUpdateClear(t *testing.T) {
	var got captured
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/profile/", captureHandler(t, &got, 200, map[string]any{}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleProfileUpdate(client), map[string]any{"clear": []any{"last_name"}})
	assertNotError(t, result)
	if v, ok := got.body["last_name"]; !ok || v != "" {
		t.Errorf("body = %v, want last_name blank", got.body)
	}
}

func TestGroupCreateDefaultsPermissions(t *testing.T) {
	var got captured
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/groups/", captureHandler(t, &got, 201, map[string]any{"id": 3}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleGroupCreate(client), map[string]any{"name": "g"})
	assertNotError(t, result)
	if perms, ok := got.body["permissions"].([]any); !ok || len(perms) != 0 {
		t.Errorf("permissions = %v, want []", got.body["permissions"])
	}
}
