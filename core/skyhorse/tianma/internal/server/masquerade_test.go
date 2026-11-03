package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMasqueradeString(t *testing.T) {
	handler, err := NewMasquerade(MasqueradeConfig{Type: "string", Content: "site-ok"})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "site-ok" {
		t.Fatalf("got %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestMasqueradeDefault404(t *testing.T) {
	handler, err := NewMasquerade(MasqueradeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("got %d", recorder.Code)
	}
}

func TestMasqueradeFileRequiresRoot(t *testing.T) {
	if _, err := NewMasquerade(MasqueradeConfig{Type: "file"}); err == nil {
		t.Fatal("file without root must fail")
	}
}

func TestMasqueradeProxyRequiresURL(t *testing.T) {
	if _, err := NewMasquerade(MasqueradeConfig{Type: "proxy"}); err == nil {
		t.Fatal("proxy without url must fail")
	}
}

func TestMasqueradeUnknownType(t *testing.T) {
	if _, err := NewMasquerade(MasqueradeConfig{Type: "nope"}); err == nil {
		t.Fatal("unknown type must fail")
	}
}
