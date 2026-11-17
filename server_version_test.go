package main

import (
	"net/http"
	"testing"
)

func TestServerVersionFromEarlierResponse(t *testing.T) {
	probes := 0
	rh := newRouteHandler(t)
	rh.Handle("GET", versionProbePath, func(w http.ResponseWriter, r *http.Request) {
		probes++
		jsonHandler(t, 200, map[string]any{})(w, r)
	})
	rh.Handle("GET", "/api/documents/42/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Version", "3.0.0-beta.rc1")
		jsonHandler(t, 200, map[string]any{"id": 42})(w, r)
	})

	client := testClientAndServer(t, rh)
	// A regular response records the version, so no probe is needed.
	resp, err := client.Get(t.Context(), "/api/documents/42/", nil)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()

	v, err := client.ServerVersion(t.Context())
	if err != nil {
		t.Fatalf("ServerVersion: %v", err)
	}
	if v != "3.0.0-beta.rc1" {
		t.Errorf("version = %q, want 3.0.0-beta.rc1", v)
	}
	if probes != 0 {
		t.Errorf("version probes = %d, want 0", probes)
	}
}

func TestServerVersionProbesOnce(t *testing.T) {
	probes := 0
	rh := newRouteHandler(t)
	rh.Handle("GET", versionProbePath, func(w http.ResponseWriter, r *http.Request) {
		probes++
		w.Header().Set("X-Version", "3.2.1")
		jsonHandler(t, 200, map[string]any{})(w, r)
	})

	client := testClientAndServer(t, rh)
	for range 2 {
		v, err := client.ServerVersion(t.Context())
		if err != nil {
			t.Fatalf("ServerVersion: %v", err)
		}
		if v != "3.2.1" {
			t.Errorf("version = %q, want 3.2.1", v)
		}
	}
	if probes != 1 {
		t.Errorf("version probes = %d, want 1", probes)
	}
}

func TestServerVersionPicksUpUpgrade(t *testing.T) {
	version := "2.20.15"
	rh := newRouteHandler(t)
	rh.Handle("GET", "/api/documents/42/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Version", version)
		jsonHandler(t, 200, map[string]any{"id": 42})(w, r)
	})

	client := testClientAndServer(t, rh)
	for _, want := range []string{"2.20.15", "3.0.0"} {
		version = want
		resp, err := client.Get(t.Context(), "/api/documents/42/", nil)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		resp.Body.Close()
		if v, _ := client.ServerVersion(t.Context()); v != want {
			t.Errorf("version = %q, want %q", v, want)
		}
	}
}

func TestRequireServerVersion(t *testing.T) {
	for _, tt := range []struct {
		server  string
		wantErr bool
	}{
		{"2.20.15", true},
		{"3.0.0", false},
		{"3.2.1", false},
		{"", false},    // no header: proceed, the API reports failures
		{"dev", false}, // unparsable: proceed
	} {
		t.Run(tt.server, func(t *testing.T) {
			rh := newRouteHandler(t)
			rh.Handle("GET", versionProbePath, func(w http.ResponseWriter, r *http.Request) {
				if tt.server != "" {
					w.Header().Set("X-Version", tt.server)
				}
				jsonHandler(t, 200, map[string]any{})(w, r)
			})
			client := testClientAndServer(t, rh)

			result := requireServerVersion(t.Context(), client, "3.0.0", "some_tool")
			if !tt.wantErr {
				if result != nil {
					t.Fatalf("expected nil, got error: %s", resultText(t, result))
				}
				return
			}
			if result == nil {
				t.Fatal("expected an error result")
			}
			assertIsError(t, result)
			if text := resultText(t, result); text != "some_tool requires Paperless-ngx 3.0.0 or later; the server runs 2.20.15" {
				t.Errorf("error = %q", text)
			}
		})
	}
}

func TestRequireServerVersionProceedsWhenProbeFails(t *testing.T) {
	client := NewClient("http://127.0.0.1:1", "unused")
	if result := requireServerVersion(t.Context(), client, "3.0.0", "some_tool"); result != nil {
		t.Errorf("expected nil, got %s", resultText(t, result))
	}
}

func TestVersionAtLeast(t *testing.T) {
	tests := []struct {
		version, min string
		atLeast, ok  bool
	}{
		{"3.0.0", "3.0.0", true, true},
		{"3.2.1", "3.0.0", true, true},
		{"10.0.0", "3.0.0", true, true},
		{"3.0.0-beta.rc1", "3.0.0", true, true},
		{"v3.1", "3.0.0", true, true},
		{"2.20.15", "3.0.0", false, true},
		{"2.99.99", "3.0.0", false, true},
		{"dev", "3.0.0", false, false},
		{"", "3.0.0", false, false},
		{"3.0.0.1", "3.0.0", false, false},
	}
	for _, tt := range tests {
		atLeast, ok := versionAtLeast(tt.version, tt.min)
		if atLeast != tt.atLeast || ok != tt.ok {
			t.Errorf("versionAtLeast(%q, %q) = %v, %v; want %v, %v", tt.version, tt.min, atLeast, ok, tt.atLeast, tt.ok)
		}
	}
}
