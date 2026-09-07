package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	if contentDisposition != `attachment; filename="clipboard-vault-extension.zip"` {
		t.Errorf("expected attachment header, got %s", contentDisposition)
	}

	body := w.Body.Bytes()
	if len(body) == 0 {
		t.Fatal("expected non-empty zip body")
	}
}

func TestHandleExtensionVersion(t *testing.T) {
	srv := &Server{}

	req := httptest.NewRequest("GET", "/api/extension/version", nil)
	req.Host = "clip.codesky.tech"
	w := httptest.NewRecorder()

	srv.handleExtensionVersion(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var data map[string]any
	if err := json.NewDecoder(w.Body).Decode(&data); err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}

	if data["version"] != ExtensionCurrentVersion {
		t.Errorf("expected version %s, got %v", ExtensionCurrentVersion, data["version"])
	}
	if !strings.Contains(data["download_url"].(string), "/api/extension/download") {
		t.Errorf("unexpected download_url: %v", data["download_url"])
	}
}

func TestHandleExtensionUpdatesXML(t *testing.T) {
	srv := &Server{}

	req := httptest.NewRequest("GET", "/api/extension/updates.xml", nil)
	req.Host = "clip.codesky.tech"
	w := httptest.NewRecorder()

	srv.handleExtensionUpdatesXML(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "xml") {
		t.Errorf("expected xml content type, got %s", contentType)
	}

	body := w.Body.String()
	if !strings.Contains(body, "gupdate") || !strings.Contains(body, ExtensionCurrentVersion) {
		t.Errorf("expected valid update XML body, got %s", body)
	}
}

func TestHandleExtensionUpdatesJSON(t *testing.T) {
	srv := &Server{}

	req := httptest.NewRequest("GET", "/api/extension/updates.json", nil)
	req.Host = "clip.codesky.tech"
	w := httptest.NewRecorder()

	srv.handleExtensionUpdatesJSON(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var data map[string]any
	if err := json.NewDecoder(w.Body).Decode(&data); err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}

	addons, ok := data["addons"].(map[string]any)
	if !ok || addons["clipboard-vault@local"] == nil {
		t.Errorf("missing addons in json response: %v", data)
	}
}
