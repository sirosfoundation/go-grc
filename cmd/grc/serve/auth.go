package serve

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	mcpserver "github.com/mark3labs/mcp-go/server"
)

const (
	pkceCookieName    = "grc_pkce"
	sessionCookieName = "grc_session"
	sessionCookieTTL  = 90 * 24 * time.Hour
)

// authConfig holds the OIDC settings for grc serve's auth layer. Auth is
// only wired up when Issuer is non-empty; callers that leave it empty get
// the pre-existing unauthenticated behavior.
type authConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	BaseURL      string
	MCPAudience  string
}

func (c authConfig) validate() error {
	if c.ClientID == "" || c.ClientSecret == "" {
		return fmt.Errorf("--auth-client-id and --auth-client-secret are required when --auth-issuer is set")
	}
	if c.BaseURL == "" {
		return fmt.Errorf("--base-url is required when --auth-issuer is set")
	}
	return nil
}

// sessionRecord is the server-side state for a logged-in browser session.
// The session cookie only ever carries the opaque map key, never these values.
type sessionRecord struct {
	subject      string
	email        string
	refreshToken string
	expiry       time.Time
}

// sessionStore is an in-memory, single-instance session store. A restart
// forces re-login, which is an acceptable trade-off for a staff tool running
// as a single Fly machine.
type sessionStore struct {
	mu   sync.Mutex
	data map[string]*sessionRecord
}

func newSessionStore() *sessionStore {
	return &sessionStore{data: make(map[string]*sessionRecord)}
}

func (s *sessionStore) create(rec *sessionRecord) (string, error) {
	id, err := randomToken(32)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.data[id] = rec
	s.mu.Unlock()
	return id, nil
}

func (s *sessionStore) get(id string) (sessionRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.data[id]
	if !ok {
		return sessionRecord{}, false
	}
	return *rec, true
}

func (s *sessionStore) update(id string, rec sessionRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[id]; ok {
		s.data[id] = &rec
	}
}

func (s *sessionStore) delete(id string) {
	s.mu.Lock()
	delete(s.data, id)
	s.mu.Unlock()
}

// oidcAuth wires up both the browser session login flow (protects the
// Docusaurus site) and the OAuth bearer-token check (protects /mcp).
type oidcAuth struct {
	cfg           authConfig
	provider      *oidc.Provider
	verifier      *oidc.IDTokenVerifier // verifies ID tokens (aud == ClientID)
	keySet        oidc.KeySet           // verifies MCP bearer access tokens (no aud check built in)
	oauth2Config  oauth2.Config
	endSessionURL string
	sessions      *sessionStore
}

// newOIDCAuth discovers the issuer and constructs the auth helper. Only
// called when cfg.Issuer is non-empty.
func newOIDCAuth(ctx context.Context, cfg authConfig) (*oidcAuth, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc: discovering issuer %q: %w", cfg.Issuer, err)
	}

	var extra struct {
		EndSessionEndpoint string `json:"end_session_endpoint"`
		JWKSURI            string `json:"jwks_uri"`
	}
	if err := provider.Claims(&extra); err != nil {
		return nil, fmt.Errorf("oidc: parsing discovery document: %w", err)
	}
	if extra.JWKSURI == "" {
		return nil, fmt.Errorf("oidc: issuer %q discovery document has no jwks_uri", cfg.Issuer)
	}

	return &oidcAuth{
		cfg:      cfg,
		provider: provider,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		keySet:   oidc.NewRemoteKeySet(ctx, extra.JWKSURI),
		oauth2Config: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  strings.TrimRight(cfg.BaseURL, "/") + "/auth/callback",
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email", oidc.ScopeOfflineAccess},
		},
		endSessionURL: extra.EndSessionEndpoint,
		sessions:      newSessionStore(),
	}, nil
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func setCookie(w http.ResponseWriter, name, value, path string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
}

func clearCookie(w http.ResponseWriter, name, path string) {
	setCookie(w, name, "", path, -1)
}

// safeReturnTo restricts post-login redirects to same-site relative paths,
// preventing the return_to query parameter from being used as an open redirect.
func safeReturnTo(raw string) string {
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return "/"
	}
	return raw
}

// --- Browser session login flow ---

func (a *oidcAuth) loginHandler(w http.ResponseWriter, r *http.Request) {
	state, err := randomToken(24)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	verifier := oauth2.GenerateVerifier()
	returnTo := safeReturnTo(r.URL.Query().Get("return_to"))

	cookieVal := base64.RawURLEncoding.EncodeToString([]byte(state + "|" + verifier + "|" + returnTo))
	setCookie(w, pkceCookieName, cookieVal, "/auth", 300)

	authURL := a.oauth2Config.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (a *oidcAuth) callbackHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(pkceCookieName)
	if err != nil {
		http.Error(w, "login session expired, please try again", http.StatusBadRequest)
		return
	}
	clearCookie(w, pkceCookieName, "/auth")

	raw, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		http.Error(w, "invalid login state", http.StatusBadRequest)
		return
	}
	parts := strings.SplitN(string(raw), "|", 3)
	if len(parts) != 3 {
		http.Error(w, "invalid login state", http.StatusBadRequest)
		return
	}
	wantState, verifier, returnTo := parts[0], parts[1], parts[2]

	if errParam := r.URL.Query().Get("error"); errParam != "" {
		http.Error(w, "login failed: "+errParam, http.StatusUnauthorized)
		return
	}
	if r.URL.Query().Get("state") != wantState {
		http.Error(w, "login state mismatch", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return
	}

	token, err := a.oauth2Config.Exchange(r.Context(), code, oauth2.VerifierOption(verifier))
	if err != nil {
		http.Error(w, "token exchange failed", http.StatusUnauthorized)
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "no id_token in token response", http.StatusUnauthorized)
		return
	}
	idToken, err := a.verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		http.Error(w, "id_token verification failed", http.StatusUnauthorized)
		return
	}

	var claims struct {
		Email string `json:"email"`
	}
	_ = idToken.Claims(&claims)

	sid, err := a.sessions.create(&sessionRecord{
		subject:      idToken.Subject,
		email:        claims.Email,
		refreshToken: token.RefreshToken,
		expiry:       idToken.Expiry,
	})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	setCookie(w, sessionCookieName, sid, "/", int(sessionCookieTTL.Seconds()))
	http.Redirect(w, r, returnTo, http.StatusFound)
}

func (a *oidcAuth) logoutHandler(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		a.sessions.delete(cookie.Value)
	}
	clearCookie(w, sessionCookieName, "/")

	redirectURL := strings.TrimRight(a.cfg.BaseURL, "/") + "/"
	if a.endSessionURL == "" {
		http.Redirect(w, r, redirectURL, http.StatusFound)
		return
	}
	u, err := url.Parse(a.endSessionURL)
	if err != nil {
		http.Redirect(w, r, redirectURL, http.StatusFound)
		return
	}
	q := u.Query()
	q.Set("client_id", a.cfg.ClientID)
	q.Set("post_logout_redirect_uri", redirectURL)
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// refresh attempts to silently renew an expired session using its stored
// refresh token, returning the updated record on success.
func (a *oidcAuth) refresh(ctx context.Context, rec sessionRecord) (sessionRecord, bool) {
	if rec.refreshToken == "" {
		return rec, false
	}
	ts := a.oauth2Config.TokenSource(ctx, &oauth2.Token{RefreshToken: rec.refreshToken})
	tok, err := ts.Token()
	if err != nil {
		return rec, false
	}
	rawIDToken, ok := tok.Extra("id_token").(string)
	if !ok {
		return rec, false
	}
	idToken, err := a.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return rec, false
	}
	rec.refreshToken = tok.RefreshToken
	rec.expiry = idToken.Expiry
	return rec, true
}

// requireSession protects the browser-facing site. Missing or expired
// sessions redirect to /auth/login instead of being silently allowed through.
func (a *oidcAuth) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			a.redirectToLogin(w, r)
			return
		}
		rec, ok := a.sessions.get(cookie.Value)
		if !ok {
			a.redirectToLogin(w, r)
			return
		}
		if time.Now().After(rec.expiry) {
			refreshed, ok := a.refresh(r.Context(), rec)
			if !ok {
				a.sessions.delete(cookie.Value)
				a.redirectToLogin(w, r)
				return
			}
			rec = refreshed
			a.sessions.update(cookie.Value, rec)
		}
		next.ServeHTTP(w, r)
	})
}

func (a *oidcAuth) redirectToLogin(w http.ResponseWriter, r *http.Request) {
	dest := "/auth/login?return_to=" + url.QueryEscape(r.URL.RequestURI())
	http.Redirect(w, r, dest, http.StatusFound)
}

// --- MCP bearer-token auth (RFC 9728 / MCP authorization spec) ---

// stringOrSlice unmarshals a JWT claim that may be either a bare string or an
// array of strings (the "aud" claim is defined this way in RFC 7519 §4.1.3).
type stringOrSlice []string

func (s *stringOrSlice) UnmarshalJSON(b []byte) error {
	var single string
	if err := json.Unmarshal(b, &single); err == nil {
		*s = []string{single}
		return nil
	}
	var multi []string
	if err := json.Unmarshal(b, &multi); err != nil {
		return err
	}
	*s = multi
	return nil
}

func (s stringOrSlice) contains(want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}

type accessTokenClaims struct {
	Issuer   string        `json:"iss"`
	Expiry   int64         `json:"exp"`
	Audience stringOrSlice `json:"aud"`
}

// bearerChallenge is the WWW-Authenticate header value returned on 401,
// pointing clients at the RFC 9728 protected-resource metadata document.
func (a *oidcAuth) bearerChallenge() string {
	return fmt.Sprintf(`Bearer resource_metadata=%q`,
		strings.TrimRight(a.cfg.BaseURL, "/")+mcpserver.WellKnownProtectedResourcePath)
}

// requireBearer protects /mcp. MCP clients don't do browser redirects, so
// unlike requireSession this always responds with a 401 challenge rather
// than redirecting.
func (a *oidcAuth) requireBearer(next http.Handler) http.Handler {
	challenge := a.bearerChallenge()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		unauthorized := func() {
			w.Header().Set("WWW-Authenticate", challenge)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		}

		const prefix = "Bearer "
		authz := r.Header.Get("Authorization")
		if !strings.HasPrefix(authz, prefix) {
			unauthorized()
			return
		}

		payload, err := a.keySet.VerifySignature(r.Context(), strings.TrimPrefix(authz, prefix))
		if err != nil {
			unauthorized()
			return
		}
		var claims accessTokenClaims
		if err := json.Unmarshal(payload, &claims); err != nil {
			unauthorized()
			return
		}
		if claims.Issuer != a.cfg.Issuer {
			unauthorized()
			return
		}
		if time.Now().Unix() >= claims.Expiry {
			unauthorized()
			return
		}
		if a.cfg.MCPAudience != "" && !claims.Audience.contains(a.cfg.MCPAudience) {
			unauthorized()
			return
		}
		next.ServeHTTP(w, r)
	})
}

// protectedResourceMetadataPath returns the well-known path that must serve
// this deployment's RFC 9728 metadata (derived from the resource identifier).
func (a *oidcAuth) protectedResourceMetadataPath() string {
	return mcpserver.ProtectedResourceMetadataPath(strings.TrimRight(a.cfg.BaseURL, "/"))
}

func (a *oidcAuth) protectedResourceMetadataHandler() http.Handler {
	return mcpserver.NewProtectedResourceMetadataHandler(mcpserver.ProtectedResourceMetadataConfig{
		Resource:             strings.TrimRight(a.cfg.BaseURL, "/"),
		AuthorizationServers: []string{a.cfg.Issuer},
	})
}
