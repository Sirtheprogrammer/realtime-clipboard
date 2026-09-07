package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleExtensionDownload(t *testing.T) {
	srv := &Server{}

	req := httptest.NewRequest("GET", "/api/extension/download", nil)
	w := httptest.NewRecorder()

	srv.handleExtensionDownload(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/zip" {
		t.Errorf("expected Content-Type application/zip, got %s", contentType)
	}

	contentDisposition := resp.Header.Get("Content-Disposition")
	if contentDisposition != "attachment; filename=\"clipboard-vault-extension.zip\"" {
		t.Errorf("expected attachment header, got %s", contentDisposition)
	}

	body := w.Body.Bytes()
	if len(body) == 0 {
		t.Fatal("expected non-empty zip body")
	}
}
