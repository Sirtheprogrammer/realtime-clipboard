package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleSetPassword_Unauthorized(t *testing.T) {
	srv := &Server{}

	req := httptest.NewRequest("POST", "/api/auth/password", bytes.NewBufferString(`{"new_password":"strongpassword123"}`))
	w := httptest.NewRecorder()

	srv.handleSetPassword(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401 unauthorized, got %d", resp.StatusCode)
	}
}
