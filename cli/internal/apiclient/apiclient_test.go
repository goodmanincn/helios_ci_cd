package apiclient

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_AuthHeader(t *testing.T) {
	var gotAuth, gotOrg, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotOrg = r.Header.Get("X-Org-ID")
		gotCT = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	cli := New(srv.URL, "tok-abc", 42)
	var out struct {
		OK bool `json:"ok"`
	}
	if err := cli.Do("POST", "/x", map[string]string{"a": "b"}, &out); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer tok-abc" {
		t.Errorf("auth: want 'Bearer tok-abc' got %q", gotAuth)
	}
	if gotOrg != "42" {
		t.Errorf("org: want 42 got %q", gotOrg)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type: want application/json got %q", gotCT)
	}
	if !out.OK {
		t.Error("response not parsed")
	}
}

func TestClient_401ReturnsUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid"}`))
	}))
	defer srv.Close()
	err := New(srv.URL, "", 0).Do("GET", "/x", nil, nil)
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("want ErrUnauthorized got %v", err)
	}
}

func TestClient_NonOKWrapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`bad request body`))
	}))
	defer srv.Close()
	err := New(srv.URL, "", 0).Do("GET", "/x", nil, nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError got %T", err)
	}
	if apiErr.Status != 400 || !strings.Contains(apiErr.Body, "bad request body") {
		t.Errorf("unexpected APIError: %+v", apiErr)
	}
}

func TestClient_NoTokenNoAuthHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.WriteHeader(204)
	}))
	defer srv.Close()
	_ = New(srv.URL, "", 0).Do("GET", "/x", nil, nil)
	if got != "" {
		t.Errorf("expected no Authorization, got %q", got)
	}
}

func TestClient_RawBytesOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-json-content"))
	}))
	defer srv.Close()
	var raw []byte
	if err := New(srv.URL, "", 0).Do("GET", "/x", nil, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw) != "not-json-content" {
		t.Errorf("raw mismatch: %q", string(raw))
	}
}
