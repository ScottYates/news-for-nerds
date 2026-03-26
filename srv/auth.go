package srv

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"srv.exe.dev/db/dbgen"
)

const (
	sessionCookieName = "session"
	visitorCookieName = "visitor_id"
	sessionDuration   = 30 * 24 * time.Hour // 30 days
	visitorDuration   = 365 * 24 * time.Hour // 1 year
)

var (
	googleClientID     = os.Getenv("GOOGLE_CLIENT_ID")
	googleClientSecret = os.Getenv("GOOGLE_CLIENT_SECRET")
)

type GoogleTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

type GoogleUserInfo struct {
	ID            string `json:"id"`
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

func generateState() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func (s *Server) getRedirectURI(r *http.Request) string {
	scheme := "https"
	host := r.Host

	// Check for proxy headers (exe.dev proxy)
	if fwdHost := r.Header.Get("X-Forwarded-Host"); fwdHost != "" {
		host = fwdHost
	}
	if fwdProto := r.Header.Get("X-Forwarded-Proto"); fwdProto != "" {
		scheme = fwdProto
	} else if r.TLS == nil && !strings.Contains(host, "exe.xyz") {
		scheme = "http"
	}

	return fmt.Sprintf("%s://%s/auth/callback", scheme, host)
}

// getCookieDomain returns just the hostname without port for cookie Domain attribute
// This ensures cookies work across different ports (8000, 443, 80)
func (s *Server) getCookieDomain(r *http.Request) string {
	host := r.Host
	if fwdHost := r.Header.Get("X-Forwarded-Host"); fwdHost != "" {
		host = fwdHost
	}
	// Strip port if present
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		// Make sure it's not an IPv6 address
		if !strings.Contains(host[idx:], "]") {
			host = host[:idx]
		}
	}
	return host
}

func (s *Server) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if googleClientID == "" {
		http.Error(w, "OAuth not configured", http.StatusServiceUnavailable)
		return
	}

	state := generateState()
	
	// Store state in a cookie for verification
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		Domain:   s.getCookieDomain(r),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600, // 10 minutes
	})

	// Store return URL to redirect back after login
	returnURL := r.URL.Query().Get("return")
	if returnURL == "" {
		returnURL = r.Referer()
	}
	if returnURL != "" {
		http.SetCookie(w, &http.Cookie{
			Name:     "oauth_return",
			Value:    returnURL,
			Path:     "/",
			Domain:   s.getCookieDomain(r),
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   600, // 10 minutes
		})
	}

	redirectURI := s.getRedirectURI(r)
	authURL := fmt.Sprintf(
		"https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=openid%%20email%%20profile&state=%s",
		url.QueryEscape(googleClientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape(state),
	)

	http.Redirect(w, r, authURL, http.StatusFound)
}

func (s *Server) HandleCallback(w http.ResponseWriter, r *http.Request) {
	// Verify state
	stateCookie, err := r.Cookie("oauth_state")
	if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, "Invalid state", http.StatusBadRequest)
		return
	}

	// Clear state cookie
	http.SetCookie(w, &http.Cookie{
		Name:   "oauth_state",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "No code provided", http.StatusBadRequest)
		return
	}

	// Exchange code for token
	redirectURI := s.getRedirectURI(r)
	tokenResp, err := s.exchangeCode(code, redirectURI)
	if err != nil {
		slog.Error("failed to exchange code", "error", err)
		http.Error(w, "Failed to authenticate", http.StatusInternalServerError)
		return
	}

	// Get user info
	userInfo, err := s.getUserInfo(tokenResp.AccessToken)
	if err != nil {
		slog.Error("failed to get user info", "error", err)
		http.Error(w, "Failed to get user info", http.StatusInternalServerError)
		return
	}

	// Create or update user
	ctx := r.Context()
	q := dbgen.New(s.DB)
	
	// Check if there's an exe.dev user ID we should link
	exedevUserID := strings.TrimSpace(r.Header.Get("X-ExeDev-UserID"))
	
	user, err := q.GetUserByEmail(ctx, userInfo.Email)
	now := time.Now()
	
	if err != nil {
		// Create new user
		userID := uuid.New().String()
		err = q.CreateUser(ctx, dbgen.CreateUserParams{
			ID:        userID,
			Email:     userInfo.Email,
			Name:      userInfo.Name,
			Picture:   &userInfo.Picture,
			CreatedAt: now,
			LastLogin: now,
		})
		if err != nil {
			slog.Error("failed to create user", "error", err)
			http.Error(w, "Failed to create user", http.StatusInternalServerError)
			return
		}
		user.ID = userID
	} else {
		// Update existing user
		err = q.UpdateUserLogin(ctx, dbgen.UpdateUserLoginParams{
			Name:      userInfo.Name,
			Picture:   &userInfo.Picture,
			LastLogin: now,
			ID:        user.ID,
		})
		if err != nil {
			slog.Warn("failed to update user", "error", err)
		}
	}
	
	// Migrate pages from visitor cookie identity to the Google OAuth user
	if visitorCookie, cookieErr := r.Cookie(visitorCookieName); cookieErr == nil && visitorCookie.Value != "" {
		visitorID := "visitor:" + visitorCookie.Value
		err = q.UpdatePagesOwnership(ctx, dbgen.UpdatePagesOwnershipParams{
			UserID:   user.ID,
			UserID_2: visitorID,
		})
		if err != nil {
			slog.Warn("failed to migrate visitor pages", "error", err)
		} else {
			slog.Info("migrated pages from visitor to Google user", "from", visitorID, "to", user.ID)
		}
		// Clear the visitor cookie since they now have a real session
		http.SetCookie(w, &http.Cookie{
			Name:   visitorCookieName,
			Value:  "",
			Path:   "/",
			Domain: s.getCookieDomain(r),
			MaxAge: -1,
		})
	}

	// Migrate pages from "anonymous" to the Google OAuth user (legacy cleanup)
	_ = q.UpdatePagesOwnership(ctx, dbgen.UpdatePagesOwnershipParams{
		UserID:   user.ID,
		UserID_2: "anonymous",
	})

	// Link exe.dev user ID if present and not already linked
	if exedevUserID != "" && exedevUserID != "anonymous" {
		// Link the exe.dev ID to this Google OAuth user
		err = q.LinkExedevID(ctx, dbgen.LinkExedevIDParams{
			ExedevID: &exedevUserID,
			ID:       user.ID,
		})
		if err != nil {
			slog.Warn("failed to link exedev ID", "error", err)
		} else {
			slog.Info("linked exedev ID to Google user", "exedev_id", exedevUserID, "user_id", user.ID)
		}
		
		// Migrate any pages owned by the exe.dev user to the Google OAuth user
		err = q.UpdatePagesOwnership(ctx, dbgen.UpdatePagesOwnershipParams{
			UserID:   user.ID,
			UserID_2: exedevUserID,
		})
		if err != nil {
			slog.Warn("failed to migrate pages ownership", "error", err)
		} else {
			slog.Info("migrated pages from exedev user to Google user", "from", exedevUserID, "to", user.ID)
		}
	}

	// Auto-assign slugs to any of this user's pages that don't have one
	userPages, _ := q.GetPagesByUserID(ctx, user.ID)
	for _, pg := range userPages {
		if pg.Slug == nil || *pg.Slug == "" {
			s.assignSlug(ctx, q, pg.ID, userInfo.Name, userInfo.Email)
		}
	}

	// Create session
	sessionID := uuid.New().String()
	err = q.CreateSession(ctx, dbgen.CreateSessionParams{
		ID:        sessionID,
		UserID:    user.ID,
		CreatedAt: now,
		ExpiresAt: now.Add(sessionDuration),
	})
	if err != nil {
		slog.Error("failed to create session", "error", err)
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	// Set session cookie with domain to work across ports
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		Domain:   s.getCookieDomain(r),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionDuration.Seconds()),
	})

	// Redirect to return URL if set, otherwise home
	returnURL := "/"
	if returnCookie, err := r.Cookie("oauth_return"); err == nil && returnCookie.Value != "" {
		returnURL = returnCookie.Value
		// Clear the return cookie
		http.SetCookie(w, &http.Cookie{
			Name:   "oauth_return",
			Value:  "",
			Path:   "/",
			MaxAge: -1,
		})
	}
	http.Redirect(w, r, returnURL, http.StatusFound)
}

func (s *Server) HandleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		q := dbgen.New(s.DB)
		q.DeleteSession(r.Context(), cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:   sessionCookieName,
		Value:  "",
		Path:   "/",
		Domain: s.getCookieDomain(r),
		MaxAge: -1,
	})

	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) exchangeCode(code, redirectURI string) (*GoogleTokenResponse, error) {
	data := url.Values{}
	data.Set("code", code)
	data.Set("client_id", googleClientID)
	data.Set("client_secret", googleClientSecret)
	data.Set("redirect_uri", redirectURI)
	data.Set("grant_type", "authorization_code")

	resp, err := http.Post(
		"https://oauth2.googleapis.com/token",
		"application/x-www-form-urlencoded",
		strings.NewReader(data.Encode()),
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("token exchange failed: %s", string(body))
	}

	var tokenResp GoogleTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, err
	}

	return &tokenResp, nil
}

func (s *Server) getUserInfo(accessToken string) (*GoogleUserInfo, error) {
	req, _ := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var userInfo GoogleUserInfo
	if err := json.Unmarshal(body, &userInfo); err != nil {
		return nil, err
	}

	return &userInfo, nil
}

// GetUserFromRequest gets the current user from the session cookie
func (s *Server) GetUserFromRequest(r *http.Request) (*dbgen.GetSessionRow, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil, err
	}

	q := dbgen.New(s.DB)
	session, err := q.GetSession(r.Context(), cookie.Value)
	if err != nil {
		return nil, err
	}

	return &session, nil
}

// GetUserID returns the user ID from session, exe.dev header, or visitor cookie.
// It never returns "anonymous" — every visitor gets a stable unique ID.
func (s *Server) GetUserID(r *http.Request) string {
	// First check for Google OAuth session
	session, err := s.GetUserFromRequest(r)
	if err == nil {
		return session.UserID
	}
	
	// Fall back to exe.dev proxy header
	if exeUserID := strings.TrimSpace(r.Header.Get("X-ExeDev-UserID")); exeUserID != "" {
		// Check if this exe.dev user is linked to a Google OAuth user
		q := dbgen.New(s.DB)
		if linkedUser, err := q.GetUserByExedevID(r.Context(), &exeUserID); err == nil {
			return linkedUser.ID
		}
		return exeUserID
	}

	// Fall back to visitor cookie
	if cookie, err := r.Cookie(visitorCookieName); err == nil && cookie.Value != "" {
		return "visitor:" + cookie.Value
	}

	return "anonymous"
}

// GetOrCreateVisitorID reads or creates a visitor_id cookie. Returns the visitor ID.
// Must be called on handlers that create resources (pages, widgets) so the
// visitor gets a stable identity before login.
func (s *Server) GetOrCreateVisitorID(w http.ResponseWriter, r *http.Request) string {
	if cookie, err := r.Cookie(visitorCookieName); err == nil && cookie.Value != "" {
		return "visitor:" + cookie.Value
	}
	vid := uuid.New().String()
	http.SetCookie(w, &http.Cookie{
		Name:     visitorCookieName,
		Value:    vid,
		Path:     "/",
		Domain:   s.getCookieDomain(r),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(visitorDuration.Seconds()),
	})
	return "visitor:" + vid
}

// AuthMiddleware checks authentication for protected routes
func (s *Server) AuthMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _ := s.GetUserFromRequest(r)
		ctx := r.Context()
		if session != nil {
			ctx = context.WithValue(ctx, "user", session)
		}
		next(w, r.WithContext(ctx))
	}
}

// HandleAuthStatus returns the current auth status
func (s *Server) HandleAuthStatus(w http.ResponseWriter, r *http.Request) {
	// Check Google OAuth session first
	session, err := s.GetUserFromRequest(r)
	if err == nil {
		s.writeJSON(w, http.StatusOK, map[string]interface{}{
			"authenticated": true,
			"auth_type":     "google",
			"oauth_enabled": googleClientID != "",
			"user": map[string]interface{}{
				"id":      session.UserID,
				"email":   session.Email,
				"name":    session.UserName,
				"picture": session.Picture,
			},
		})
		return
	}
	
	// Check exe.dev proxy auth
	exeUserID := strings.TrimSpace(r.Header.Get("X-ExeDev-UserID"))
	exeEmail := strings.TrimSpace(r.Header.Get("X-ExeDev-Email"))
	if exeUserID != "" {
		s.writeJSON(w, http.StatusOK, map[string]interface{}{
			"authenticated": true,
			"auth_type":     "exedev",
			"oauth_enabled": googleClientID != "",
			"user": map[string]interface{}{
				"id":    exeUserID,
				"email": exeEmail,
				"name":  exeEmail,
			},
		})
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"authenticated": false,
		"oauth_enabled": googleClientID != "",
	})
}
