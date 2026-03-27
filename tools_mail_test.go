package main

import (
	"net/http"
	"testing"
)

func TestMailAccountList(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/mail_accounts/", jsonHandler(t, 200, paginatedResponse([]map[string]any{{"id": 1, "name": "Gmail"}}, 1)))
	client := testClientAndServer(t, rh)
	result := callTool(t, handlePaginatedList(client, "/api/mail_accounts/"), nil)
	assertNotError(t, result)
}

func TestMailAccountGet(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/mail_accounts/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "Gmail"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleGetByID(client, "/api/mail_accounts/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestMailAccountCreate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/mail_accounts/", jsonHandler(t, 201, map[string]any{"id": 1, "name": "Gmail"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleMailAccountCreate(client), map[string]any{
		"name":        "Gmail",
		"imap_server": "imap.gmail.com",
		"username":    "user@gmail.com",
		"password":    "secret",
	})
	assertNotError(t, result)
}

func TestMailAccountCreateRequiresFields(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleMailAccountCreate(client), map[string]any{})
	assertIsError(t, result)
	result = callTool(t, handleMailAccountCreate(client), map[string]any{"name": "test"})
	assertIsError(t, result)
	result = callTool(t, handleMailAccountCreate(client), map[string]any{"name": "test", "imap_server": "imap.test.com"})
	assertIsError(t, result)
	result = callTool(t, handleMailAccountCreate(client), map[string]any{"name": "test", "imap_server": "imap.test.com", "username": "user"})
	assertIsError(t, result)
}

func TestMailAccountUpdate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/mail_accounts/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "Updated"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleMailAccountUpdate(client), map[string]any{"id": float64(1), "name": "Updated"})
	assertNotError(t, result)
}

func TestMailAccountUpdateNoFields(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleMailAccountUpdate(client), map[string]any{"id": float64(1)})
	assertIsError(t, result)
}

func TestMailAccountDelete(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("DELETE", "/api/mail_accounts/1/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDeleteByID(client, "/api/mail_accounts/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestMailAccountTest(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/mail_accounts/1/test/", jsonHandler(t, 200, map[string]any{"result": "ok"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleMailAccountTest(client), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestMailAccountProcess(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/mail_accounts/1/process/", jsonHandler(t, 200, map[string]any{"result": "ok"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleMailAccountProcess(client), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestMailRuleList(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/mail_rules/", jsonHandler(t, 200, paginatedResponse([]map[string]any{{"id": 1, "name": "Invoices"}}, 1)))
	client := testClientAndServer(t, rh)
	result := callTool(t, handlePaginatedList(client, "/api/mail_rules/"), nil)
	assertNotError(t, result)
}

func TestMailRuleGet(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/mail_rules/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "Invoices"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleGetByID(client, "/api/mail_rules/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestMailRuleCreate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/mail_rules/", jsonHandler(t, 201, map[string]any{"id": 1, "name": "Invoices"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleMailRuleCreate(client), map[string]any{
		"name":    "Invoices",
		"account": float64(1),
		"body":    `{"folder": "INBOX", "filter_subject": "invoice"}`,
	})
	assertNotError(t, result)
}

func TestMailRuleCreateRequiresFields(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleMailRuleCreate(client), map[string]any{})
	assertIsError(t, result)
	result = callTool(t, handleMailRuleCreate(client), map[string]any{"name": "test"})
	assertIsError(t, result)
}

func TestMailRuleUpdate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/mail_rules/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "Updated"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleMailRuleUpdate(client), map[string]any{
		"id":   float64(1),
		"body": `{"name": "Updated"}`,
	})
	assertNotError(t, result)
}

func TestMailRuleUpdateNoFields(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleMailRuleUpdate(client), map[string]any{"id": float64(1)})
	assertIsError(t, result)
}

func TestMailRuleDelete(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("DELETE", "/api/mail_rules/1/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	client := testClientAndServer(t, rh)
	result := callTool(t, handleDeleteByID(client, "/api/mail_rules/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestProcessedMailList(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/processed_mail/", jsonHandler(t, 200, paginatedResponse([]map[string]any{{"id": 1}}, 1)))
	client := testClientAndServer(t, rh)
	result := callTool(t, handlePaginatedList(client, "/api/processed_mail/"), nil)
	assertNotError(t, result)
}

func TestProcessedMailGet(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/processed_mail/1/", jsonHandler(t, 200, map[string]any{"id": 1, "subject": "Invoice"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleGetByID(client, "/api/processed_mail/%d/"), map[string]any{"id": float64(1)})
	assertNotError(t, result)
}

func TestProcessedMailBulkDelete(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/processed_mail/bulk_delete/", jsonHandler(t, 200, map[string]any{"result": "ok"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleProcessedMailBulkDelete(client), map[string]any{"ids": "[1, 2, 3]"})
	assertNotError(t, result)
}

func TestProcessedMailBulkDeleteRequiresIds(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleProcessedMailBulkDelete(client), map[string]any{})
	assertIsError(t, result)
}
