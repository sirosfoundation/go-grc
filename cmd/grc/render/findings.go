package render

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sirosfoundation/go-grc/pkg/audit"
	"github.com/sirosfoundation/go-grc/pkg/config"
)

func generateFindings(cfg *config.Config, audits *audit.AuditSet, activeFindings []*audit.Finding) error {
	dir := filepath.Join(cfg.SiteDir, "findings")
	if err := writePage(filepath.Join(dir, "_category_.json"), categoryJSON("Findings", 3)); err != nil {
		return err
	}
	if err := writePage(filepath.Join(dir, "index.md"), renderFindingsIndex(activeFindings)); err != nil {
		return err
	}

	for _, f := range activeFindings {
		slug := idSlug(f.ID)
		if err := writePage(filepath.Join(dir, slug+".md"), renderFindingPage(f, cfg)); err != nil {
			return err
		}
	}
	return nil
}

func renderFindingPage(f *audit.Finding, cfg *config.Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\ntitle: \"%s — %s\"\nsidebar_label: \"%s\"\n---\n\n", f.ID, f.Title, f.ID)
	fmt.Fprintf(&b, "# %s — %s\n\n", f.ID, f.Title)

	b.WriteString("| | |\n|---|---|\n")
	fmt.Fprintf(&b, "| **Severity** | %s %s |\n", sevIcon(f.Severity), f.Severity)
	fmt.Fprintf(&b, "| **Status** | %s |\n", f.Status)
	fmt.Fprintf(&b, "| **Owner** | %s |\n", ownerBadge(f.Owner))
	if f.TrackingIssue != nil {
		fmt.Fprintf(&b, "| **Tracking Issue** | [%s#%d](https://github.com/%s/issues/%d) |\n",
			f.TrackingIssue.Repo, f.TrackingIssue.Number, f.TrackingIssue.Repo, f.TrackingIssue.Number)
	}
	if f.ResolvedDate != "" {
		fmt.Fprintf(&b, "| **Resolved** | %s |\n", f.ResolvedDate)
	}
	b.WriteString("\n")

	if f.Description != "" {
		fmt.Fprintf(&b, "## Description\n\n%s\n\n", f.Description)
	}

	if len(f.Controls) > 0 {
		fmt.Fprintf(&b, "## Controls\n\n%s\n\n", controlLinks(f.Controls))
	}

	if len(f.Evidence) > 0 {
		b.WriteString("## Evidence\n\n| Type | Reference | Description |\n|------|-----------|-------------|\n")
		for _, ev := range f.Evidence {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", ev.Type, ev.Ref, ev.Description)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func renderFindingsIndex(findings []*audit.Finding) string {
	var b strings.Builder
	b.WriteString("---\nsidebar_label: Findings\nsidebar_position: 1\ntitle: Findings Overview\n---\n\n# Findings Overview\n\n")
	fmt.Fprintf(&b, "%d open findings are tracked as GitHub issues.\n\n", len(findings))
	b.WriteString("| Finding | Severity | Owner | Controls |\n|---------|----------|-------|----------|\n")
	for _, f := range findings {
		icon := sevIcon(f.Severity)
		fmt.Fprintf(&b, "| %s | %s %s | %s | %s |\n",
			findingLink(f), icon, f.Severity, ownerBadge(f.Owner), controlLinks(f.Controls))
	}
	return b.String()
}
