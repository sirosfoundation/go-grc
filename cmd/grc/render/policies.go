package render

import (
	"context"
	"fmt"
	"html"
	"path/filepath"
	"regexp"
	"strings"
	"time"

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
	changes := map[string]*policy.Change{}
	revisions := map[string]int{}
	for _, p := range pols {
		changes[p.Slug] = policy.LastChange(context.Background(), p.Path)
		revisions[p.Slug] = policy.Revisions(context.Background(), p.Path)
	}
	if err := writePage(filepath.Join(dir, "index.md"), renderPolicyIndex(pols, changes, cfg.Project.Repo, isPublic)); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, p := range pols {
		if p.Slug == "index" || seen[p.Slug] {
			return fmt.Errorf("policy %s: slug %q collides with another page", p.Path, p.Slug)
		}
		seen[p.Slug] = true
		page, err := renderPolicyPage(p, changes[p.Slug], revisions[p.Slug], cfg.Project.Repo, isPublic, time.Now())
		if err != nil {
			return err
		}
		if err := writePage(filepath.Join(dir, p.Slug+".md"), page); err != nil {
			return err
		}
	}
	return nil
}

// renderPolicyIndex lists the policies. The private profile splits them into
// public and private tables, so it is clear at a glance what is published.
func renderPolicyIndex(pols []*policy.Policy, changes map[string]*policy.Change, repo string, isPublic bool) string {
	var b strings.Builder
	b.WriteString("---\ntitle: \"Policies\"\nsidebar_label: \"Overview\"\nsidebar_position: 0\n---\n\n# Policies\n\n")
	var pub, priv []*policy.Policy
	for _, p := range pols {
		if p.Public() {
			pub = append(pub, p)
		} else {
			priv = append(priv, p)
		}
	}
	if isPublic {
		b.WriteString(policyTable(pub, changes, repo, isPublic))
		return b.String()
	}
	for _, g := range []struct {
		title string
		intro string
		pols  []*policy.Policy
	}{
		{"Public policies", "Published on the public site.", pub},
		{"Private policies", "Internal only; never published.", priv},
	} {
		if len(g.pols) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n%s\n\n%s\n", g.title, g.intro, policyTable(g.pols, changes, repo, isPublic))
	}
	return b.String()
}

func policyTable(pols []*policy.Policy, changes map[string]*policy.Change, repo string, isPublic bool) string {
	var b strings.Builder
	b.WriteString("| Policy | Status | Last change |\n|---|---|---|\n")
	for _, p := range pols {
		fmt.Fprintf(&b, "| [%s](/policies/%s) | %s | %s |\n", cell(p.Title), p.Slug, stateLabel(p.State()), lastChangeCell(changes[p.Slug], repo, isPublic))
	}
	return b.String()
}

// lastChangeCell shows when a policy last changed. The private profile links
// the commit; the public one shows the time only, since the repository is not
// public and the link would not resolve for its readers.
func lastChangeCell(c *policy.Change, repo string, isPublic bool) string {
	if c == nil {
		return "–"
	}
	stamp := c.Time.Format("2006-01-02 15:04") + " UTC"
	if isPublic || repo == "" {
		return stamp
	}
	return fmt.Sprintf("[%s](https://github.com/%s/commit/%s)", stamp, repo, c.SHA)
}

func visibilityLabel(p *policy.Policy) string {
	if p.Public() {
		return "public"
	}
	return "internal"
}

// reH1 matches the first top-level heading line.
var reH1 = regexp.MustCompile(`(?m)^#\s+.*$`)

func renderPolicyPage(p *policy.Policy, change *policy.Change, revisions int, repo string, isPublic bool, now time.Time) (string, error) {
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
	card := policyCard(p, change, revisions, repo, isPublic, now)
	if loc := reH1.FindStringIndex(body); loc != nil {
		// The card goes before the heading so that it floats beside it.
		body = body[:loc[0]] + card + "\n" + body[loc[0]:loc[1]] + "\n\n" + widget + body[loc[1]:]
	} else {
		body = card + "\n# " + p.Title + "\n\n" + widget + "\n" + body
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

// policyCard is the document metadata box shown at the top right of a policy,
// generated from front matter and git so that it cannot disagree with them.
// The public profile shows only the owner role, the version and the
// publication date.
func policyCard(p *policy.Policy, c *policy.Change, revisions int, repo string, isPublic bool, now time.Time) string {
	var rows [][2]string
	add := func(k, v string) {
		if v != "" {
			rows = append(rows, [2]string{k, v})
		}
	}
	esc := html.EscapeString
	version := esc(p.Version)
	if isPublic {
		add("Owner", esc(p.Owner))
		add("Version", version)
		add("Published", esc(p.ApprovedDate))
	} else {
		add("Document ID", esc(p.DocID))
		if version == "" {
			version = "Draft"
			if revisions > 0 {
				version += fmt.Sprintf(" · revision %d", revisions)
			}
		}
		add("Version", version)
		add("Owner", esc(p.Owner))
		add("Approver", esc(p.Approver))
		add("Approved", esc(p.ApprovedDate))
		add("Review cycle", esc(p.ReviewCycle))
		if next, ok := p.NextReview(); ok {
			label := next.Format("2006-01-02")
			if now.After(next) {
				label += " (overdue)"
			}
			add("Next review", label)
		}
		if c != nil {
			stamp := c.Time.Format("2006-01-02 15:04") + " UTC"
			if repo != "" {
				add("Last change", fmt.Sprintf(`<a href="https://github.com/%s/commit/%s">%s</a>`, esc(repo), esc(c.SHA), stamp))
			} else {
				add("Last change", stamp)
			}
		}
	}
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<aside class=\"doc-card\">\n<dl>\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "<dt>%s</dt><dd>%s</dd>\n", r[0], r[1])
	}
	b.WriteString("</dl>\n</aside>\n")
	return b.String()
}
