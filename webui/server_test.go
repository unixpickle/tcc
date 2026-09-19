package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMountRootHidesUnprefixedRoutes(t *testing.T) {
	handler := mountRoot(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/devices" {
			t.Fatalf("unexpected stripped path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}), "secret")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/devices", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected unprefixed path to be hidden; got %d", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/secret/api/devices", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected prefixed path to route; got %d", recorder.Code)
	}
}

func TestCleanRootRequiresSingleDirectoryName(t *testing.T) {
	if root, err := cleanRoot("/secret/"); err != nil || root != "secret" {
		t.Fatalf("unexpected clean root result: root=%q err=%v", root, err)
	}
	if _, err := cleanRoot("a/b"); err == nil {
		t.Fatal("expected nested root to fail")
	}
	if _, err := cleanRoot(".."); err == nil {
		t.Fatal("expected parent directory root to fail")
	}
}
