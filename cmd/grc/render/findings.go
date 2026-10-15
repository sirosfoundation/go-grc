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
	if err := writePage(filepath.Join(dir, "index.md"), renderFindingsIndex(audits, activeFindings)); err != nil {
		return err
	}

	for _, f := range activeFindings {
		slug := idSlug(f.ID)
		if err := writePage(filepath.Join(dir, slug+".md"), renderFindingPage(f, cfg, audits)); err != nil {
			return err
		}
	}
	return nil
}

func renderFindingPage(f *audit.Finding, cfg *config.Config, audits *audit.AuditSet) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\ntitle: \"%s — %s\"\nsidebar_label: \"%s\"\n---\n\n", f.ID, f.Title, f.ID)
	fmt.Fprintf(&b, "# %s — %s\n\n", f.ID, f.Title)

	b.WriteString("| | |\n|---|---|\n")
	if ref := audits.FindingsByID[f.ID]; ref != nil {
		fmt.Fprintf(&b, "| **%s** | %s |\n", sourceKindLabel(ref.File.Kind), sourceLink(ref.File))
	}
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

// sourceKindLabel names the kind of the audit or assessment a finding is from.
func sourceKindLabel(k audit.Kind) string {
	if k == audit.KindAssessment {
		return "Assessment"
	}
	return "Audit"
}

// sourceTitle is the display title of an audit or assessment file.
func sourceTitle(f *audit.LoadedFile) string {
	if t := f.Data.Meta().Title; t != "" {
		return t
	}
	return f.Name
}

// sourceLink links to the page of the audit or assessment a file records.
func sourceLink(f *audit.LoadedFile) string {
	if f.Kind == audit.KindAssessment {
		return fmt.Sprintf("[%s](/audits/assessments/%s)", cell(sourceTitle(f)), cycleSlug(f.Name))
	}
	return fmt.Sprintf("[%s](/audits/%s)", cell(sourceTitle(f)), cycleSlug(f.Name))
}

// renderFindingsIndex lists the open findings grouped by the audit or
// assessment they belong to, so it is clear which finding relates to which.
func renderFindingsIndex(audits *audit.AuditSet, findings []*audit.Finding) string {
	var b strings.Builder
	b.WriteString("---\nsidebar_label: Findings\nsidebar_position: 1\ntitle: Findings Overview\n---\n\n# Findings Overview\n\n")
	fmt.Fprintf(&b, "%d open findings are tracked as GitHub issues, grouped by audit or assessment.\n\n", len(findings))

	// Audits first, then assessments, each in file order.
	for _, kind := range []audit.Kind{audit.KindAudit, audit.KindAssessment} {
		for i := range audits.Files {
			file := &audits.Files[i]
			if file.Kind != kind {
				continue
			}
			var mine []*audit.Finding
			for _, f := range findings {
				if ref := audits.FindingsByID[f.ID]; ref != nil && ref.File.Path == file.Path {
					mine = append(mine, f)
				}
			}
			if len(mine) == 0 {
				continue
			}
			fmt.Fprintf(&b, "## %s: %s\n\n", sourceKindLabel(kind), cell(sourceTitle(file)))
			fmt.Fprintf(&b, "%s · %d open\n\n", sourceLink(file), len(mine))
			b.WriteString("| Finding | Severity | Owner | Controls |\n|---------|----------|-------|----------|\n")
			for _, f := range mine {
				fmt.Fprintf(&b, "| %s | %s %s | %s | %s |\n",
					findingLink(f), sevIcon(f.Severity), f.Severity, ownerBadge(f.Owner), controlLinks(f.Controls))
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}
