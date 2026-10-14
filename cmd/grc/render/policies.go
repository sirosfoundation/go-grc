package render

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/sirosfoundation/go-grc/pkg/config"
	"github.com/sirosfoundation/go-grc/pkg/policy"
)

// visiblePolicies selects what a profile may show. Visibility alone decides
// publication; status is only displayed.
func visiblePolicies(all []*policy.Policy, isPublic bool) []*policy.Policy {
	if !isPublic {
		return all
	}
	var out []*policy.Policy
	for _, p := range all {
		if p.Public() {
			out = append(out, p)
		}
	}
	return out
}

func generatePolicies(cfg *config.Config, pols []*policy.Policy, isPublic bool) error {
	dir := filepath.Join(cfg.SiteDir, "policies")
	if err := writePage(filepath.Join(dir, "_category_.json"), categoryJSON("Policies", 5)); err != nil {
		return err
	}
	if err := writePage(filepath.Join(dir, "index.md"), renderPolicyIndex(pols, isPublic)); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, p := range pols {
		if p.Slug == "index" || seen[p.Slug] {
			return fmt.Errorf("policy %s: slug %q collides with another page", p.Path, p.Slug)
		}
		seen[p.Slug] = true
		page, err := renderPolicyPage(p, isPublic)
		if err != nil {
			return err
		}
		if err := writePage(filepath.Join(dir, p.Slug+".md"), page); err != nil {
			return err
		}
	}
	return nil
}

func renderPolicyIndex(pols []*policy.Policy, isPublic bool) string {
	var b strings.Builder
	b.WriteString("---\ntitle: \"Policies\"\nsidebar_label: \"Overview\"\nsidebar_position: 0\n---\n\n# Policies\n\n")
	if isPublic {
		b.WriteString("| Policy | Status |\n|---|---|\n")
	} else {
		b.WriteString("| Policy | Status | Visibility |\n|---|---|---|\n")
	}
	for _, p := range pols {
		if isPublic {
			fmt.Fprintf(&b, "| [%s](/policies/%s) | %s |\n", cell(p.Title), p.Slug, stateLabel(p.State()))
		} else {
			fmt.Fprintf(&b, "| [%s](/policies/%s) | %s | %s |\n", cell(p.Title), p.Slug, stateLabel(p.State()), visibilityLabel(p))
		}
	}
	return b.String()
}

func visibilityLabel(p *policy.Policy) string {
	if p.Public() {
		return "public"
	}
	return "internal"
}

// reH1 matches the first top-level heading line.
var reH1 = regexp.MustCompile(`(?m)^#\s+.*$`)

func renderPolicyPage(p *policy.Policy, isPublic bool) (string, error) {
	meta := map[string]any{}
	for k, v := range p.Meta {
		meta[k] = v
	}
	meta["title"] = p.Title
	if _, ok := meta["sidebar_label"]; !ok {
		meta["sidebar_label"] = p.Title
	}
	fm, err := yaml.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("policy %s: %w", p.Path, err)
	}
	body := p.Body
	widget := policyWidget(p, isPublic)
	if loc := reH1.FindStringIndex(body); loc != nil {
		body = body[:loc[1]] + "\n\n" + widget + body[loc[1]:]
	} else {
		body = "# " + p.Title + "\n\n" + widget + "\n" + body
	}
	return "---\n" + string(fm) + "---\n" + body, nil
}

// policyWidget is the status box shown on every policy.
func policyWidget(p *policy.Policy, isPublic bool) string {
	kind := map[string]string{
		policy.StatusApproved: "tip",
		policy.StatusInReview: "info",
		policy.StatusDraft:    "caution",
	}[p.State()]
	line := "**Status:** " + stateLabel(p.State())
	if p.State() == policy.StatusApproved && p.ApprovedDate != "" {
		line += " · approved " + p.ApprovedDate
	}
	if !isPublic {
		line += " · **Visibility:** " + visibilityLabel(p)
	}
	return fmt.Sprintf(":::%s Policy status\n%s\n:::\n", kind, line)
}
