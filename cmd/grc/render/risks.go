package render

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sirosfoundation/go-grc/pkg/config"
	"github.com/sirosfoundation/go-grc/pkg/risk"
)

func generateRiskRegister(cfg *config.Config, risks *risk.RiskSet) error {
	dir := filepath.Join(cfg.SiteDir, "risk-register")
	writePage(filepath.Join(dir, "_category_.json"), categoryJSON("Risk Register", 7))
	writePage(filepath.Join(dir, "index.md"), renderRiskIndex(risks))

	for _, file := range risks.Files {
		for _, r := range file.Data.Risks {
			slug := idSlug(r.ID)
			writePage(filepath.Join(dir, slug+".md"), renderRiskPage(&r, file.Data.Register.Owner))
		}
	}
	return nil
}

func renderRiskIndex(risks *risk.RiskSet) string {
	var b strings.Builder
	b.WriteString("---\nsidebar_label: Risk Register\nsidebar_position: 1\ntitle: Risk Register\n---\n\n# Risk Register\n\n")
	b.WriteString("Accepted risks with compensating controls and residual risk assessment.\n\n")

	for _, file := range risks.Files {
		reg := file.Data.Register
		fmt.Fprintf(&b, "## %s\n\n", reg.Title)
		fmt.Fprintf(&b, "**Owner:** %s | **Last Review:** %s | **Next Review:** %s\n\n",
			ownerBadge(reg.Owner), reg.LastReview, reg.NextReview)

		if risk.IsOverdueRegister(reg) {
			b.WriteString(":::warning\nThis risk register is overdue for review.\n:::\n\n")
		}

		b.WriteString("| Risk | Finding | Severity | Residual | Status | Profiles |\n")
		b.WriteString("|------|---------|----------|----------|--------|----------|\n")
		for _, r := range file.Data.Risks {
			profiles := "all"
			if len(r.Profiles) > 0 {
				profiles = strings.Join(r.Profiles, ", ")
			}
			fmt.Fprintf(&b, "| [%s](/risk-register/%s) | %s | %s %s | %s %s | %s | %s |\n",
				r.ID, idSlug(r.ID), r.Finding,
				sevIcon(r.Severity), r.Severity,
				sevIcon(r.ResidualSeverity), r.ResidualSeverity,
				r.Status, profiles)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func renderRiskPage(r *risk.Risk, owner string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\ntitle: \"%s — %s\"\nsidebar_label: \"%s\"\n---\n\n", r.ID, r.Title, r.ID)
	fmt.Fprintf(&b, "# %s — %s\n\n", r.ID, r.Title)

	b.WriteString("| | |\n|---|---|\n")
	fmt.Fprintf(&b, "| **Finding** | %s |\n", r.Finding)
	fmt.Fprintf(&b, "| **Owner** | %s |\n", ownerBadge(owner))
	fmt.Fprintf(&b, "| **Original Severity** | %s %s |\n", sevIcon(r.Severity), r.Severity)
	fmt.Fprintf(&b, "| **Residual Severity** | %s %s |\n", sevIcon(r.ResidualSeverity), r.ResidualSeverity)
	fmt.Fprintf(&b, "| **Status** | %s |\n", r.Status)
	if len(r.Profiles) > 0 {
		fmt.Fprintf(&b, "| **Profiles** | %s |\n", strings.Join(r.Profiles, ", "))
	}
	fmt.Fprintf(&b, "| **Decision Date** | %s |\n", r.Decision.Date)
	fmt.Fprintf(&b, "| **Reviewer** | %s |\n", r.Decision.Reviewer)
	fmt.Fprintf(&b, "| **Review Interval** | %s |\n", r.Decision.ReviewInterval)
	if r.Tracking != nil {
		fmt.Fprintf(&b, "| **Tracking Issue** | [%s#%d](https://github.com/%s/issues/%d) |\n",
			r.Tracking.Repo, r.Tracking.Number, r.Tracking.Repo, r.Tracking.Number)
	}
	b.WriteString("\n")

	if r.Description != "" {
		fmt.Fprintf(&b, "## Description\n\n%s\n\n", r.Description)
	}

	if len(r.CompensatingControls) > 0 {
		b.WriteString("## Compensating Controls\n\n")
		for _, cc := range r.CompensatingControls {
			fmt.Fprintf(&b, "- %s\n", cc)
		}
		b.WriteString("\n")
	}

	if r.ResidualRisk != "" {
		fmt.Fprintf(&b, "## Residual Risk\n\n%s\n\n", r.ResidualRisk)
	}

	if r.Decision.Rationale != "" {
		fmt.Fprintf(&b, "## Decision Rationale\n\n%s\n\n", r.Decision.Rationale)
	}

	return b.String()
}
