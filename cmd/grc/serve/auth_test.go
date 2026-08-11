package serve

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// --- Fake IdP test helpers ---

func mustRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating RSA key: %v", err)
	}
	return key
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func signRS256(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": kid}
	hb, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("marshaling header: %v", err)
	}
	cb, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshaling claims: %v", err)
	}
	signingInput := b64(hb) + "." + b64(cb)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("signing token: %v", err)
	}
	return signingInput + "." + b64(sig)
}

func jwkFor(key *rsa.PrivateKey, kid string) map[string]any {
	pub := key.PublicKey
	return map[string]any{
		"kty": "RSA",
		"kid": kid,
		"use": "sig",
		"alg": "RS256",
		"n":   b64(pub.N.Bytes()),
		"e":   b64(big.NewInt(int64(pub.E)).Bytes()),
	}
}

const testKID = "test-key-1"

// testIdP is a minimal fake OIDC provider: discovery + JWKS + scriptable
// refresh_token and authorization_code grants on /token.
type testIdP struct {
	srv *httptest.Server
	key *rsa.PrivateKey

	// refreshResponses maps a refresh_token value to the id_token claims to
	// mint on success; a missing entry causes the /token handler to 400.
	refreshResponses map[string]map[string]any
	// authCodeResponses maps an authorization code to the id_token claims to
	// mint on success; a missing entry causes the /token handler to 400.
	authCodeResponses map[string]map[string]any
}

func newTestIdP(t *testing.T) *testIdP {
	t.Helper()
	idp := &testIdP{
		key:               mustRSAKey(t),
		refreshResponses:  make(map[string]map[string]any),
		authCodeResponses: make(map[string]map[string]any),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 idp.srv.URL,
			"authorization_endpoint": idp.srv.URL + "/auth",
			"token_endpoint":         idp.srv.URL + "/token",
			"jwks_uri":               idp.srv.URL + "/jwks",
			"end_session_endpoint":   idp.srv.URL + "/logout",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{jwkFor(idp.key, testKID)}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		var claims map[string]any
		var ok bool
		var newRefreshToken string
		switch r.PostForm.Get("grant_type") {
		case "refresh_token":
			rt := r.PostForm.Get("refresh_token")
			claims, ok = idp.refreshResponses[rt]
			newRefreshToken = "refreshed-" + rt
		case "authorization_code":
			claims, ok = idp.authCodeResponses[r.PostForm.Get("code")]
			newRefreshToken = "issued-refresh-token"
		default:
			http.Error(w, "unsupported grant_type in test IdP", http.StatusBadRequest)
			return
		}
		if !ok {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		idToken := signRS256(t, idp.key, testKID, claims)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "test-access-token",
			"refresh_token": newRefreshToken,
			"id_token":      idToken,
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	})
	idp.srv = httptest.NewServer(mux)
	t.Cleanup(idp.srv.Close)
	return idp
}

func (idp *testIdP) issuer() string { return idp.srv.URL }

func newTestAuth(t *testing.T, idp *testIdP, mcpAudience string) *oidcAuth {
	t.Helper()
	auth, err := newOIDCAuth(context.Background(), authConfig{
		Issuer:       idp.issuer(),
		ClientID:     "grc-site",
		ClientSecret: "test-secret",
		BaseURL:      "https://grc.example.org",
		MCPAudience:  mcpAudience,
	})
	if err != nil {
		t.Fatalf("newOIDCAuth: %v", err)
	}
	return auth
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// --- requireBearer ---

func TestRequireBearer(t *testing.T) {
	idp := newTestIdP(t)
	auth := newTestAuth(t, idp, "grc-mcp")
	handler := auth.requireBearer(okHandler())

	validClaims := func() map[string]any {
		return map[string]any{
			"iss": idp.issuer(),
			"aud": "grc-mcp",
			"sub": "user-1",
			"exp": time.Now().Add(time.Hour).Unix(),
		}
	}

	tests := []struct {
		name       string
		authHeader func() string
		wantStatus int
	}{
		{
			name:       "missing header",
			authHeader: func() string { return "" },
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "non-bearer scheme",
			authHeader: func() string { return "Basic dXNlcjpwYXNz" },
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "malformed token",
			authHeader: func() string { return "Bearer not-a-jwt" },
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "valid token",
			authHeader: func() string {
				return "Bearer " + signRS256(t, idp.key, testKID, validClaims())
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "expired token",
			authHeader: func() string {
				claims := validClaims()
				claims["exp"] = time.Now().Add(-time.Hour).Unix()
				return "Bearer " + signRS256(t, idp.key, testKID, claims)
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "wrong issuer",
			authHeader: func() string {
				claims := validClaims()
				claims["iss"] = "https://not-our-idp.example.org"
				return "Bearer " + signRS256(t, idp.key, testKID, claims)
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "wrong audience",
			authHeader: func() string {
				claims := validClaims()
				claims["aud"] = "some-other-service"
				return "Bearer " + signRS256(t, idp.key, testKID, claims)
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "unknown signing key",
			authHeader: func() string {
				otherKey := mustRSAKey(t)
				return "Bearer " + signRS256(t, otherKey, testKID, validClaims())
			},
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
			if h := tt.authHeader(); h != "" {
				req.Header.Set("Authorization", h)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus == http.StatusUnauthorized {
				got := rec.Header().Get("WWW-Authenticate")
				want := `Bearer resource_metadata="https://grc.example.org/.well-known/oauth-protected-resource"`
				if got != want {
					t.Errorf("WWW-Authenticate = %q, want %q", got, want)
				}
			}
		})
	}
}

// --- requireSession ---

func TestRequireSession_NoCookie(t *testing.T) {
	idp := newTestIdP(t)
	auth := newTestAuth(t, idp, "")
	handler := auth.requireSession(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/controls/foo", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parsing Location: %v", err)
	}
	if loc.Path != "/auth/login" {
		t.Errorf("redirect path = %q, want /auth/login", loc.Path)
	}
	if got := loc.Query().Get("return_to"); got != "/controls/foo" {
		t.Errorf("return_to = %q, want /controls/foo", got)
	}
}

func TestRequireSession_UnknownCookie(t *testing.T) {
	idp := newTestIdP(t)
	auth := newTestAuth(t, idp, "")
	handler := auth.requireSession(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "does-not-exist"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
}

func TestRequireSession_Valid(t *testing.T) {
	idp := newTestIdP(t)
	auth := newTestAuth(t, idp, "")
	handler := auth.requireSession(okHandler())

	sid, err := auth.sessions.create(&sessionRecord{
		subject: "user-1",
		expiry:  time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sid})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequireSession_ExpiredNoRefreshToken(t *testing.T) {
	idp := newTestIdP(t)
	auth := newTestAuth(t, idp, "")
	handler := auth.requireSession(okHandler())

	sid, err := auth.sessions.create(&sessionRecord{
		subject: "user-1",
		expiry:  time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sid})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if _, ok := auth.sessions.get(sid); ok {
		t.Error("expired session with no refresh token should have been deleted")
	}
}

func TestRequireSession_ExpiredRefreshFails(t *testing.T) {
	idp := newTestIdP(t)
	auth := newTestAuth(t, idp, "")
	handler := auth.requireSession(okHandler())

	sid, err := auth.sessions.create(&sessionRecord{
		subject:      "user-1",
		refreshToken: "bad-rt",
		expiry:       time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sid})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if _, ok := auth.sessions.get(sid); ok {
		t.Error("session with failed refresh should have been deleted")
	}
}

func TestRequireSession_ExpiredRefreshSucceeds(t *testing.T) {
	idp := newTestIdP(t)
	auth := newTestAuth(t, idp, "")
	handler := auth.requireSession(okHandler())

	idp.refreshResponses["good-rt"] = map[string]any{
		"iss": idp.issuer(),
		"aud": "grc-site",
		"sub": "user-1",
		"exp": time.Now().Add(time.Hour).Unix(),
	}

	sid, err := auth.sessions.create(&sessionRecord{
		subject:      "user-1",
		refreshToken: "good-rt",
		expiry:       time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sid})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusOK, rec.Body.String())
	}
	rec2, ok := auth.sessions.get(sid)
	if !ok {
		t.Fatal("session should still exist after successful refresh")
	}
	if !rec2.expiry.After(time.Now()) {
		t.Errorf("session expiry not extended: %v", rec2.expiry)
	}
	if rec2.refreshToken != "refreshed-good-rt" {
		t.Errorf("refresh token not rotated: %q", rec2.refreshToken)
	}
}

// --- callback handler ---

// pkceCookieValue mimics the cookie loginHandler would have set, without
// going through the full redirect round trip — lets tests control the
// encoded state/verifier/returnTo directly.
func pkceCookieValue(state, verifier, returnTo string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(state + "|" + verifier + "|" + returnTo))
}

func TestCallbackHandler_Success(t *testing.T) {
	idp := newTestIdP(t)
	auth := newTestAuth(t, idp, "")

	idp.authCodeResponses["good-code"] = map[string]any{
		"iss":   idp.issuer(),
		"aud":   "grc-site",
		"sub":   "user-1",
		"email": "user1@example.org",
		"exp":   time.Now().Add(time.Hour).Unix(),
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=state-1&code=good-code", nil)
	req.AddCookie(&http.Cookie{Name: pkceCookieName, Value: pkceCookieValue("state-1", "verifier-1", "/dashboard")})
	rec := httptest.NewRecorder()
	auth.callbackHandler(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusFound, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/dashboard" {
		t.Errorf("redirect = %q, want /dashboard", loc)
	}

	var sid string
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			sid = c.Value
		}
	}
	if sid == "" {
		t.Fatal("expected a session cookie to be set")
	}
	rec2, ok := auth.sessions.get(sid)
	if !ok {
		t.Fatal("session should exist in the store")
	}
	if rec2.subject != "user-1" {
		t.Errorf("subject = %q, want user-1", rec2.subject)
	}
	if rec2.email != "user1@example.org" {
		t.Errorf("email = %q, want user1@example.org", rec2.email)
	}
}

// Regression test for the SonarCloud-flagged open redirect: the pkce cookie
// is unsigned, so an attacker who never went through /auth/login could send
// a forged cookie carrying an arbitrary returnTo. callbackHandler must
// re-validate it (safeReturnTo), not just trust what loginHandler wrote.
func TestCallbackHandler_RejectsForgedOpenRedirect(t *testing.T) {
	idp := newTestIdP(t)
	auth := newTestAuth(t, idp, "")

	idp.authCodeResponses["good-code"] = map[string]any{
		"iss": idp.issuer(),
		"aud": "grc-site",
		"sub": "user-1",
		"exp": time.Now().Add(time.Hour).Unix(),
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=state-1&code=good-code", nil)
	req.AddCookie(&http.Cookie{
		Name:  pkceCookieName,
		Value: pkceCookieValue("state-1", "verifier-1", "https://evil.example.com/phish"),
	})
	rec := httptest.NewRecorder()
	auth.callbackHandler(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Errorf("redirect = %q, want / (forged returnTo must be rejected)", loc)
	}
}

func TestCallbackHandler_NoCookie(t *testing.T) {
	idp := newTestIdP(t)
	auth := newTestAuth(t, idp, "")

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=state-1&code=good-code", nil)
	rec := httptest.NewRecorder()
	auth.callbackHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCallbackHandler_StateMismatch(t *testing.T) {
	idp := newTestIdP(t)
	auth := newTestAuth(t, idp, "")

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=wrong-state&code=good-code", nil)
	req.AddCookie(&http.Cookie{Name: pkceCookieName, Value: pkceCookieValue("state-1", "verifier-1", "/")})
	rec := httptest.NewRecorder()
	auth.callbackHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCallbackHandler_IdPError(t *testing.T) {
	idp := newTestIdP(t)
	auth := newTestAuth(t, idp, "")

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=state-1&error=access_denied", nil)
	req.AddCookie(&http.Cookie{Name: pkceCookieName, Value: pkceCookieValue("state-1", "verifier-1", "/")})
	rec := httptest.NewRecorder()
	auth.callbackHandler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestCallbackHandler_ExchangeFails(t *testing.T) {
	idp := newTestIdP(t)
	auth := newTestAuth(t, idp, "")
	// no authCodeResponses entry for "bad-code" -> token endpoint 400s

	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=state-1&code=bad-code", nil)
	req.AddCookie(&http.Cookie{Name: pkceCookieName, Value: pkceCookieValue("state-1", "verifier-1", "/")})
	rec := httptest.NewRecorder()
	auth.callbackHandler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// --- login/logout smoke tests ---

func TestLoginHandler_RedirectsToProvider(t *testing.T) {
	idp := newTestIdP(t)
	auth := newTestAuth(t, idp, "")

	req := httptest.NewRequest(http.MethodGet, "/auth/login?return_to=/foo", nil)
	rec := httptest.NewRecorder()
	auth.loginHandler(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if loc := rec.Header().Get("Location"); loc == "" {
		t.Fatal("expected a redirect Location header")
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != pkceCookieName {
		t.Fatalf("expected a %s cookie to be set, got %v", pkceCookieName, cookies)
	}
}

func TestLogoutHandler_ClearsSessionAndRedirects(t *testing.T) {
	idp := newTestIdP(t)
	auth := newTestAuth(t, idp, "")

	sid, err := auth.sessions.create(&sessionRecord{subject: "user-1", expiry: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sid})
	rec := httptest.NewRecorder()
	auth.logoutHandler(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	if _, ok := auth.sessions.get(sid); ok {
		t.Error("session should be deleted after logout")
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parsing Location: %v", err)
	}
	if loc.Host != mustParseHost(t, idp.issuer()) {
		t.Errorf("logout should redirect to the provider's end_session_endpoint, got %s", loc)
	}
}

func mustParseHost(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parsing URL: %v", err)
	}
	return u.Host
}

// --- RFC 9728 protected-resource metadata ---

func TestProtectedResourceMetadata(t *testing.T) {
	idp := newTestIdP(t)
	auth := newTestAuth(t, idp, "grc-mcp")

	if got, want := auth.protectedResourceMetadataPath(), "/.well-known/oauth-protected-resource"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}

	req := httptest.NewRequest(http.MethodGet, auth.protectedResourceMetadataPath(), nil)
	rec := httptest.NewRecorder()
	auth.protectedResourceMetadataHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.Resource != "https://grc.example.org" {
		t.Errorf("resource = %q, want https://grc.example.org", body.Resource)
	}
	if len(body.AuthorizationServers) != 1 || body.AuthorizationServers[0] != idp.issuer() {
		t.Errorf("authorization_servers = %v, want [%s]", body.AuthorizationServers, idp.issuer())
	}
}
