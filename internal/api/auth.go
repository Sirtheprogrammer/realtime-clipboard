package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"clipboard/internal/crypto"
	"clipboard/internal/models"
	"clipboard/internal/store"
)

const sessionDuration = 30 * 24 * time.Hour
const sessionCookieName = "clipboard_session"

type authRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authResponse struct {
	Token string      `json:"token"`
	User  models.User `json:"user"`
}

func (s *Server) extractToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		return cookie.Value
	}
	return ""
}

func (s *Server) authenticate(r *http.Request) (models.User, error) {
	token := s.extractToken(r)
	if token == "" {
		return models.User{}, store.ErrNotFound
	}

	sess, err := s.store.GetSession(r.Context(), token)
	if err != nil {
		return models.User{}, err
	}

	return s.store.GetUserByID(r.Context(), sess.UserID)
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || !strings.Contains(email, "@") {
		writeError(w, http.StatusBadRequest, "valid email is required")
		return
	}

	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	// Check if already exists
	if _, err := s.store.GetUserByEmail(r.Context(), email); err == nil {
		writeError(w, http.StatusConflict, "an account with this email already exists")
		return
	}

	pwdHash, err := crypto.HashPassword(req.Password)
	if err != nil {
		s.log.Error("hash password failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to process credentials")
		return
	}

	user, err := s.store.CreateUser(r.Context(), email, pwdHash, "", "", "")
	if err != nil {
		s.log.Error("create user failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to create account")
		return
	}

	token, err := crypto.GenerateSessionToken()
	if err != nil {
		s.log.Error("generate session token failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to generate session")
		return
	}

	_, err = s.store.CreateSession(r.Context(), user.ID, token, time.Now().UTC().Add(sessionDuration))
	if err != nil {
		s.log.Error("save session failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to save session")
		return
	}

	s.setSessionCookie(w, token, sessionDuration)
	writeJSON(w, http.StatusCreated, authResponse{Token: token, User: user})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	user, err := s.store.GetUserByEmail(r.Context(), email)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	if user.PasswordHash == "" || !crypto.VerifyPassword(req.Password, user.PasswordHash) {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	token, err := crypto.GenerateSessionToken()
	if err != nil {
		s.log.Error("generate session token failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to generate session")
		return
	}

	_, err = s.store.CreateSession(r.Context(), user.ID, token, time.Now().UTC().Add(sessionDuration))
	if err != nil {
		s.log.Error("save session failed", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to save session")
		return
	}

	s.setSessionCookie(w, token, sessionDuration)
	writeJSON(w, http.StatusOK, authResponse{Token: token, User: user})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	token := s.extractToken(r)
	if token != "" {
		_ = s.store.DeleteSession(r.Context(), token)
	}

	s.setSessionCookie(w, "", -time.Hour)
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, err := s.authenticate(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":  user,
		"token": s.extractToken(r),
	})
}

// GitHub OAuth
func (s *Server) handleGitHubAuth(w http.ResponseWriter, r *http.Request) {
	if s.cfg.GitHubClientID == "" {
		writeError(w, http.StatusBadRequest, "GitHub authentication is not configured on this server")
		return
	}

	state, err := crypto.GenerateSessionToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate oauth state")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "github_oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   600, // 10 minutes
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	redirectURI := s.githubRedirectURI(r)
	authURL := fmt.Sprintf(
		"https://github.com/login/oauth/authorize?client_id=%s&redirect_uri=%s&scope=user:email&state=%s",
		url.QueryEscape(s.cfg.GitHubClientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape(state),
	)

	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

func (s *Server) handleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	if s.cfg.GitHubClientID == "" || s.cfg.GitHubClientSecret == "" {
		writeError(w, http.StatusBadRequest, "GitHub OAuth not configured")
		return
	}

	stateCookie, err := r.Cookie("github_oauth_state")
	if err != nil || stateCookie.Value == "" || stateCookie.Value != r.URL.Query().Get("state") {
		writeError(w, http.StatusBadRequest, "invalid or expired oauth state")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "missing authorization code")
		return
	}

	// 1. Exchange code for access token
	token, err := s.exchangeGitHubCode(r.Context(), code, s.githubRedirectURI(r))
	if err != nil {
		s.log.Error("exchange github code failed", "err", err)
		writeError(w, http.StatusBadGateway, "failed to authenticate with GitHub")
		return
	}

	// 2. Fetch user profile and email
	ghUser, err := s.fetchGitHubUser(r.Context(), token)
	if err != nil {
		s.log.Error("fetch github user failed", "err", err)
		writeError(w, http.StatusBadGateway, "failed to fetch GitHub profile")
		return
	}

	// 3. Match or create user
	user, err := s.store.GetUserByGitHubID(r.Context(), ghUser.ID)
	if err != nil {
		// Try matching by email
		user, err = s.store.GetUserByEmail(r.Context(), ghUser.Email)
		if err == nil {
			// Link github to existing account
			_ = s.store.LinkGitHubAccount(r.Context(), user.ID, ghUser.ID, ghUser.Login, ghUser.AvatarURL)
			user.GitHubID = ghUser.ID
			user.GitHubUser = ghUser.Login
			user.AvatarURL = ghUser.AvatarURL
		} else {
			// Create brand new user
			user, err = s.store.CreateUser(r.Context(), ghUser.Email, "", ghUser.ID, ghUser.Login, ghUser.AvatarURL)
			if err != nil {
				s.log.Error("create github user failed", "err", err)
				writeError(w, http.StatusInternalServerError, "failed to create user from GitHub")
				return
			}
		}
	}

	// 4. Create session
	sessToken, err := crypto.GenerateSessionToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate session")
		return
	}

	_, err = s.store.CreateSession(r.Context(), user.ID, sessToken, time.Now().UTC().Add(sessionDuration))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save session")
		return
	}

	s.setSessionCookie(w, sessToken, sessionDuration)

	// Redirect back to app shell with auth flag
	http.Redirect(w, r, "/?auth=success", http.StatusSeeOther)
}

func (s *Server) githubRedirectURI(r *http.Request) string {
	if s.cfg.BaseURL != "" {
		return s.cfg.BaseURL + "/api/auth/github/callback"
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/api/auth/github/callback", scheme, r.Host)
}

type ghTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Error       string `json:"error"`
}

func (s *Server) exchangeGitHubCode(ctx context.Context, code, redirectURI string) (string, error) {
	data := url.Values{}
	data.Set("client_id", s.cfg.GitHubClientID)
	data.Set("client_secret", s.cfg.GitHubClientSecret)
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://github.com/login/oauth/access_token", strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var ghResp ghTokenResponse
	if err := json.Unmarshal(body, &ghResp); err != nil {
		return "", err
	}
	if ghResp.Error != "" {
		return "", fmt.Errorf("github oauth error: %s", ghResp.Error)
	}
	return ghResp.AccessToken, nil
}

type ghProfile struct {
	ID        string
	Login     string
	Email     string
	AvatarURL string
}

func (s *Server) fetchGitHubUser(ctx context.Context, token string) (ghProfile, error) {
	var prof ghProfile

	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
	if err != nil {
		return prof, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return prof, err
	}
	defer resp.Body.Close()

	var raw struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return prof, err
	}

	prof.ID = strconv.FormatInt(raw.ID, 10)
	prof.Login = raw.Login
	prof.AvatarURL = raw.AvatarURL
	prof.Email = raw.Email

	// If email is not public, fetch from user emails endpoint
	if prof.Email == "" {
		emailReq, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user/emails", nil)
		if err == nil {
			emailReq.Header.Set("Authorization", "Bearer "+token)
			emailReq.Header.Set("Accept", "application/vnd.github.v3+json")
			if emailResp, err := http.DefaultClient.Do(emailReq); err == nil {
				defer emailResp.Body.Close()
				var emails []struct {
					Email    string `json:"email"`
					Primary  bool   `json:"primary"`
					Verified bool   `json:"verified"`
				}
				if err := json.NewDecoder(emailResp.Body).Decode(&emails); err == nil {
					for _, e := range emails {
						if e.Primary && e.Verified {
							prof.Email = e.Email
							break
						}
					}
					if prof.Email == "" && len(emails) > 0 {
						prof.Email = emails[0].Email
					}
				}
			}
		}
	}

	if prof.Email == "" {
		prof.Email = fmt.Sprintf("%s@users.noreply.github.com", prof.Login)
	}

	return prof, nil
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(maxAge.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
