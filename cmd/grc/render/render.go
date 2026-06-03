package render

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sirosfoundation/go-grc/pkg/audit"
	"github.com/sirosfoundation/go-grc/pkg/catalog"
	"github.com/sirosfoundation/go-grc/pkg/config"
	"github.com/sirosfoundation/go-grc/pkg/mapping"
	"github.com/sirosfoundation/go-grc/pkg/risk"
	"github.com/sirosfoundation/go-grc/pkg/yearcycle"
)

// titleCase returns s with the first letter upper-cased (ASCII-only, sufficient
// for the fixed set of group kinds used in rendering).
func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func NewCommand() *cobra.Command {
	var profile string
	cmd := &cobra.Command{
		Use:   "render",
		Short: "Generate Docusaurus site pages from catalog, mappings, and findings",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			return run(root, profile)
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "public", `Render profile: "public" (no status/findings) or "private" (full detail)`)
	return cmd
}

// --- URL maps (populated during run, used for cross-linking) ---
var (
	projectOrg    string
	controlURL    map[string]string
	frameworkURLs map[string]map[string]string // framework ID → req key → URL
)

func run(root, profile string) error {
	cfg, err := config.New(root)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	cat, err := catalog.Load(cfg.CatalogDir, cfg.CatalogSubdirs...)
	if err != nil {
		return fmt.Errorf("loading catalog: %w", err)
	}
	audits, err := audit.Load(cfg.AuditsDir)
	if err != nil {
		return fmt.Errorf("loading audits: %w", err)
	}
	maps, err := mapping.Load(cfg.MappingsDir, cfg.Frameworks)
	if err != nil {
		return fmt.Errorf("loading mappings: %w", err)
	}

	isPublic := profile != "private"

	// Derive effective control statuses from findings before rendering.
	if !isPublic {
		catalog.DeriveControlStatuses(cat, audits)
	}

	// Extract org from project repo for GitHub links.
	if parts := strings.SplitN(cfg.Project.Repo, "/", 2); len(parts) >= 1 {
		projectOrg = parts[0]
	}

	// Load framework catalogs (normative requirement text)
	fwCats := make(map[string]*catalog.FrameworkCatalog)
	for _, fw := range cfg.Frameworks {
		name := strings.TrimSuffix(fw.CatalogFile, ".yaml")
		name = strings.TrimSuffix(name, ".yml")
		fwCat, _ := catalog.LoadFrameworkCatalog(cfg.CatalogDir, name)
		if fwCat != nil {
			fwCats[fw.ID] = fwCat
		}
	}

	// Active findings (have tracking issue, not terminal) — private only
	var activeFindings []*audit.Finding
	if !isPublic {
		for _, ref := range audits.FindingsByID {
			f := ref.Finding
			if f.TrackingIssue != nil && !f.IsTerminal() {
				activeFindings = append(activeFindings, f)
			}
		}
		sort.Slice(activeFindings, func(i, j int) bool {
			return sevRank(activeFindings[i].Severity) > sevRank(activeFindings[j].Severity)
		})
	}

	// Build URL maps
	controlURL = make(map[string]string)
	frameworkURLs = make(map[string]map[string]string)

	for _, group := range cat.Groups {
		kind := groupKind(group)
		for _, ctrl := range group.Controls {
			slug := idSlug(ctrl.ID)
			controlURL[ctrl.ID] = "/controls/" + kind + "/" + slug
		}
	}
	for _, fw := range cfg.Frameworks {
		fm := maps[fw.ID]
		if fm == nil {
			continue
		}
		urls := make(map[string]string)
		for _, e := range fm.Entries {
			slug := entrySlug(e.Key)
			urls[e.Key] = "/frameworks/" + fw.Slug + "/" + slug
		}
		frameworkURLs[fw.ID] = urls
	}

	// Build framework→control reverse index
	frameworkRefs := buildFrameworkRefs(maps)

	// Render into a staging directory, then atomically swap each subdir
	// so the live site is never missing pages during regeneration.
	stagingDir, err := os.MkdirTemp(filepath.Dir(cfg.SiteDir), ".grc-staging-*")
	if err != nil {
		return fmt.Errorf("creating staging dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(stagingDir) }() // clean up on error

	origSiteDir := cfg.SiteDir
	cfg.SiteDir = stagingDir

	// Controls
	if err := generateControls(cfg, cat, audits, activeFindings, frameworkRefs, isPublic); err != nil {
		cfg.SiteDir = origSiteDir
		return err
	}
	// Frameworks
	if err := generateFrameworks(cfg, cat, maps, audits, activeFindings, fwCats, isPublic); err != nil {
		cfg.SiteDir = origSiteDir
		return err
	}
	// CSF overview
	if err := generateCSFPage(cfg, cat, isPublic); err != nil {
		cfg.SiteDir = origSiteDir
		return err
	}
	// Findings
	if !isPublic {
		if err := generateFindings(cfg, audits, activeFindings); err != nil {
			cfg.SiteDir = origSiteDir
			return err
		}
	}
	// Risk register (private only, if configured and not public)
	if !isPublic && !cfg.RiskRegister.Public && cfg.RiskDir != "" {
		risks, err := risk.Load(cfg.RiskDir, cfg.RiskRegister.Files)
		if err != nil {
			cfg.SiteDir = origSiteDir
			return fmt.Errorf("loading risk register: %w", err)
		}
		if len(risks.Files) > 0 {
			if err := generateRiskRegister(cfg, risks); err != nil {
				cfg.SiteDir = origSiteDir
				return err
			}
		}
	}
	// Year cycle (if configured and visibility matches profile)
	if cfg.YearCycle.Source != "" && (!isPublic || cfg.YearCycle.Public) {
		yc, err := yearcycle.Load(cfg.YearCycle.Source, cfg.YearCycle.Title)
		if err != nil {
			cfg.SiteDir = origSiteDir
			return fmt.Errorf("loading year cycle: %w", err)
		}
		if err := generateYearCycle(cfg, yc); err != nil {
			cfg.SiteDir = origSiteDir
			return err
		}
	}
	// Landing page
	if err := generateLanding(cfg, cat, activeFindings, isPublic); err != nil {
		cfg.SiteDir = origSiteDir
		return err
	}

	cfg.SiteDir = origSiteDir

	// Atomically swap each subdirectory
	subdirs := []string{"controls", "frameworks"}
	if !isPublic {
		subdirs = append(subdirs, "findings")
		if !cfg.RiskRegister.Public && cfg.RiskDir != "" {
			subdirs = append(subdirs, "risk-register")
		}
	}
	if cfg.YearCycle.Source != "" && (!isPublic || cfg.YearCycle.Public) {
		subdirs = append(subdirs, "year-cycle")
	}
	for _, subdir := range subdirs {
		dst := filepath.Join(cfg.SiteDir, subdir)
		src := filepath.Join(stagingDir, subdir)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		old := dst + ".old"
		_ = os.RemoveAll(old)
		_ = os.Rename(dst, old) // move current out of the way
		if err := os.Rename(src, dst); err != nil {
			_ = os.Rename(old, dst) // rollback on failure
			return fmt.Errorf("swapping %s: %w", subdir, err)
		}
		_ = os.RemoveAll(old) // clean up previous version
	}
	// Swap top-level files
	for _, fname := range []string{"index.md", "csf.md"} {
		srcFile := filepath.Join(stagingDir, fname)
		dstFile := filepath.Join(cfg.SiteDir, fname)
		if _, err := os.Stat(srcFile); err == nil {
			_ = os.Rename(dstFile, dstFile+".old")
			if err := os.Rename(srcFile, dstFile); err != nil {
				_ = os.Rename(dstFile+".old", dstFile)
				return fmt.Errorf("swapping %s: %w", fname, err)
			}
			_ = os.RemoveAll(dstFile + ".old")
		}
	}

	// Strip findings/severity from architecture docs in public profile
	if isPublic {
		if err := sanitizeArchitectureDocs(cfg.SiteDir); err != nil {
			return err
		}
	}

	// Generate Docusaurus sidebars.ts and update config based on profile
	if err := generateDocusaurusConfig(cfg, isPublic); err != nil {
		return err
	}

	fmt.Println("Site generated.")
	return nil
}

// ---------------------------------------------------------------------------
// Architecture doc sanitization (public profile)
// ---------------------------------------------------------------------------

// reFindingRef matches inline finding ID references (e.g., AV-P-3, STR-M-1, EN-S-2, ISO-T-5, F-001).
// It also captures optional surrounding phrases like "tracked in", "per", "resolved", "see".
var reFindingRef = regexp.MustCompile(
	`(?i)` +
		`(?:\s*\(?\s*` +
		`(?:tracked in|per|resolved|see|address(?:ed)? (?:via|in))\s+` +
		`)?\b` +
		`(?:AV|STR|EN|ISO|F|P)-[A-Z]*-?\d+` +
		`\b` +
		`\)?`,
)

func sanitizeArchitectureDocs(siteDir string) error {
	archDir := filepath.Join(siteDir, "architecture")
	entries, err := os.ReadDir(archDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(archDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines := strings.Split(string(data), "\n")
		var out []string
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "| **Finding**") ||
				strings.HasPrefix(trimmed, "| **Severity**") {
				continue
			}
			line = reFindingRef.ReplaceAllString(line, "")
			out = append(out, line)
		}
		if e.Name() == "index.md" {
			out = stripTableColumn(out, "Finding")
		}
		if err := os.WriteFile(path, []byte(strings.Join(out, "\n")), 0644); err != nil {
			return err
		}
	}
	return nil
}

// stripTableColumn removes a column by header name from markdown tables.
func stripTableColumn(lines []string, colName string) []string {
	var result []string
	var colIdx int
	var inTable bool
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			inTable = false
			result = append(result, line)
			continue
		}
		parts := strings.Split(trimmed, "|")
		if !inTable {
			colIdx = -1
			for i, p := range parts {
				if strings.TrimSpace(p) == colName {
					colIdx = i
					break
				}
			}
			if colIdx < 0 {
				result = append(result, line)
				continue
			}
			inTable = true
		}
		if colIdx >= 0 && colIdx < len(parts) {
			parts = append(parts[:colIdx], parts[colIdx+1:]...)
		}
		result = append(result, strings.Join(parts, "|"))
	}
	return result
}

func generateDocusaurusConfig(cfg *config.Config, isPublic bool) error {
	// Generate sidebars.ts
	var sb strings.Builder
	sb.WriteString(`import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  controlsSidebar: [
    'controls/index',
    {type: 'autogenerated', dirName: 'controls/technical'},
    {type: 'autogenerated', dirName: 'controls/organizational'},
  ],
  architectureSidebar: [
    {type: 'autogenerated', dirName: 'architecture'},
  ],
  frameworksSidebar: [
    {type: 'autogenerated', dirName: 'frameworks'},
  ],
`)
	if !isPublic {
		sb.WriteString(`  findingsSidebar: [
    {type: 'autogenerated', dirName: 'findings'},
  ],
`)
	}
	sb.WriteString(`};

export default sidebars;
`)
	siteRoot := filepath.Dir(cfg.SiteDir)
	sidebarsPath := filepath.Join(siteRoot, "sidebars.ts")
	if err := os.WriteFile(sidebarsPath, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("writing sidebars.ts: %w", err)
	}

	configPath := filepath.Join(siteRoot, "docusaurus.config.ts")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("reading docusaurus.config.ts: %w", err)
	}
	configStr := string(data)

	findingsItem := `        {
          type: 'docSidebar',
          sidebarId: 'findingsSidebar',
          position: 'left',
          label: 'Findings',
        },`

	if isPublic && strings.Contains(configStr, findingsItem) {
		configStr = strings.Replace(configStr, findingsItem+"\n", "", 1)
		if err := os.WriteFile(configPath, []byte(configStr), 0644); err != nil {
			return fmt.Errorf("writing docusaurus.config.ts: %w", err)
		}
	} else if !isPublic && !strings.Contains(configStr, findingsItem) {
		ghItem := `        {
          href: 'https://github.com/sirosfoundation',`
		configStr = strings.Replace(configStr, ghItem, findingsItem+"\n"+ghItem, 1)
		if err := os.WriteFile(configPath, []byte(configStr), 0644); err != nil {
			return fmt.Errorf("writing docusaurus.config.ts: %w", err)
		}
	}

	if isPublic {
		findingsDir := filepath.Join(cfg.SiteDir, "docs", "findings")
		_ = os.RemoveAll(findingsDir)
	}

	return nil
}
