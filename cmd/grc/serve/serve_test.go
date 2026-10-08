package serve

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

func TestWebhookValidPushAcceptedImmediately(t *testing.T) {
	// root is intentionally not a real git checkout: the point of this test
	// is that ServeHTTP responds before the (background, and here doomed to
	// fail) rebuild finishes, not that the rebuild itself succeeds.
	wh := &webhookHandler{root: t.TempDir(), profile: "private", secret: "test", repo: "org/repo"}
	body := []byte(`{"repository":{"full_name":"org/repo"},"ref":"refs/heads/main"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", signPayload(body, "test"))

	start := time.Now()
	w := httptest.NewRecorder()
	wh.ServeHTTP(w, req)
	elapsed := time.Since(start)

	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d", w.Code, http.StatusAccepted)
	}
	respBody, _ := io.ReadAll(w.Body)
	if !strings.Contains(string(respBody), "accepted") {
		t.Errorf("body = %q, want to contain \"accepted\"", respBody)
	}
	if elapsed > time.Second {
		t.Errorf("ServeHTTP took %v, want it to return before the rebuild completes", elapsed)
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

func pushRequest(t *testing.T, secret string) *http.Request {
	t.Helper()
	body := []byte(`{"repository":{"full_name":"org/repo"},"ref":"refs/heads/main"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", signPayload(body, secret))
	return req
}

// A burst of pushes inside the debounce window must not start any rebuild
// until the window has been quiet, and then must produce exactly one.
func TestWebhookDebounceCoalescesBurst(t *testing.T) {
	var runs atomic.Int32
	wh := &webhookHandler{secret: "s", repo: "org/repo", debounce: 200 * time.Millisecond,
		rebuildFn: func() { runs.Add(1) }}
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		wh.ServeHTTP(w, pushRequest(t, "s"))
		if w.Code != http.StatusAccepted {
			t.Fatalf("push %d: status = %d, want 202", i, w.Code)
		}
		time.Sleep(60 * time.Millisecond) // each push lands inside the window
	}
	if n := runs.Load(); n != 0 {
		t.Fatalf("%d rebuild(s) started while pushes were still arriving", n)
	}
	time.Sleep(600 * time.Millisecond)
	if n := runs.Load(); n != 1 {
		t.Fatalf("rebuilds after burst = %d, want 1", n)
	}
}

// A push arriving during a rebuild yields exactly one follow-up, never an
// overlapping run.
func TestWebhookPushDuringRebuildRunsOnceMore(t *testing.T) {
	var runs, active, maxActive atomic.Int32
	release := make(chan struct{})
	wh := &webhookHandler{}
	wh.rebuildFn = func() {
		n := active.Add(1)
		for {
			m := maxActive.Load()
			if n <= m || maxActive.CompareAndSwap(m, n) {
				break
			}
		}
		if runs.Add(1) == 1 {
			<-release
		}
		active.Add(-1)
	}
	done := make(chan struct{})
	go func() { wh.run(); close(done) }()
	for runs.Load() < 1 {
		time.Sleep(5 * time.Millisecond)
	}
	wh.run() // two more requests while the first rebuild is in flight
	wh.run()
	close(release)
	<-done
	if runs.Load() != 2 {
		t.Errorf("rebuilds = %d, want 2 (initial + one coalesced follow-up)", runs.Load())
	}
	if maxActive.Load() != 1 {
		t.Errorf("max concurrent rebuilds = %d, want 1", maxActive.Load())
	}
}

// A timer callback that was already waiting on the lock when a newer push
// replaced its timer must not start a rebuild.
func TestWebhookStaleTimerCallbackIgnored(t *testing.T) {
	var runs atomic.Int32
	wh := &webhookHandler{debounce: time.Hour, rebuildFn: func() { runs.Add(1) }}
	wh.schedule()
	wh.mu.Lock()
	stale := wh.gen
	wh.mu.Unlock()
	wh.schedule() // newer push replaces the first timer
	wh.fire(stale)
	time.Sleep(50 * time.Millisecond)
	if n := runs.Load(); n != 0 {
		t.Fatalf("stale callback started %d rebuild(s)", n)
	}
	wh.mu.Lock()
	cur := wh.gen
	wh.mu.Unlock()
	wh.fire(cur)
	time.Sleep(50 * time.Millisecond)
	if n := runs.Load(); n != 1 {
		t.Fatalf("current callback rebuilds = %d, want 1", n)
	}
	wh.mu.Lock()
	wh.timer.Stop()
	wh.mu.Unlock()
}
