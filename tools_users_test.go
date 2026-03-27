package main

import (
	"net/http"
	"testing"
)

func TestUserList(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/users/", jsonHandler(t, 200, paginatedResponse([]map[string]any{{"id": 1, "username": "admin"}}, 1)))
	client := testClientAndServer(t, rh)
	result := callTool(t, handlePaginatedList(client, "/api/users/"), nil)
	assertNotError(t, result)
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
	result = callTool(t, handleUserCreate(client), map[string]any{"username": "test"})
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
	result := callTool(t, handleSimpleGet(client, "/api/profile/"), nil)
	assertNotError(t, result)
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
