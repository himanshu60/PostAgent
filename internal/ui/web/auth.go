package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/clodura/linkedin-post-agent/internal/config"
	"github.com/clodura/linkedin-post-agent/internal/models"
	"github.com/clodura/linkedin-post-agent/internal/publisher"
)

type ctxKey string

const userCtxKey ctxKey = "pa_user"

const (
	sessionCookie = "pa_session"
	stateCookie   = "pa_oauth_state"
	sessionTTL    = 30 * 24 * time.Hour
)

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// baseURL reconstructs the externally-visible origin, honoring the proxy
// headers Render/other PaaS set so the OAuth redirect URI matches in prod.
func baseURL(r *http.Request) string {
	scheme := "http"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	} else if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if fh := r.Header.Get("X-Forwarded-Host"); fh != "" {
		host = fh
	}
	return scheme + "://" + host
}

// loginClient builds a LinkedIn OAuth client for the sign-in flow, with the
// redirect URI pointed back at this request's /auth/callback.
func (s *Server) loginClient(r *http.Request) *publisher.Client {
	return publisher.NewClient(config.LinkedInConfig{
		ClientID:     s.li.ClientID,
		ClientSecret: s.li.ClientSecret,
		RedirectURI:  baseURL(r) + "/auth/callback",
		APIVersion:   s.li.APIVersion,
	}, s.logger)
}

// currentUser loads the logged-in user from the session cookie, or nil.
func (s *Server) currentUser(r *http.Request) *models.User {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	sess, err := s.sessions.Get(r.Context(), c.Value)
	if err != nil {
		return nil
	}
	u, err := s.users.GetByID(r.Context(), sess.UserID)
	if err != nil {
		return nil
	}
	return u
}

func userFromCtx(ctx context.Context) *models.User {
	u, _ := ctx.Value(userCtxKey).(*models.User)
	return u
}

// requireAuth wraps a handler, redirecting anonymous visitors to /login and
// otherwise injecting the current user into the request context.
func (s *Server) requireAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := s.currentUser(r)
		if u == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		ctx := context.WithValue(r.Context(), userCtxKey, u)
		h(w, r.WithContext(ctx))
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	// Already logged in → go home.
	if s.currentUser(r) != nil {
		s.redirect(w, r, "/")
		return
	}
	s.renderBare(w, "login", "Sign in", map[string]interface{}{
		"Error": r.URL.Query().Get("err"),
	})
}

// handleLinkedInStart kicks off the OAuth flow.
func (s *Server) handleLinkedInStart(w http.ResponseWriter, r *http.Request) {
	if s.li.ClientID == "" {
		http.Error(w, "LinkedIn login is not configured on this server", http.StatusServiceUnavailable)
		return
	}
	state := randomToken()
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookie,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
	http.Redirect(w, r, s.loginClient(r).AuthURL(state), http.StatusSeeOther)
}

// handleLinkedInCallback completes OAuth: exchange code → identity → upsert
// user with their posting tokens → create session.
func (s *Server) handleLinkedInCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if e := r.URL.Query().Get("error"); e != "" {
		s.logger.Warn("LinkedIn OAuth error", zap.String("error", e), zap.String("desc", r.URL.Query().Get("error_description")))
		s.redirect(w, r, "/login?err=LinkedIn+sign-in+was+cancelled")
		return
	}
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	sc, _ := r.Cookie(stateCookie)
	if code == "" || state == "" || sc == nil || sc.Value != state {
		s.redirect(w, r, "/login?err=Sign-in+failed+(state+mismatch),+please+try+again")
		return
	}

	client := s.loginClient(r)
	if err := client.ExchangeCode(ctx, code); err != nil {
		s.logger.Error("OAuth code exchange failed", zap.Error(err))
		s.redirect(w, r, "/login?err=Could+not+complete+sign-in")
		return
	}
	info, err := publisher.FetchUserInfo(ctx, client.AccessToken())
	if err != nil {
		s.logger.Error("Fetch userinfo failed", zap.Error(err))
		s.redirect(w, r, "/login?err=Could+not+read+your+LinkedIn+profile")
		return
	}

	user := &models.User{
		ID:             info.Sub,
		Name:           info.Name,
		Email:          info.Email,
		Picture:        info.Picture,
		AccessToken:    client.AccessToken(),
		RefreshToken:   client.RefreshTokenStr(),
		TokenExpiresAt: time.Now().Add(50 * 24 * time.Hour),
	}
	if err := s.users.Upsert(ctx, user); err != nil {
		s.logger.Error("Upsert user failed", zap.Error(err))
		s.redirect(w, r, "/login?err=Sign-in+failed,+please+try+again")
		return
	}

	token := randomToken()
	if err := s.sessions.Create(ctx, &models.Session{
		Token:     token,
		UserID:    user.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(sessionTTL),
	}); err != nil {
		s.logger.Error("Create session failed", zap.Error(err))
		s.redirect(w, r, "/login?err=Sign-in+failed,+please+try+again")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	s.logger.Info("User signed in", zap.String("user_id", user.ID), zap.String("name", user.Name))
	s.redirect(w, r, "/")
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.sessions.Delete(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	s.redirect(w, r, "/login")
}
