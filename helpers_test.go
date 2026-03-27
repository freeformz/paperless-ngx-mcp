package main

import (
	"bytes"
	"io"
	"net/http"
	"testing"
)

func TestErrResult(t *testing.T) {
	result := errResult("something went wrong")
	if !result.IsError {
		t.Fatal("expected IsError to be true")
	}
	text := resultText(t, result)
	if text != "something went wrong" {
		t.Errorf("got %q, want %q", text, "something went wrong")
	}
}

func TestJsonResult(t *testing.T) {
	result, err := jsonResult(map[string]string{"key": "value"})
	if err != nil {
		t.Fatal(err)
	}
	m := resultJSON(t, result)
	if m["key"] != "value" {
		t.Errorf("got %v, want value", m["key"])
	}
}

func TestRawJSONResult(t *testing.T) {
	result, err := rawJSONResult([]byte(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	m := resultJSON(t, result)
	if m["a"] != float64(1) {
		t.Errorf("got %v, want 1", m["a"])
	}
}

func TestRawJSONResultInvalid(t *testing.T) {
	result, err := rawJSONResult([]byte(`not json`))
	if err != nil {
		t.Fatal(err)
	}
	text := resultText(t, result)
	if text != "not json" {
		t.Errorf("got %q, want %q", text, "not json")
	}
}

func TestApiErrorResult(t *testing.T) {
	result := apiErrorResult(404, []byte(`{"detail":"Not found."}`), "GET", "/api/docs/1/")
	if !result.IsError {
		t.Fatal("expected IsError to be true")
	}
	m := resultJSON(t, result)
	if m["status_code"] != float64(404) {
		t.Errorf("status_code = %v, want 404", m["status_code"])
	}
	if m["detail"] != "Not found." {
		t.Errorf("detail = %v, want Not found.", m["detail"])
	}
}

func TestApiErrorResultPlainText(t *testing.T) {
	result := apiErrorResult(500, []byte(`server error`), "GET", "/api/status/")
	if !result.IsError {
		t.Fatal("expected IsError to be true")
	}
	m := resultJSON(t, result)
	if m["detail"] != "server error" {
		t.Errorf("detail = %v, want server error", m["detail"])
	}
}

func TestDoRequestError(t *testing.T) {
	result, err := doRequest(nil, io.ErrUnexpectedEOF, "GET", "/api/test/")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError to be true")
	}
}

func TestDoRequestHTTPError(t *testing.T) {
	resp := &http.Response{
		StatusCode: 403,
		Body:       io.NopCloser(bytes.NewBufferString(`{"detail":"Forbidden"}`)),
	}
	result, err := doRequest(resp, nil, "GET", "/api/test/")
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected IsError to be true")
	}
}

func TestDoRequestEmptyBody(t *testing.T) {
	resp := &http.Response{
		StatusCode: 204,
		Body:       io.NopCloser(bytes.NewReader(nil)),
	}
	result, err := doRequest(resp, nil, "DELETE", "/api/test/")
	if err != nil {
		t.Fatal(err)
	}
	text := resultText(t, result)
	if text != "success" {
		t.Errorf("got %q, want success", text)
	}
}

func TestDoRequestSuccess(t *testing.T) {
	resp := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewBufferString(`{"id":1}`)),
	}
	result, err := doRequest(resp, nil, "GET", "/api/test/")
	if err != nil {
		t.Fatal(err)
	}
	m := resultJSON(t, result)
	if m["id"] != float64(1) {
		t.Errorf("id = %v, want 1", m["id"])
	}
}

func TestSetJSONFieldValid(t *testing.T) {
	body := map[string]any{}
	req := makeRequest(map[string]any{"tags": "[1, 2, 3]"})
	err := setJSONField(body, req, "tags")
	if err != nil {
		t.Fatal(err)
	}
	arr, ok := body["tags"].([]any)
	if !ok {
		t.Fatal("expected array")
	}
	if len(arr) != 3 {
		t.Errorf("got %d elements, want 3", len(arr))
	}
}

func TestSetJSONFieldEmpty(t *testing.T) {
	body := map[string]any{}
	req := makeRequest(map[string]any{})
	err := setJSONField(body, req, "tags")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body["tags"]; ok {
		t.Error("expected no tags key")
	}
}

func TestSetJSONFieldInvalid(t *testing.T) {
	body := map[string]any{}
	req := makeRequest(map[string]any{"tags": "not json"})
	err := setJSONField(body, req, "tags")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
