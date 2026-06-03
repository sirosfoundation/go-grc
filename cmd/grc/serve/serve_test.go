package serve

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVerifySignatureValid(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main"}`)
	secret := "test-secret"

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !verifySignature(body, sig, secret) {
		t.Error("valid signature rejected")
	}
}

func TestVerifySignatureInvalid(t *testing.T) {
	body := []byte(`{"ref":"refs/heads/main"}`)
	if verifySignature(body, "sha256=deadbeef", "secret") {
		t.Error("invalid signature accepted")
	}
}

func TestVerifySignatureNoPrefix(t *testing.T) {
	if verifySignature([]byte("body"), "invalid", "secret") {
		t.Error("signature without sha256= prefix accepted")
	}
}

func TestVerifySignatureBadHex(t *testing.T) {
	if verifySignature([]byte("body"), "sha256=not-hex!!!", "secret") {
		t.Error("non-hex signature accepted")
	}
}

func signPayload(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestWebhookPing(t *testing.T) {
	wh := &webhookHandler{
		root:    "/tmp",
		profile: "private",
		secret:  "test",
		repo:    "org/repo",
	}

	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "ping")
	req.Header.Set("X-Hub-Signature-256", signPayload(body, "test"))

	w := httptest.NewRecorder()
	wh.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("ping: status = %d, want 200", w.Code)
	}
	respBody, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(respBody), "pong") {
		t.Errorf("ping: body = %q, want pong", respBody)
	}
}

func TestWebhookMethodNotAllowed(t *testing.T) {
	wh := &webhookHandler{secret: "test"}
	req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
	w := httptest.NewRecorder()
	wh.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: status = %d, want 405", w.Code)
	}
}

func TestWebhookInvalidSignature(t *testing.T) {
	wh := &webhookHandler{secret: "test"}
	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", "sha256=badbadbad")
	w := httptest.NewRecorder()
	wh.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("bad sig: status = %d, want 403", w.Code)
	}
}

func TestWebhookIgnoredEvent(t *testing.T) {
	wh := &webhookHandler{secret: "test", repo: "org/repo"}
	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-Hub-Signature-256", signPayload(body, "test"))
	w := httptest.NewRecorder()
	wh.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("ignored event: status = %d, want 200", w.Code)
	}
	respBody, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(respBody), "ignored") {
		t.Errorf("ignored event: body = %q", respBody)
	}
}

func TestWebhookRepoMismatch(t *testing.T) {
	wh := &webhookHandler{secret: "test", repo: "org/repo"}
	body := []byte(`{"repository":{"full_name":"other/repo"},"ref":"refs/heads/main"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", signPayload(body, "test"))
	w := httptest.NewRecorder()
	wh.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("repo mismatch: status = %d, want 200", w.Code)
	}
	respBody, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(respBody), "repo mismatch") {
		t.Errorf("repo mismatch: body = %q", respBody)
	}
}

func TestWebhookNonDefaultBranch(t *testing.T) {
	wh := &webhookHandler{secret: "test", repo: "org/repo"}
	body := []byte(`{"repository":{"full_name":"org/repo"},"ref":"refs/heads/feature"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", signPayload(body, "test"))
	w := httptest.NewRecorder()
	wh.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("non-default branch: status = %d, want 200", w.Code)
	}
	respBody, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(respBody), "non-default branch") {
		t.Errorf("non-default branch: body = %q", respBody)
	}
}

func TestHealthEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("health: status = %d, want 200", w.Code)
	}
}
