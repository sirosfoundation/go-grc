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
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the rendered compliance site over HTTP",
		Long: `Start an HTTP server that serves the rendered compliance dashboard.

The site is rendered on startup (including a Docusaurus build when pnpm is
available) and can be automatically re-rendered when a GitHub push webhook
is received or on a recurring schedule (--rebuild-interval).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			return runServe(root, profile, addr, webhookSecret, enableWebhook, rebuildInterval)
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "private", `Render profile: "public" or "private" (default)`)
	cmd.Flags().StringVar(&addr, "addr", ":8080", "Listen address (host:port)")
	cmd.Flags().BoolVar(&enableWebhook, "webhook", false, "Enable GitHub webhook listener at /webhook")
	cmd.Flags().StringVar(&webhookSecret, "webhook-secret", "", "GitHub webhook secret (required if --webhook is set). Can also be set via GRC_WEBHOOK_SECRET env var.")
	cmd.Flags().DurationVar(&rebuildInterval, "rebuild-interval", 24*time.Hour, "How often to automatically re-render and rebuild the site (0 to disable)")
	return cmd
}

func runServe(root, profile, addr, webhookSecret string, enableWebhook bool, rebuildInterval time.Duration) error {
	// Resolve webhook secret from env if not set via flag.
	if webhookSecret == "" {
		webhookSecret = os.Getenv("GRC_WEBHOOK_SECRET")
	}
	if enableWebhook && webhookSecret == "" {
		return fmt.Errorf("--webhook-secret or GRC_WEBHOOK_SECRET is required when --webhook is enabled")
	}

	cfg, err := config.New(root)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Initial render + Docusaurus build.
	log.Printf("Rendering site (profile=%s)...", profile)
	if err := renderAndBuild(root, profile); err != nil {
		return fmt.Errorf("initial build: %w", err)
	}
	log.Printf("Site rendered to %s", cfg.SiteDir)

	// Determine which directory to serve.
	// Prefer the Docusaurus build output (site/build/) when available;
	// fall back to the raw rendered markdown (site/docs/).
	serveDir := cfg.SiteDir
	buildDir := filepath.Join(filepath.Dir(cfg.SiteDir), "build")
	if fi, err := os.Stat(buildDir); err == nil && fi.IsDir() {
		serveDir = buildDir
		log.Printf("Serving built site from %s", serveDir)
	} else {
		log.Printf("WARNING: no Docusaurus build found at %s — serving raw markdown (no styling)", buildDir)
		log.Printf("Run 'cd site && pnpm exec docusaurus build' first for a styled site")
	}

	// Serve the site directory.
	mux := http.NewServeMux()

	// Health endpoint.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"status":"healthy"}`)
	})

	// Readiness endpoint.
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"status":"ready"}`)
	})

	// Webhook endpoint.
	if enableWebhook {
		wh := &webhookHandler{
			root:    root,
			profile: profile,
			secret:  webhookSecret,
			repo:    cfg.Project.Repo,
		}
		mux.Handle("/webhook", wh)
		log.Printf("Webhook listener enabled for repo %s", cfg.Project.Repo)
	}

	// Static file server for the site.
	siteFS := http.FileServer(http.Dir(serveDir))
	mux.Handle("/", siteFS)

	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in background.
	errCh := make(chan error, 1)
	go func() {
		log.Printf("Listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	// Periodic rebuild.
	stopCh := make(chan struct{})
	if rebuildInterval > 0 {
		go func() {
			log.Printf("Scheduled rebuild every %s", rebuildInterval)
			ticker := time.NewTicker(rebuildInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					log.Printf("Scheduled rebuild starting...")
					if err := renderAndBuild(root, profile); err != nil {
						log.Printf("Scheduled rebuild failed: %v", err)
					} else {
						log.Printf("Scheduled rebuild completed")
					}
				case <-stopCh:
					return
				}
			}
		}()
	}

	// Graceful shutdown on signal.
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

// webhookHandler handles GitHub push webhooks and triggers site re-renders.
type webhookHandler struct {
	root    string
	profile string
	secret  string
	repo    string

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

	// Pull latest changes.
	log.Printf("Webhook: push to %s, pulling and re-rendering...", wh.repo)
	if err := gitPull(wh.root); err != nil {
		log.Printf("Webhook: git pull failed: %v", err)
		http.Error(w, "git pull failed", http.StatusInternalServerError)
		return
	}

	// Re-render and rebuild.
	if err := renderAndBuild(wh.root, wh.profile); err != nil {
		log.Printf("Webhook: rebuild failed: %v", err)
		http.Error(w, "rebuild failed", http.StatusInternalServerError)
		return
	}

	log.Printf("Webhook: site rebuilt successfully")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `{"status":"rebuilt"}`)
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
