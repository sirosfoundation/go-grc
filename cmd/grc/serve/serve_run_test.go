package serve

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sirosfoundation/go-grc/pkg/config"
)

func clearServeEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"GRC_WEBHOOK_SECRET", "GRC_OIDC_ISSUER", "GRC_OIDC_CLIENT_ID", "GRC_OIDC_CLIENT_SECRET", "GRC_BASE_URL", "GRC_MCP_AUDIENCE", "GRC_MCP_SCOPES"} {
		t.Setenv(k, "")
	}
}

func TestNewCommandFlagsAndDefaults(t *testing.T) {
	cmd := NewCommand()
	if cmd.Use != "serve" {
		t.Errorf("Use = %q", cmd.Use)
	}
	want := map[string]string{
		"profile":          "private",
		"addr":             ":8080",
		"webhook":          "false",
		"rebuild-interval": "24h0m0s",
		"mcp-scopes":       defaultMCPScopes,
		"auth-issuer":      "",
	}
	for name, def := range want {
		f := cmd.Flags().Lookup(name)
		if f == nil {
			t.Errorf("flag --%s missing", name)
			continue
		}
		if f.DefValue != def {
			t.Errorf("--%s default = %q, want %q", name, f.DefValue, def)
		}
	}
	for _, name := range []string{"webhook-secret", "auth-client-id", "auth-client-secret", "base-url", "mcp-audience"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag --%s missing", name)
		}
	}
}

func TestNewCommandRunEFailsWithoutWebhookSecret(t *testing.T) {
	clearServeEnv(t)
	cmd := NewCommand()
	cmd.Flags().String("root", t.TempDir(), "")
	if err := cmd.Flags().Parse([]string{"--webhook"}); err != nil {
		t.Fatal(err)
	}
	err := cmd.RunE(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--webhook-secret or GRC_WEBHOOK_SECRET is required") {
		t.Errorf("err = %v", err)
	}
}

func TestResolveServeSecrets(t *testing.T) {
	clearServeEnv(t)

	if _, _, err := resolveServeSecrets("", true, authConfig{}); err == nil {
		t.Error("webhook enabled without secret must fail")
	}

	secret, cfg, err := resolveServeSecrets("flag-secret", true, authConfig{Issuer: "https://flag"})
	if err != nil || secret != "flag-secret" || cfg.Issuer != "https://flag" {
		t.Errorf("flags must win: %q %+v %v", secret, cfg, err)
	}

	t.Setenv("GRC_WEBHOOK_SECRET", "env-secret")
	t.Setenv("GRC_OIDC_ISSUER", "https://env-issuer")
	t.Setenv("GRC_OIDC_CLIENT_ID", "env-id")
	t.Setenv("GRC_OIDC_CLIENT_SECRET", "env-client-secret")
	t.Setenv("GRC_BASE_URL", "https://grc.example")
	t.Setenv("GRC_MCP_AUDIENCE", "env-aud")
	t.Setenv("GRC_MCP_SCOPES", "openid,custom")

	secret, cfg, err = resolveServeSecrets("", true, authConfig{MCPScopes: splitScopes(defaultMCPScopes)})
	if err != nil {
		t.Fatal(err)
	}
	if secret != "env-secret" || cfg.Issuer != "https://env-issuer" || cfg.ClientID != "env-id" ||
		cfg.ClientSecret != "env-client-secret" || cfg.BaseURL != "https://grc.example" || cfg.MCPAudience != "env-aud" {
		t.Errorf("env not applied: %q %+v", secret, cfg)
	}
	if strings.Join(cfg.MCPScopes, "|") != "openid|custom" {
		t.Errorf("scopes = %v", cfg.MCPScopes)
	}

	// An explicitly set scope list is not overridden by the env var.
	_, cfg, _ = resolveServeSecrets("x", false, authConfig{MCPScopes: []string{"only"}})
	if strings.Join(cfg.MCPScopes, "|") != "only" {
		t.Errorf("explicit scopes overridden: %v", cfg.MCPScopes)
	}
}

func TestSplitAndSameScopes(t *testing.T) {
	if got := splitScopes(" \t,\n"); got != nil {
		t.Errorf("blank = %v", got)
	}
	if got := splitScopes("a,b c\td\ne"); strings.Join(got, "|") != "a|b|c|d|e" {
		t.Errorf("got %v", got)
	}
	if sameScopes([]string{"a"}, "a b") {
		t.Error("different lengths must differ")
	}
	if sameScopes([]string{"a", "c"}, "a b") {
		t.Error("different values must differ")
	}
	if !sameScopes([]string{"a", "b"}, "a,b") {
		t.Error("equal lists must match")
	}
}

func TestMaybeInitAuthDisabledAndFailing(t *testing.T) {
	a, err := maybeInitAuth(authConfig{})
	if a != nil || err != nil {
		t.Errorf("no issuer: %v %v", a, err)
	}
	// Issuer set but not a valid, reachable provider: initialization errors.
	_, err = maybeInitAuth(authConfig{Issuer: "http://127.0.0.1:1", ClientID: "c", BaseURL: "https://x"})
	if err == nil || !strings.Contains(err.Error(), "initializing auth") {
		t.Errorf("err = %v", err)
	}
}

func get(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestBuildMuxRoutes(t *testing.T) {
	root := fixtureRoot(t)
	cfg, err := config.New(root)
	if err != nil {
		t.Fatal(err)
	}
	siteDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(siteDir, "index.html"), []byte("<h1>site home</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}

	mux := buildMux(muxConfig{root: root, profile: "public", cfg: cfg, serveDir: siteDir})
	for path, want := range map[string]string{"/health": "healthy", "/ready": "ready"} {
		rec := get(mux, http.MethodGet, path)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), want) || rec.Header().Get("Content-Type") != "application/json" {
			t.Errorf("%s: %d %q %v", path, rec.Code, rec.Body.String(), rec.Header())
		}
	}
	if rec := get(mux, http.MethodGet, "/"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "site home") {
		t.Errorf("site: %d %q", rec.Code, rec.Body.String())
	}
	// Public profile has no MCP data: /mcp is a 404, and webhook is not mounted.
	if rec := get(mux, http.MethodGet, "/mcp"); rec.Code != http.StatusNotFound {
		t.Errorf("/mcp without data: %d", rec.Code)
	}
	if rec := get(mux, http.MethodPost, "/webhook"); rec.Code != http.StatusNotFound {
		t.Errorf("/webhook should not be mounted: %d", rec.Code)
	}

	// Webhook enabled: wrong method is rejected by the handler itself.
	mux = buildMux(muxConfig{root: root, profile: "private", cfg: cfg, serveDir: siteDir, enableWebhook: true, webhookSecret: "s", mcpData: loadedData(t)})
	if rec := get(mux, http.MethodGet, "/webhook"); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("/webhook GET: %d", rec.Code)
	}
	// With MCP data, /mcp is served by the MCP handler rather than 404.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, newPostRequest(t, initializeBody()))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "grc-compliance") {
		t.Errorf("/mcp should be mounted with MCP data: %d %s", rec.Code, rec.Body.String())
	}
}

func TestMCPAndSiteMuxHandlerPassthrough(t *testing.T) {
	if rec := get(mcpMuxHandler(nil, nil), http.MethodGet, "/mcp"); rec.Code != http.StatusNotFound {
		t.Errorf("nil data: %d", rec.Code)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := get(siteMuxHandler(dir, nil), http.MethodGet, "/a.txt")
	if rec.Code != 200 || rec.Body.String() != "hello" {
		t.Errorf("site file: %d %q", rec.Code, rec.Body.String())
	}
	if rec := get(siteMuxHandler(dir, nil), http.MethodGet, "/missing.txt"); rec.Code != http.StatusNotFound {
		t.Errorf("missing: %d", rec.Code)
	}
}

func TestResolveServeDir(t *testing.T) {
	root := fixtureRoot(t)
	cfg, err := config.New(root)
	if err != nil {
		t.Fatal(err)
	}
	// No build dir: fall back to the rendered markdown directory.
	if got := resolveServeDir(cfg); got != cfg.SiteDir {
		t.Errorf("fallback = %q, want %q", got, cfg.SiteDir)
	}
	buildDir := filepath.Join(filepath.Dir(cfg.SiteDir), "build")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := resolveServeDir(cfg); got != buildDir {
		t.Errorf("build = %q, want %q", got, buildDir)
	}
}

// fakePnpm puts a fake pnpm executable on PATH that records its invocations.
func fakePnpm(t *testing.T, exitCode int) (logFile string) {
	t.Helper()
	bin := t.TempDir()
	logFile = filepath.Join(bin, "calls.log")
	script := fmt.Sprintf("#!/bin/sh\necho \"$@\" >> %q\nexit %d\n", logFile, exitCode)
	if err := os.WriteFile(filepath.Join(bin, "pnpm"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logFile
}

func TestBuildSite(t *testing.T) {
	t.Run("no pnpm", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if err := buildSite(fixtureRoot(t)); err != nil {
			t.Errorf("missing pnpm must be tolerated: %v", err)
		}
	})
	t.Run("no package.json", func(t *testing.T) {
		logFile := fakePnpm(t, 0)
		if err := buildSite(fixtureRoot(t)); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(logFile); err == nil {
			t.Error("pnpm must not run without package.json")
		}
	})
	t.Run("success", func(t *testing.T) {
		logFile := fakePnpm(t, 0)
		root := fixtureRoot(t)
		if err := os.WriteFile(filepath.Join(root, "site", "package.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := buildSite(root); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(logFile)
		if got := strings.TrimSpace(string(b)); got != "install --frozen-lockfile\nexec docusaurus build" {
			t.Errorf("pnpm calls = %q", got)
		}
	})
	t.Run("pnpm failure", func(t *testing.T) {
		fakePnpm(t, 1)
		root := fixtureRoot(t)
		if err := os.WriteFile(filepath.Join(root, "site", "package.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := buildSite(root); err == nil || !strings.Contains(err.Error(), "pnpm install") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("bad config", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".grc.yaml"), []byte("project: [bad"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := buildSite(root); err == nil || !strings.Contains(err.Error(), "loading config") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestRenderAndBuildAndPrepareSite(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no pnpm: skip the Docusaurus build
	root := fixtureRoot(t)
	if err := renderAndBuild(root, "private"); err != nil {
		t.Fatalf("renderAndBuild: %v", err)
	}

	cfg, data, err := prepareSite(root, "private")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Project.Repo != "test/compliance" || data == nil {
		t.Fatalf("cfg=%+v data=%v", cfg.Project, data)
	}
	if len(data.catalog.Controls) != 5 {
		t.Errorf("MCP data not loaded: %d controls", len(data.catalog.Controls))
	}

	_, data, err = prepareSite(root, "public")
	if err != nil || data != nil {
		t.Errorf("public profile must have no MCP data: %v %v", data, err)
	}

	bad := t.TempDir()
	if err := os.WriteFile(filepath.Join(bad, ".grc.yaml"), []byte("project: [bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := prepareSite(bad, "private"); err == nil || !strings.Contains(err.Error(), "loading config") {
		t.Errorf("err = %v", err)
	}
	if err := renderAndBuild(bad, "private"); err == nil || !strings.Contains(err.Error(), "render") {
		t.Errorf("renderAndBuild err = %v", err)
	}
}

func TestRunScheduledRebuildReloadsData(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := fixtureRoot(t)
	data := newComplianceData(root, "private")
	runScheduledRebuild(root, "private", data)
	if data.catalog == nil || len(data.catalog.Controls) != 5 {
		t.Errorf("data not reloaded after scheduled rebuild")
	}
	// Failure path: broken config must not panic and must not touch data.
	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, ".grc.yaml"), []byte("project: [bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	fresh := newComplianceData(broken, "private")
	runScheduledRebuild(broken, "private", fresh)
	if fresh.catalog != nil {
		t.Error("failed rebuild must not load data")
	}
	runScheduledRebuild(root, "private", nil)
}

func TestStartRebuildTicker(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	// The ticker goroutine may still be mid-render when the test returns, so
	// the directory is removed best-effort instead of via t.TempDir (whose
	// cleanup would fail the test on a concurrent write).
	dir, err := os.MkdirTemp("", "grc-ticker-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	root := populateFixture(t, dir)

	stop := make(chan struct{})
	defer close(stop)
	data := newComplianceData(root, "private")
	startRebuildTicker(stop, 0, root, "private", data) // no-op
	time.Sleep(50 * time.Millisecond)
	if data.catalog != nil {
		t.Fatal("interval 0 must not schedule rebuilds")
	}

	startRebuildTicker(stop, 20*time.Millisecond, root, "private", data)
	deadline := time.Now().Add(10 * time.Second)
	for {
		data.mu.RLock()
		loaded := data.catalog != nil
		data.mu.RUnlock()
		if loaded {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("ticker never reloaded the data")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestServeUntilShutdownListenError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	stop := make(chan struct{})
	err = serveUntilShutdown(&http.Server{Addr: ln.Addr().String(), Handler: http.NewServeMux()}, stop)
	if err == nil {
		t.Fatal("expected listen error for address in use")
	}
	select {
	case <-stop:
	default:
		t.Error("stop channel must be closed on error")
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func waitForHealth(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/health")
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == 200 && strings.Contains(string(b), "healthy") {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("server never became healthy")
}

func TestServeUntilShutdownGracefulOnSignal(t *testing.T) {
	// Intercept SIGTERM in the test too, so a signal can never kill the test binary.
	guard := make(chan os.Signal, 1)
	signal.Notify(guard, syscall.SIGTERM)
	defer signal.Stop(guard)

	addr := freeAddr(t)
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- serveUntilShutdown(&http.Server{Addr: addr, Handler: http.NewServeMux()}, stop)
	}()
	waitForHealthOrNotFound(t, addr)
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("graceful shutdown returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("did not shut down on SIGTERM")
	}
	select {
	case <-stop:
	default:
		t.Error("stop channel must be closed on shutdown")
	}
}

// waitForHealthOrNotFound waits until the listener accepts HTTP requests.
func waitForHealthOrNotFound(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/")
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("listener never came up")
}

func TestRunServeEndToEnd(t *testing.T) {
	clearServeEnv(t)
	t.Setenv("PATH", t.TempDir())
	guard := make(chan os.Signal, 1)
	signal.Notify(guard, syscall.SIGTERM)
	defer signal.Stop(guard)

	root := fixtureRoot(t)
	addr := freeAddr(t)
	done := make(chan error, 1)
	go func() {
		done <- runServe(root, "private", addr, "", false, 0, authConfig{})
	}()
	waitForHealth(t, addr)

	resp, err := http.Get("http://" + addr + "/ready")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("/ready = %d", resp.StatusCode)
	}
	// The rendered site is served from site/docs when no build exists.
	resp, err = http.Get("http://" + addr + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		t.Error("/mcp must be mounted for the private profile")
	}

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("runServe returned %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("runServe did not exit on SIGTERM")
	}
}

func TestRunServeErrors(t *testing.T) {
	clearServeEnv(t)
	if err := runServe(t.TempDir(), "private", "127.0.0.1:0", "", true, 0, authConfig{}); err == nil || !strings.Contains(err.Error(), "required when --webhook") {
		t.Errorf("secret err = %v", err)
	}
	err := runServe(t.TempDir(), "private", "127.0.0.1:0", "", false, 0, authConfig{Issuer: "http://127.0.0.1:1", BaseURL: "https://x"})
	if err == nil || !strings.Contains(err.Error(), "initializing auth") {
		t.Errorf("auth err = %v", err)
	}
	bad := t.TempDir()
	if err := os.WriteFile(filepath.Join(bad, ".grc.yaml"), []byte("project: [bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runServe(bad, "private", "127.0.0.1:0", "", false, 0, authConfig{}); err == nil || !strings.Contains(err.Error(), "loading config") {
		t.Errorf("prepare err = %v", err)
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestWebhookRebuild(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	origin := fixtureRoot(t)
	if err := os.WriteFile(filepath.Join(origin, "site", "docs", ".gitkeep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, origin, "init", "-q", "-b", "main")
	gitIn(t, origin, "add", "-A")
	gitIn(t, origin, "commit", "-q", "-m", "init")
	clone := filepath.Join(t.TempDir(), "clone")
	gitIn(t, filepath.Dir(clone), "clone", "-q", origin, clone)

	data := newComplianceData(clone, "private")
	wh := &webhookHandler{root: clone, profile: "private", secret: "s", repo: "test/compliance", mcpData: data}
	wh.rebuild()
	if data.catalog == nil || len(data.catalog.Controls) != 5 {
		t.Errorf("rebuild must reload MCP data after pulling")
	}
	if _, err := os.Stat(filepath.Join(clone, "site", "docs")); err != nil {
		t.Errorf("rebuild must render the site: %v", err)
	}

	// gitPull failure (not a repository) is reported and aborts the rebuild.
	notRepo := t.TempDir()
	if err := gitPull(notRepo); err == nil {
		t.Error("gitPull outside a repository must fail")
	}
	fresh := newComplianceData(notRepo, "private")
	(&webhookHandler{root: notRepo, profile: "private", mcpData: fresh}).rebuild()
	if fresh.catalog != nil {
		t.Error("failed pull must not reload data")
	}

	// Render failure after a successful pull.
	if err := os.WriteFile(filepath.Join(origin, ".grc.yaml"), []byte("project: [bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, origin, "commit", "-q", "-am", "break config")
	badData := newComplianceData(clone, "private")
	wh.mcpData = badData
	wh.rebuild()
	if badData.catalog != nil {
		t.Error("failed render must not reload data")
	}
}
