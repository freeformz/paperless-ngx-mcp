package main

import "testing"

func TestNewServer(t *testing.T) {
	client := NewClient("http://localhost", "test-token")
	srv := NewServer(client)
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
}
