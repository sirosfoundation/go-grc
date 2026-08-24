// Package serve implements the "grc serve" command — an HTTP server that
// serves the rendered compliance site and optionally listens for GitHub
// webhooks to trigger automatic re-renders.
package serve

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/sirosfoundation/go-grc/cmd/grc/render"
	"github.com/sirosfoundation/go-grc/pkg/config"
)

// NewCommand returns the cobra command for "grc serve".
func NewCommand() *cobra.Command {
	var (
		profile         string
		addr            string
		webhookSecret   string
		enableWebhook   bool
		rebuildInterval time.Duration
		authIssuer      string
		authClientID    string
		authClientSecre string
		baseURL         string
		mcpAudience     string
		mcpScopes       string
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the rendered compliance site over HTTP",
		Long: `Start an HTTP server that serves the rendered compliance dashboard.

The site is rendered on startup (including a Docusaurus build when pnpm is
available) and can be automatically re-rendered when a GitHub push webhook
is received or on a recurring schedule (--rebuild-interval).

When --auth-issuer is set, the site (browser session login) and /mcp (OAuth
bearer token) are both protected against the given OIDC provider. When
unset, auth is disabled entirely (the pre-existing behavior).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			authCfg := authConfig{
				Issuer:       authIssuer,
				ClientID:     authClientID,
				ClientSecret: authClientSecre,
				BaseURL:      baseURL,
				MCPAudience:  mcpAudience,
				MCPScopes:    splitScopes(mcpScopes),
			}
			return runServe(root, profile, addr, webhookSecret, enableWebhook, rebuildInterval, authCfg)
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "private", `Render profile: "public" or "private" (default)`)
	cmd.Flags().StringVar(&addr, "addr", ":8080", "Listen address (host:port)")
	cmd.Flags().BoolVar(&enableWebhook, "webhook", false, "Enable GitHub webhook listener at /webhook")
	cmd.Flags().StringVar(&webhookSecret, "webhook-secret", "", "GitHub webhook secret (required if --webhook is set). Can also be set via GRC_WEBHOOK_SECRET env var.")
	cmd.Flags().DurationVar(&rebuildInterval, "rebuild-interval", 24*time.Hour, "How often to automatically re-render and rebuild the site (0 to disable)")
	cmd.Flags().StringVar(&authIssuer, "auth-issuer", "", "OIDC issuer URL for site login + MCP bearer auth (disabled if unset). Can also be set via GRC_OIDC_ISSUER env var.")
	cmd.Flags().StringVar(&authClientID, "auth-client-id", "", "OIDC client ID for the browser login flow. Can also be set via GRC_OIDC_CLIENT_ID env var.")
	cmd.Flags().StringVar(&authClientSecre, "auth-client-secret", "", "OIDC client secret for the browser login flow. Can also be set via GRC_OIDC_CLIENT_SECRET env var.")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "Public base URL of this deployment, e.g. https://grc.siros.org (required if --auth-issuer is set). Can also be set via GRC_BASE_URL env var.")
	cmd.Flags().StringVar(&mcpAudience, "mcp-audience", "", "Required \"aud\" claim on /mcp bearer tokens. Can also be set via GRC_MCP_AUDIENCE env var.")
	cmd.Flags().StringVar(&mcpScopes, "mcp-scopes", defaultMCPScopes, "Scopes advertised as \"scopes_supported\" in the /mcp protected-resource metadata (space- or comma-separated). Can also be set via GRC_MCP_SCOPES env var.")
	return cmd
}

// resolveServeSecrets fills in the webhook and auth settings from
// environment variables when the corresponding flag was left empty, and
// validates the webhook secret is present when the webhook listener is
// enabled.
func resolveServeSecrets(webhookSecret string, enableWebhook bool, authCfg authConfig) (string, authConfig, error) {
	if webhookSecret == "" {
		webhookSecret = os.Getenv("GRC_WEBHOOK_SECRET")
	}
	if enableWebhook && webhookSecret == "" {
		return "", authCfg, fmt.Errorf("--webhook-secret or GRC_WEBHOOK_SECRET is required when --webhook is enabled")
	}

	if authCfg.Issuer == "" {
		authCfg.Issuer = os.Getenv("GRC_OIDC_ISSUER")
	}
	if authCfg.ClientID == "" {
		authCfg.ClientID = os.Getenv("GRC_OIDC_CLIENT_ID")
	}
	if authCfg.ClientSecret == "" {
		authCfg.ClientSecret = os.Getenv("GRC_OIDC_CLIENT_SECRET")
	}
	if authCfg.BaseURL == "" {
		authCfg.BaseURL = os.Getenv("GRC_BASE_URL")
	}
	if authCfg.MCPAudience == "" {
		authCfg.MCPAudience = os.Getenv("GRC_MCP_AUDIENCE")
	}
	if env := os.Getenv("GRC_MCP_SCOPES"); env != "" && sameScopes(authCfg.MCPScopes, defaultMCPScopes) {
		authCfg.MCPScopes = splitScopes(env)
	}
	return webhookSecret, authCfg, nil
}

// defaultMCPScopes is what an MCP client should request to authenticate to
// /mcp: an OIDC login plus a refresh token so long-lived connections don't
// have to re-prompt. Deliberately minimal -- the endpoint authorizes on the
// token's audience, not on any scope.
const defaultMCPScopes = "openid profile email offline_access"

// splitScopes parses a space- or comma-separated scope list.
func splitScopes(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	if len(fields) == 0 {
		return nil
	}
	return fields
}

// sameScopes reports whether scopes is exactly the parsed form of raw, i.e.
// the flag was left at its default and an env var may still override it.
func sameScopes(scopes []string, raw string) bool {
	def := splitScopes(raw)
	if len(scopes) != len(def) {
		return false
	}
	for i := range scopes {
		if scopes[i] != def[i] {
			return false
		}
	}
	return true
}

// maybeInitAuth constructs the OIDC auth helper when an issuer is
// configured, or returns nil (auth disabled, the pre-existing behavior)
// otherwise.
func maybeInitAuth(authCfg authConfig) (*oidcAuth, error) {
	if authCfg.Issuer == "" {
		return nil, nil
	}
	auth, err := newOIDCAuth(context.Background(), authCfg)
	if err != nil {
		return nil, fmt.Errorf("initializing auth: %w", err)
	}
	log.Printf("Auth enabled: issuer=%s base-url=%s", authCfg.Issuer, authCfg.BaseURL)
	return auth, nil
}

// buildMux assembles routing for grc serve: health/readiness probes, the
// optional GitHub webhook listener, the optional auth routes + RFC 9728
// metadata, the MCP endpoint (private profile only, bearer-gated when auth
// is enabled), and the static site (session-gated when auth is enabled).
type muxConfig struct {
	root, profile, webhookSecret string
	enableWebhook                bool
	cfg                          *config.Config
	mcpData                      *complianceData
	auth                         *oidcAuth
	serveDir                     string
}

func buildMux(m muxConfig) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"status":"healthy"}`)
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"status":"ready"}`)
	})

	if m.enableWebhook {
		wh := &webhookHandler{
			root:    m.root,
			profile: m.profile,
			secret:  m.webhookSecret,
			repo:    m.cfg.Project.Repo,
			mcpData: m.mcpData,
		}
		mux.Handle("/webhook", wh)
		log.Printf("Webhook listener enabled for repo %s", m.cfg.Project.Repo)
	}

	if m.auth != nil {
		mux.HandleFunc("/auth/login", m.auth.loginHandler)
		mux.HandleFunc("/auth/callback", m.auth.callbackHandler)
		mux.HandleFunc("/auth/logout", m.auth.logoutHandler)
		mux.Handle(m.auth.protectedResourceMetadataPath(), m.auth.protectedResourceMetadataHandler())
	}

	mux.Handle("/mcp", mcpMuxHandler(m.mcpData, m.auth))
	mux.Handle("/", siteMuxHandler(m.serveDir, m.auth))

	return mux
}

// mcpMuxHandler returns the /mcp handler (private profile only), bearer-gated
// when auth is enabled, or a 404 passthrough when MCP isn't active — mirrors
// the pre-existing behavior of only registering the route when mcpData != nil.
func mcpMuxHandler(mcpData *complianceData, auth *oidcAuth) http.Handler {
	if mcpData == nil {
		return http.NotFoundHandler()
	}
	var h http.Handler = newMCPHandler(mcpData)
	if auth != nil {
		h = auth.requireBearer(h)
	}
	log.Printf("MCP server enabled at /mcp (private mode)")
	return h
}

// siteMuxHandler returns the static site handler, session-gated when auth is
// enabled.
func siteMuxHandler(serveDir string, auth *oidcAuth) http.Handler {
	h := http.FileServer(http.Dir(serveDir))
	if auth != nil {
		return auth.requireSession(h)
	}
	return h
}

// prepareSite loads the project config, performs the initial render +
// Docusaurus build, and loads MCP data (private profile only). Returns the
// loaded config and MCP data (nil for public profile).
func prepareSite(root, profile string) (*config.Config, *complianceData, error) {
	cfg, err := config.New(root)
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %w", err)
	}

	var mcpData *complianceData
	if profile == "private" {
		mcpData = newComplianceData(root, profile)
	}

	log.Printf("Rendering site (profile=%s)...", profile)
	if err := renderAndBuild(root, profile); err != nil {
		return nil, nil, fmt.Errorf("initial build: %w", err)
	}
	log.Printf("Site rendered to %s", cfg.SiteDir)

	if mcpData != nil {
		if err := mcpData.reload(); err != nil {
			log.Printf("WARNING: MCP data load failed: %v", err)
		} else {
			log.Printf("MCP data loaded")
		}
	}

	return cfg, mcpData, nil
}

// resolveServeDir picks the Docusaurus build output (site/build/) when
// available, falling back to the raw rendered markdown (site/docs/).
func resolveServeDir(cfg *config.Config) string {
	buildDir := filepath.Join(filepath.Dir(cfg.SiteDir), "build")
	if fi, err := os.Stat(buildDir); err == nil && fi.IsDir() {
		log.Printf("Serving built site from %s", buildDir)
		return buildDir
	}
	log.Printf("WARNING: no Docusaurus build found at %s — serving raw markdown (no styling)", buildDir)
	log.Printf("Run 'cd site && pnpm exec docusaurus build' first for a styled site")
	return cfg.SiteDir
}

// startRebuildTicker runs renderAndBuild on the given interval until stopCh
// is closed. No-op when interval is 0.
func startRebuildTicker(stopCh <-chan struct{}, interval time.Duration, root, profile string, mcpData *complianceData) {
	if interval <= 0 {
		return
	}
	go func() {
		log.Printf("Scheduled rebuild every %s", interval)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				runScheduledRebuild(root, profile, mcpData)
			case <-stopCh:
				return
			}
		}
	}()
}

func runScheduledRebuild(root, profile string, mcpData *complianceData) {
	log.Printf("Scheduled rebuild starting...")
	if err := renderAndBuild(root, profile); err != nil {
		log.Printf("Scheduled rebuild failed: %v", err)
		return
	}
	log.Printf("Scheduled rebuild completed")
	if mcpData != nil {
		if err := mcpData.reload(); err != nil {
			log.Printf("MCP data reload failed: %v", err)
		}
	}
}

// serveUntilShutdown starts srv in the background, waits for either a
// termination signal or a listener error, then gracefully shuts down.
func serveUntilShutdown(srv *http.Server, stopCh chan struct{}) error {
	errCh := make(chan error, 1)
	go func() {
		log.Printf("Listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("Received %v, shutting down...", sig)
	case err := <-errCh:
		close(stopCh)
		return err
	}

	close(stopCh)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

func runServe(root, profile, addr, webhookSecret string, enableWebhook bool, rebuildInterval time.Duration, authCfg authConfig) error {
	webhookSecret, authCfg, err := resolveServeSecrets(webhookSecret, enableWebhook, authCfg)
	if err != nil {
		return err
	}

	auth, err := maybeInitAuth(authCfg)
	if err != nil {
		return err
	}

	cfg, mcpData, err := prepareSite(root, profile)
	if err != nil {
		return err
	}

	mux := buildMux(muxConfig{
		root: root, profile: profile, webhookSecret: webhookSecret, enableWebhook: enableWebhook,
		cfg: cfg, mcpData: mcpData, auth: auth, serveDir: resolveServeDir(cfg),
	})

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	stopCh := make(chan struct{})
	startRebuildTicker(stopCh, rebuildInterval, root, profile, mcpData)
	return serveUntilShutdown(srv, stopCh)
}

// webhookHandler handles GitHub push webhooks and triggers site re-renders.
type webhookHandler struct {
	root    string
	profile string
	secret  string
	repo    string
	mcpData *complianceData

	mu         sync.Mutex
	lastRender time.Time
}

func (wh *webhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read body (limit to 10 MB).
	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer func() { _ = r.Body.Close() }()

	// Verify signature.
	sig := r.Header.Get("X-Hub-Signature-256")
	if !verifySignature(body, sig, wh.secret) {
		http.Error(w, "invalid signature", http.StatusForbidden)
		return
	}

	// Check event type.
	event := r.Header.Get("X-GitHub-Event")
	if event == "ping" {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"status":"pong"}`)
		return
	}
	if event != "push" {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"status":"ignored","event":"%s"}`, event)
		return
	}

	// Parse push payload to verify repo.
	var payload struct {
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	if !strings.EqualFold(payload.Repository.FullName, wh.repo) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"status":"ignored","reason":"repo mismatch"}`)
		return
	}

	// Only re-render on pushes to the default branch (main/master).
	ref := payload.Ref
	if ref != "refs/heads/main" && ref != "refs/heads/master" {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"status":"ignored","reason":"non-default branch"}`)
		return
	}

	// Debounce: skip if last render was < 10 seconds ago.
	wh.mu.Lock()
	if time.Since(wh.lastRender) < 10*time.Second {
		wh.mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"status":"debounced"}`)
		return
	}
	wh.lastRender = time.Now()
	wh.mu.Unlock()

	// Acknowledge immediately and rebuild in the background: a full
	// render + Docusaurus build routinely takes 30-60s, well past GitHub's
	// webhook delivery timeout, which would otherwise report every
	// successful delivery as a failed one.
	go wh.rebuild()

	w.WriteHeader(http.StatusAccepted)
	_, _ = fmt.Fprintf(w, `{"status":"accepted"}`)
}

// rebuild pulls latest changes and re-renders the site. Runs in the
// background so ServeHTTP can respond before GitHub's delivery timeout.
func (wh *webhookHandler) rebuild() {
	log.Printf("Webhook: push to %s, pulling and re-rendering...", wh.repo)
	if err := gitPull(wh.root); err != nil {
		log.Printf("Webhook: git pull failed: %v", err)
		return
	}

	if err := renderAndBuild(wh.root, wh.profile); err != nil {
		log.Printf("Webhook: rebuild failed: %v", err)
		return
	}
	if wh.mcpData != nil {
		if err := wh.mcpData.reload(); err != nil {
			log.Printf("Webhook: MCP data reload failed: %v", err)
		}
	}

	log.Printf("Webhook: site rebuilt successfully")
}

// verifySignature checks the HMAC-SHA256 signature from GitHub.
func verifySignature(body []byte, sig, secret string) bool {
	if !strings.HasPrefix(sig, "sha256=") {
		return false
	}
	sigBytes, err := hex.DecodeString(strings.TrimPrefix(sig, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)
	return hmac.Equal(sigBytes, expected)
}

// renderAndBuild runs the full render + Docusaurus build pipeline.
func renderAndBuild(root, profile string) error {
	if err := render.Run(root, profile); err != nil {
		return fmt.Errorf("render: %w", err)
	}
	if err := buildSite(root); err != nil {
		return fmt.Errorf("build: %w", err)
	}
	return nil
}

// buildSite runs pnpm install + docusaurus build in the site directory.
// If pnpm is not available it logs a warning and returns nil.
func buildSite(root string) error {
	cfg, err := config.New(root)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	siteRoot := filepath.Dir(cfg.SiteDir)

	// Check if pnpm is available.
	if _, err := exec.LookPath("pnpm"); err != nil {
		log.Printf("WARNING: pnpm not found — skipping Docusaurus build")
		return nil
	}

	// Check if package.json exists in site root.
	if _, err := os.Stat(filepath.Join(siteRoot, "package.json")); err != nil {
		log.Printf("WARNING: no package.json in %s — skipping Docusaurus build", siteRoot)
		return nil
	}

	log.Printf("Installing site dependencies...")
	install := exec.Command("pnpm", "install", "--frozen-lockfile")
	install.Dir = siteRoot
	install.Env = append(os.Environ(), "CI=true")
	install.Stdout = os.Stdout
	install.Stderr = os.Stderr
	if err := install.Run(); err != nil {
		return fmt.Errorf("pnpm install: %w", err)
	}

	log.Printf("Building Docusaurus site...")
	build := exec.Command("pnpm", "exec", "docusaurus", "build")
	build.Dir = siteRoot
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		return fmt.Errorf("docusaurus build: %w", err)
	}

	log.Printf("Docusaurus build completed")
	return nil
}

// gitPull runs "git pull" in the compliance data root.
func gitPull(root string) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	cmd := newGitCommand(absRoot, "pull", "--ff-only")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
