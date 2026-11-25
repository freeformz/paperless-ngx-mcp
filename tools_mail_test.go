package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
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
		"imap_port":   float64(993),
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

func TestMailAccountCreateIMAPSecuritySSL(t *testing.T) {
	var capturedBody map[string]any
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/mail_accounts/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		w.Write([]byte(`{"id":1}`))
	})
	client := testClientAndServer(t, rh)
	result := callTool(t, handleMailAccountCreate(client), map[string]any{
		"name":          "Gmail",
		"imap_server":   "imap.gmail.com",
		"imap_port":     float64(993),
		"imap_security": "ssl",
		"username":      "user@gmail.com",
		"password":      "secret",
	})
	assertNotError(t, result)

	if capturedBody["imap_security"] != float64(2) {
		t.Errorf("imap_security = %v, want 2", capturedBody["imap_security"])
	}
}

func TestMailAccountCreateIMAPSecurityInteger(t *testing.T) {
	var capturedBody map[string]any
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/mail_accounts/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		w.Write([]byte(`{"id":1}`))
	})
	client := testClientAndServer(t, rh)
	result := callTool(t, handleMailAccountCreate(client), map[string]any{
		"name":          "Gmail",
		"imap_server":   "imap.gmail.com",
		"imap_port":     float64(993),
		"imap_security": 3,
		"username":      "user@gmail.com",
		"password":      "secret",
	})
	assertNotError(t, result)

	if capturedBody["imap_security"] != float64(3) {
		t.Errorf("imap_security = %v, want 3", capturedBody["imap_security"])
	}
}

func TestMailAccountCreateRequiresIMAPPort(t *testing.T) {
	called := false
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/mail_accounts/", func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		w.Write([]byte(`{"id":1}`))
	})
	client := testClientAndServer(t, rh)
	result := callTool(t, handleMailAccountCreate(client), map[string]any{
		"name":        "Gmail",
		"imap_server": "imap.gmail.com",
		"username":    "user@gmail.com",
		"password":    "secret",
	})
	assertIsError(t, result)

	if called {
		t.Error("server should not be called when imap_port is missing")
	}
}

func TestMailAccountCreateRejectsInvalidIMAPSecurity(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
	}{
		{name: "unknown name", value: "tls"},
		{name: "out of range integer", value: 4},
		{name: "zero", value: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			rh := newRouteHandler(t)
			rh.Handle("POST", "/api/mail_accounts/", func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(201)
				w.Write([]byte(`{"id":1}`))
			})
			client := testClientAndServer(t, rh)
			result := callTool(t, handleMailAccountCreate(client), map[string]any{
				"name":          "Gmail",
				"imap_server":   "imap.gmail.com",
				"imap_port":     float64(993),
				"imap_security": tc.value,
				"username":      "user@gmail.com",
				"password":      "secret",
			})
			assertIsError(t, result)

			if called {
				t.Error("server should not be called when imap_security is invalid")
			}
			msg := resultText(t, result)
			for _, want := range []string{"none", "ssl", "starttls"} {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q should name accepted value %q", msg, want)
				}
			}
		})
	}
}

func TestMailAccountUpdate(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/mail_accounts/1/", jsonHandler(t, 200, map[string]any{"id": 1, "name": "Updated"}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleMailAccountUpdate(client), map[string]any{"id": float64(1), "name": "Updated"})
	assertNotError(t, result)
}

func TestMailAccountUpdateIMAPSecurity(t *testing.T) {
	var capturedBody map[string]any
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/mail_accounts/1/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"id":1}`))
	})
	client := testClientAndServer(t, rh)
	result := callTool(t, handleMailAccountUpdate(client), map[string]any{
		"id":            float64(1),
		"imap_security": "starttls",
	})
	assertNotError(t, result)

	if capturedBody["imap_security"] != float64(3) {
		t.Errorf("imap_security = %v, want 3", capturedBody["imap_security"])
	}
	for _, field := range []string{"name", "imap_server", "imap_port", "username", "password"} {
		if _, ok := capturedBody[field]; ok {
			t.Errorf("body should not contain %q when it was not supplied", field)
		}
	}
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
	var capturedBody map[string]any
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/mail_accounts/1/", jsonHandler(t, 200, map[string]any{
		"id":            1,
		"name":          "Gmail",
		"imap_server":   "imap.gmail.com",
		"imap_port":     993,
		"imap_security": 2,
		"username":      "user@gmail.com",
		"password":      "**********",
	}))
	rh.Handle("POST", "/api/mail_accounts/test/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"success":true}`))
	})
	client := testClientAndServer(t, rh)
	result := callTool(t, handleMailAccountTest(client), map[string]any{"id": float64(1)})
	assertNotError(t, result)

	if capturedBody["id"] != float64(1) {
		t.Errorf("id = %v, want 1", capturedBody["id"])
	}
	if capturedBody["imap_server"] != "imap.gmail.com" {
		t.Errorf("imap_server = %v, want imap.gmail.com", capturedBody["imap_server"])
	}
	if capturedBody["password"] != "**********" {
		t.Errorf("password = %v, want obfuscated placeholder", capturedBody["password"])
	}
}

func TestMailAccountTestAccountNotFound(t *testing.T) {
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/mail_accounts/99/", jsonHandler(t, 404, map[string]any{"detail": "Not found."}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleMailAccountTest(client), map[string]any{"id": float64(99)})
	assertIsError(t, result)
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
	var capturedBody map[string]any
	rh := newRouteHandler(t)
	rh.Handle("POST", "/api/processed_mail/bulk_delete/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"result":"OK"}`))
	})
	client := testClientAndServer(t, rh)
	result := callTool(t, handleProcessedMailBulkDelete(client), map[string]any{"ids": "[1, 2, 3]"})
	assertNotError(t, result)

	mailIDs, ok := capturedBody["mail_ids"].([]any)
	if !ok || len(mailIDs) != 3 {
		t.Errorf("mail_ids = %v, want [1 2 3]", capturedBody["mail_ids"])
	}
	if _, ok := capturedBody["ids"]; ok {
		t.Error("body should not contain 'ids' — the API field is 'mail_ids'")
	}
}

func TestProcessedMailBulkDeleteRequiresIds(t *testing.T) {
	client := NewClient("http://unused", "unused")
	result := callTool(t, handleProcessedMailBulkDelete(client), map[string]any{})
	assertIsError(t, result)
}

func TestMailAccountUpdateCharacterSet(t *testing.T) {
	var got captured
	rh := newRouteHandler(t)
	rh.Handle("PATCH", "/api/mail_accounts/1/", captureHandler(t, &got, 200, map[string]any{"id": 1}))
	client := testClientAndServer(t, rh)
	result := callTool(t, handleMailAccountUpdate(client), map[string]any{"id": float64(1), "character_set": "ISO-8859-1"})
	assertNotError(t, result)
	if got.body["character_set"] != "ISO-8859-1" {
		t.Errorf("body = %v, want character_set", got.body)
	}
}

func TestMailRuleCreateRefusesNameInBody(t *testing.T) {
	client := NewClient("http://unused", "unused")
	for _, key := range []string{"name", "account"} {
		result := callTool(t, handleMailRuleCreate(client), map[string]any{
			"name": "r", "account": float64(1), "body": map[string]any{key: "x"},
		})
		assertIsError(t, result)
		if !strings.Contains(resultText(t, result), key) {
			t.Errorf("error = %s, want it to name %s", resultText(t, result), key)
		}
	}
}
