package render

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sirosfoundation/go-grc/pkg/audit"
	"github.com/sirosfoundation/go-grc/pkg/catalog"
	"github.com/sirosfoundation/go-grc/pkg/config"
	"github.com/sirosfoundation/go-grc/pkg/mapping"
)

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "render",
		Short: "Generate Docusaurus site pages from catalog, mappings, and findings",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			return run(root)
		},
	}
	return cmd
}

func run(root string) error {
	cfg := config.New(root)

	cat, err := catalog.Load(cfg.CatalogDir)
	if err != nil {
		return fmt.Errorf("loading catalog: %w", err)
	}
	audits, err := audit.Load(cfg.AuditsDir)
	if err != nil {
		return fmt.Errorf("loading audits: %w", err)
	}
	maps, err := mapping.Load(cfg.MappingsDir)
	if err != nil {
		return fmt.Errorf("loading mappings: %w", err)
	}

	if err := generateFindings(cfg, audits); err != nil {
		return err
	}
	if err := generateControls(cfg, cat, audits); err != nil {
		return err
	}
	if err := generateEUDI(cfg, maps, cat, audits); err != nil {
		return err
	}
	if err := generateISO(cfg, maps); err != nil {
		return err
	}
	if err := generateGDPR(cfg, maps); err != nil {
		return err
	}
	if err := generateLanding(cfg, cat, audits); err != nil {
		return err
	}

	fmt.Println("Site generated.")
	return nil
}

func ensureDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0755)
}

func writePage(path, content string) error {
	if err := ensureDir(path); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}

func generateFindings(cfg *config.Config, audits *audit.AuditSet) error {
	var b strings.Builder
	b.WriteString("---\ntitle: Open Findings\nsidebar_position: 1\n---\n\n# Open Findings\n\n")
	b.WriteString("| Finding | Severity | Title | Owner | Status | Evidence | Tracking |\n")
	b.WriteString("|---------|----------|-------|-------|--------|----------|----------|\n")

	var findings []*audit.FindingRef
	for _, ref := range audits.FindingsByID {
		findings = append(findings, ref)
	}
	sort.Slice(findings, func(i, j int) bool {
		return sevRank(findings[i].Finding.Severity) > sevRank(findings[j].Finding.Severity)
	})

	for _, ref := range findings {
		f := ref.Finding
		tracking := ""
		if f.TrackingIssue != nil {
			tracking = fmt.Sprintf("[#%d](https://github.com/%s/issues/%d)", f.TrackingIssue.Number, f.TrackingIssue.Repo, f.TrackingIssue.Number)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %d | %s |\n",
			f.ID, sevBadge(f.Severity), f.Title, f.Owner, statusIcon(f.Status), len(f.Evidence), tracking)
	}

	return writePage(filepath.Join(cfg.SiteDir, "findings", "index.md"), b.String())
}

func generateControls(cfg *config.Config, cat *catalog.Catalog, audits *audit.AuditSet) error {
	for _, group := range cat.Groups {
		kind := "technical"
		if group.ID == "governance" || group.ID == "operations" || group.ID == "people" || group.ID == "physical" {
			kind = "organizational"
		}
		for _, ctrl := range group.Controls {
			slug := strings.ToLower(strings.ReplaceAll(ctrl.ID, "-", "_"))
			path := filepath.Join(cfg.SiteDir, "controls", kind, slug+".md")

			var b strings.Builder
			fmt.Fprintf(&b, "---\ntitle: \"%s: %s\"\n---\n\n# %s: %s\n\n", ctrl.ID, ctrl.Title, ctrl.ID, ctrl.Title)
			effective := ctrl.Status
			if ctrl.DerivedStatus != "" {
				effective = ctrl.DerivedStatus
			}
			fmt.Fprintf(&b, "**Status:** %s %s | **Owner:** %s | **CSF:** %s\n\n", statusIcon(effective), effective, ctrl.Owner, ctrl.CSFFunction)
			if ctrl.Description != "" {
				fmt.Fprintf(&b, "%s\n\n", ctrl.Description)
			}
			findings := audits.FindingsByControl[ctrl.ID]
			if len(findings) > 0 {
				b.WriteString("## Findings\n\n| ID | Severity | Status | Evidence |\n|---|---|---|---|\n")
				for _, ref := range findings {
					f := ref.Finding
					fmt.Fprintf(&b, "| %s | %s | %s | %d |\n", f.ID, sevBadge(f.Severity), statusIcon(f.Status), len(f.Evidence))
				}
				b.WriteString("\n")
			}
			if err := writePage(path, b.String()); err != nil {
				return err
			}
		}
	}
	return nil
}

func generateEUDI(cfg *config.Config, maps *mapping.Mappings, cat *catalog.Catalog, audits *audit.AuditSet) error {
	if maps.EUDI == nil {
		return nil
	}
	var b strings.Builder
	b.WriteString("---\ntitle: EUDI Security Requirements\nsidebar_position: 1\n---\n\n# EUDI Security Requirements\n\n")
	b.WriteString("| Req | Result | Status | Controls | Owner |\n|---|---|---|---|---|\n")
	for _, req := range maps.EUDI.Requirements {
		ctrls := strings.Join(req.Controls, ", ")
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", req.ID, resultBadge(req.Result), req.Status, ctrls, req.Owner)
	}
	return writePage(filepath.Join(cfg.SiteDir, "frameworks", "eudi.md"), b.String())
}

func generateISO(cfg *config.Config, maps *mapping.Mappings) error {
	if maps.ISO == nil {
		return nil
	}
	var b strings.Builder
	b.WriteString("---\ntitle: ISO 27001 Annex A\nsidebar_position: 2\n---\n\n# ISO 27001 Annex A\n\n")
	b.WriteString("| Annex A | Controls | Coverage | Owner |\n|---|---|---|---|\n")
	for _, m := range maps.ISO.Mappings {
		ctrls := strings.Join(m.Controls, ", ")
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", m.AnnexA, ctrls, covBadge(m.Coverage), m.Owner)
	}
	return writePage(filepath.Join(cfg.SiteDir, "frameworks", "iso27001.md"), b.String())
}

func generateGDPR(cfg *config.Config, maps *mapping.Mappings) error {
	if maps.GDPR == nil {
		return nil
	}
	var b strings.Builder
	b.WriteString("---\ntitle: GDPR Checklist\nsidebar_position: 3\n---\n\n# GDPR Checklist\n\n")
	b.WriteString("| Item | Controls | Coverage | Owner |\n|---|---|---|---|\n")
	for _, m := range maps.GDPR.Mappings {
		ctrls := strings.Join(m.Controls, ", ")
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", m.MatchName, ctrls, covBadge(m.Coverage), m.Owner)
	}
	return writePage(filepath.Join(cfg.SiteDir, "frameworks", "gdpr.md"), b.String())
}

func generateLanding(cfg *config.Config, cat *catalog.Catalog, audits *audit.AuditSet) error {
	open, inProg, resolved, withEv := 0, 0, 0, 0
	for _, ref := range audits.FindingsByID {
		switch ref.Finding.Status {
		case "open":
			open++
		case "in_progress":
			inProg++
		case "resolved":
			resolved++
			if ref.Finding.HasEvidence() {
				withEv++
			}
		}
	}
	var b strings.Builder
	b.WriteString("---\ntitle: Compliance Dashboard\nslug: /\n---\n\n# SirosID Compliance Dashboard\n\n")
	fmt.Fprintf(&b, "| Metric | Value |\n|---|---|\n")
	fmt.Fprintf(&b, "| Controls | %d |\n| Findings | %d |\n| Open | %d |\n| In Progress | %d |\n| Resolved | %d |\n| With Evidence | %d |\n",
		len(cat.Controls), len(audits.FindingsByID), open, inProg, resolved, withEv)
	return writePage(filepath.Join(cfg.SiteDir, "index.md"), b.String())
}

func sevRank(s string) int {
	switch s {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	}
	return 0
}

func sevBadge(s string) string {
	m := map[string]string{"critical": "critical", "high": "high", "medium": "medium", "low": "low"}
	if v, ok := m[s]; ok {
		return v
	}
	return s
}

func statusIcon(s string) string {
	m := map[string]string{"open": "open", "in_progress": "in-progress", "resolved": "resolved", "to_do": "to-do", "planned": "planned", "verified": "verified", "validated": "validated"}
	if v, ok := m[s]; ok {
		return v
	}
	return s
}

func resultBadge(r string) string { return r }
func covBadge(c string) string    { return c }
