package api

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"clipboard/internal/crypto"
	"clipboard/internal/models"
	"clipboard/internal/store"
)

type secretPayload struct {
	Title    string `json:"title"`
	Kind     string `json:"kind"`
	Username string `json:"username"`
	URL      string `json:"url"`
	Value    string `json:"value"`
	Notes    string `json:"notes"`
}

func (s *Server) handleListSecrets(w http.ResponseWriter, r *http.Request) {
	user, err := s.authenticate(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	secrets, err := s.store.ListSecrets(r.Context(), user.ID)
	if err != nil {
		s.log.Error("list secrets failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to list secrets")
		return
	}

	for i := range secrets {
		if secrets[i].EncryptedValue != "" {
			decrypted, err := crypto.DecryptSecret(secrets[i].EncryptedValue, s.cfg.SecretMasterKey)
			if err == nil {
				secrets[i].Value = decrypted
			} else {
				s.log.Error("decrypt secret failed", "id", secrets[i].ID, "err", err)
			}
		}
	}

	if secrets == nil {
		secrets = []models.Secret{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"secrets": secrets})
}

func (s *Server) handleCreateSecret(w http.ResponseWriter, r *http.Request) {
	user, err := s.authenticate(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req secretPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = models.SecretKindPassword
	}

	encryptedVal, err := crypto.EncryptSecret(req.Value, s.cfg.SecretMasterKey)
	if err != nil {
		s.log.Error("encrypt secret failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to encrypt secret")
		return
	}

	sec := models.Secret{
		UserID:         user.ID,
		Title:          title,
		Kind:           kind,
		Username:       strings.TrimSpace(req.Username),
		URL:            strings.TrimSpace(req.URL),
		EncryptedValue: encryptedVal,
		Notes:          strings.TrimSpace(req.Notes),
	}

	created, err := s.store.CreateSecret(r.Context(), sec)
	if err != nil {
		s.log.Error("create secret failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to save secret")
		return
	}

	created.Value = req.Value
	writeJSON(w, http.StatusCreated, map[string]any{"secret": created})
}

func (s *Server) handleGetSecret(w http.ResponseWriter, r *http.Request) {
	user, err := s.authenticate(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := r.PathValue("id")
	sec, err := s.store.GetSecret(r.Context(), user.ID, id)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "secret not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get secret")
		return
	}

	if sec.EncryptedValue != "" {
		decrypted, err := crypto.DecryptSecret(sec.EncryptedValue, s.cfg.SecretMasterKey)
		if err == nil {
			sec.Value = decrypted
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"secret": sec})
}

func (s *Server) handleUpdateSecret(w http.ResponseWriter, r *http.Request) {
	user, err := s.authenticate(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := r.PathValue("id")
	existing, err := s.store.GetSecret(r.Context(), user.ID, id)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "secret not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to find secret")
		return
	}

	var req secretPayload
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if strings.TrimSpace(req.Title) != "" {
		existing.Title = strings.TrimSpace(req.Title)
	}
	if strings.TrimSpace(req.Kind) != "" {
		existing.Kind = strings.TrimSpace(req.Kind)
	}
	existing.Username = strings.TrimSpace(req.Username)
	existing.URL = strings.TrimSpace(req.URL)
	existing.Notes = strings.TrimSpace(req.Notes)

	if req.Value != "" {
		enc, err := crypto.EncryptSecret(req.Value, s.cfg.SecretMasterKey)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to encrypt secret")
			return
		}
		existing.EncryptedValue = enc
	}

	updated, err := s.store.UpdateSecret(r.Context(), existing)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update secret")
		return
	}

	if req.Value != "" {
		updated.Value = req.Value
	} else {
		decrypted, _ := crypto.DecryptSecret(existing.EncryptedValue, s.cfg.SecretMasterKey)
		updated.Value = decrypted
	}

	writeJSON(w, http.StatusOK, map[string]any{"secret": updated})
}

func (s *Server) handleDeleteSecret(w http.ResponseWriter, r *http.Request) {
	user, err := s.authenticate(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := r.PathValue("id")
	err = s.store.DeleteSecret(r.Context(), user.ID, id)
	if err != nil {
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "secret not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete secret")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleLookupSecrets is specifically tailored for browser extensions to query secrets
// matching a website's domain or url, and optionally filtered by kind (e.g. password, api_key).
func (s *Server) handleLookupSecrets(w http.ResponseWriter, r *http.Request) {
	user, err := s.authenticate(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	target := strings.TrimSpace(r.URL.Query().Get("url"))
	if target == "" {
		target = strings.TrimSpace(r.URL.Query().Get("q"))
	}

	var secrets []models.Secret
	if target == "" {
		var err error
		secrets, err = s.store.ListSecrets(r.Context(), user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to lookup secrets")
			return
		}
	} else {
		// Clean URL to domain if full URL passed
		domain := target
		domain = strings.TrimPrefix(domain, "https://")
		domain = strings.TrimPrefix(domain, "http://")
		if slashIdx := strings.Index(domain, "/"); slashIdx != -1 {
			domain = domain[:slashIdx]
		}
		domain = strings.TrimPrefix(domain, "www.")

		var err error
		secrets, err = s.store.SearchSecretsByURL(r.Context(), user.ID, domain)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to lookup secrets")
			return
		}
	}

	kindFilter := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kindFilter != "" {
		var filtered []models.Secret
		for _, sec := range secrets {
			if strings.EqualFold(sec.Kind, kindFilter) {
				filtered = append(filtered, sec)
			}
		}
		secrets = filtered
	}

	for i := range secrets {
		if secrets[i].EncryptedValue != "" {
			decrypted, err := crypto.DecryptSecret(secrets[i].EncryptedValue, s.cfg.SecretMasterKey)
			if err == nil {
				secrets[i].Value = decrypted
			}
		}
	}

	if secrets == nil {
		secrets = []models.Secret{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"secrets": secrets})
}

type importSecretsRequest struct {
	Secrets []secretPayload `json:"secrets"`
}

func (s *Server) handleImportSecrets(w http.ResponseWriter, r *http.Request) {
	user, err := s.authenticate(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	contentType := r.Header.Get("Content-Type")
	var payloads []secretPayload

	if strings.Contains(contentType, "multipart/form-data") {
		// Parse uploaded CSV file from multipart form
		if err := r.ParseMultipartForm(10 << 20); err != nil { // 10MB limit
			writeError(w, http.StatusBadRequest, "failed to parse multipart form")
			return
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "file field is required in form")
			return
		}
		defer file.Close()

		parsed, err := parseCSVSecrets(file)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid CSV: %v", err))
			return
		}
		payloads = parsed
	} else if strings.Contains(contentType, "text/csv") {
		parsed, err := parseCSVSecrets(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid CSV: %v", err))
			return
		}
		payloads = parsed
	} else {
		// Standard JSON body
		var req importSecretsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		payloads = req.Secrets
	}

	if len(payloads) == 0 {
		writeError(w, http.StatusBadRequest, "no credentials provided for import")
		return
	}

	var toInsert []models.Secret
	skipped := 0

	for _, item := range payloads {
		val := strings.TrimSpace(item.Value)
		if val == "" {
			skipped++
			continue
		}

		title := strings.TrimSpace(item.Title)
		urlStr := strings.TrimSpace(item.URL)
		if title == "" && urlStr != "" {
			title = cleanDomainFromURL(urlStr)
		}
		if title == "" {
			title = "Imported Credential"
		}

		kind := strings.TrimSpace(item.Kind)
		if kind == "" {
			kind = models.SecretKindPassword
		}

		enc, err := crypto.EncryptSecret(val, s.cfg.SecretMasterKey)
		if err != nil {
			s.log.Error("encrypt secret during import failed", "err", err)
			continue
		}

		toInsert = append(toInsert, models.Secret{
			UserID:         user.ID,
			Title:          title,
			Kind:           kind,
			Username:       strings.TrimSpace(item.Username),
			URL:            urlStr,
			EncryptedValue: enc,
			Notes:          strings.TrimSpace(item.Notes),
		})
	}

	inserted, err := s.store.CreateSecretsBatch(r.Context(), toInsert)
	if err != nil {
		s.log.Error("batch create secrets failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to import secrets batch")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"imported": inserted,
		"skipped":  skipped,
		"total":    len(payloads),
	})
}

func parseCSVSecrets(reader io.Reader) ([]secretPayload, error) {
	csvReader := csv.NewReader(reader)
	csvReader.FieldsPerRecord = -1
	csvReader.TrimLeadingSpace = true

	records, err := csvReader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}
	if len(records) < 2 {
		return nil, errors.New("csv is empty or missing headers")
	}

	headerMap := make(map[string]int)
	for i, h := range records[0] {
		cleaned := strings.ToLower(strings.Trim(strings.TrimSpace(h), `"'`))
		headerMap[cleaned] = i
	}

	colIdx := func(candidates ...string) int {
		for _, c := range candidates {
			if idx, ok := headerMap[c]; ok {
				return idx
			}
		}
		return -1
	}

	titleIdx := colIdx("name", "title", "folder")
	urlIdx := colIdx("url", "login_uri", "uri", "website", "formactionorigin")
	userIdx := colIdx("username", "login_username", "user", "login", "email")
	passIdx := colIdx("password", "login_password", "pass")
	notesIdx := colIdx("notes", "note")

	if passIdx == -1 {
		return nil, errors.New("no password column found in CSV")
	}

	var parsed []secretPayload
	for _, row := range records[1:] {
		val := ""
		if passIdx < len(row) {
			val = strings.TrimSpace(row[passIdx])
		}
		if val == "" {
			continue
		}

		title := ""
		if titleIdx != -1 && titleIdx < len(row) {
			title = strings.TrimSpace(row[titleIdx])
		}

		rawURL := ""
		if urlIdx != -1 && urlIdx < len(row) {
			rawURL = strings.TrimSpace(row[urlIdx])
		}

		user := ""
		if userIdx != -1 && userIdx < len(row) {
			user = strings.TrimSpace(row[userIdx])
		}

		notes := ""
		if notesIdx != -1 && notesIdx < len(row) {
			notes = strings.TrimSpace(row[notesIdx])
		}

		if title == "" && rawURL != "" {
			title = cleanDomainFromURL(rawURL)
		}
		if title == "" {
			title = "Imported Credential"
		}

		parsed = append(parsed, secretPayload{
			Title:    title,
			URL:      rawURL,
			Username: user,
			Value:    val,
			Notes:    notes,
			Kind:     models.SecretKindPassword,
		})
	}

	return parsed, nil
}

func cleanDomainFromURL(rawURL string) string {
	d := rawURL
	d = strings.TrimPrefix(d, "https://")
	d = strings.TrimPrefix(d, "http://")
	if idx := strings.Index(d, "/"); idx != -1 {
		d = d[:idx]
	}
	if idx := strings.Index(d, ":"); idx != -1 {
		d = d[:idx]
	}
	d = strings.TrimPrefix(d, "www.")
	if d == "" {
		return "Imported Credential"
	}
	return strings.ToUpper(d[:1]) + d[1:]
}
