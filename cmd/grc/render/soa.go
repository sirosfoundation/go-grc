package render

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sirosfoundation/go-grc/pkg/config"
	"github.com/sirosfoundation/go-grc/pkg/soa"
)

// generateSoAs renders one page per Statement of Applicability, plus an
// index, under audits/. Statements are part of a real audit and are public;
// the evidence column points into the (possibly private) repository, so it
// is shown on the private profile only.
func generateSoAs(cfg *config.Config, soas []*soa.Loaded, isPublic bool) error {
	seen := map[string]string{}
	for _, s := range soas {
		slug := cycleSlug(s.Cycle)
		if slug == "" || slug == "index" {
			return fmt.Errorf("audit cycle %q has no usable page name", s.Cycle)
		}
		if other, ok := seen[slug]; ok {
			return fmt.Errorf("audit cycles %q and %q map to the same page %q", other, s.Cycle, slug)
		}
		seen[slug] = s.Cycle
	}
	dir := filepath.Join(cfg.SiteDir, "audits")
	if err := writePage(filepath.Join(dir, "_category_.json"), categoryJSON("Audits", 4)); err != nil {
		return err
	}
	if err := writePage(filepath.Join(dir, "index.md"), renderSoAIndex(soas)); err != nil {
		return err
	}
	for _, s := range soas {
		if err := writePage(filepath.Join(dir, cycleSlug(s.Cycle)+".md"), renderSoAPage(s, isPublic)); err != nil {
			return err
		}
	}
	return nil
}

func renderSoAIndex(soas []*soa.Loaded) string {
	var b strings.Builder
	b.WriteString("---\ntitle: \"Audits\"\nsidebar_label: \"Overview\"\nsidebar_position: 0\n---\n\n# Audits\n\n")
	b.WriteString("Statements of Applicability for audit cycles.\n\n| Statement | Status | Version | Date |\n|---|---|---|---|\n")
	for _, s := range soas {
		h := s.SoA.Header
		title := h.Title
		if title == "" {
			title = h.ID
		}
		fmt.Fprintf(&b, "| [%s](/audits/%s) | %s | %s | %s |\n", cell(title), cycleSlug(s.Cycle), stateLabel(s.SoA.State()), cell(h.Version), cell(h.Date))
	}
	return b.String()
}

func renderSoAPage(s *soa.Loaded, isPublic bool) string {
	h := s.SoA.Header
	title := h.Title
	if title == "" {
		title = h.ID
	}
	var b strings.Builder
	fmt.Fprintf(&b, "---\ntitle: %q\nsidebar_label: %q\n---\n\n# %s\n\n", title, h.ID, title)

	if !s.SoA.Published() {
		fmt.Fprintf(&b, ":::caution %s\nThis statement has not been through its review and is internal. It is not published on the public site.\n:::\n\n", stateLabel(s.SoA.State()))
	}
	b.WriteString("| | |\n|---|---|\n")
	fmt.Fprintf(&b, "| **Status** | %s |\n", stateLabel(s.SoA.State()))
	if h.ApprovedDate != "" {
		fmt.Fprintf(&b, "| **Approved** | %s |\n", cell(h.ApprovedDate))
	}
	if h.Version != "" {
		fmt.Fprintf(&b, "| **Version** | %s |\n", cell(h.Version))
	}
	if h.Date != "" {
		fmt.Fprintf(&b, "| **Date** | %s |\n", cell(h.Date))
	}
	if h.Assurance != "" {
		fmt.Fprintf(&b, "| **Assurance** | %s |\n", cell(h.Assurance))
	}
	if h.ScopeRef != "" {
		fmt.Fprintf(&b, "| **Scope** | %s |\n", cell(h.ScopeRef))
	}

	var applicable, partial, notApplicable, implemented int
	for _, r := range s.SoA.Requirements {
		switch r.Applicable {
		case soa.Applicable:
			applicable++
		case soa.Partial:
			partial++
		case soa.NotApplicable:
			notApplicable++
		}
		if r.Status == "implemented" {
			implemented++
		}
	}
	fmt.Fprintf(&b, "\n%d controls: %d applicable, %d partially applicable, %d not applicable; %d implemented.\n\n",
		len(s.SoA.Requirements), applicable, partial, notApplicable, implemented)

	b.WriteString("| Control | Title | Applicable | Status | Justification |")
	sep := "|---|---|---|---|---|"
	if !isPublic {
		b.WriteString(" Evidence |")
		sep += "---|"
	}
	b.WriteString("\n" + sep + "\n")
	for _, r := range s.SoA.Requirements {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |", cell(r.AnnexA), cell(r.Title), cell(string(r.Applicable)), cell(r.Status), cell(r.Justification))
		if !isPublic {
			ev := make([]string, len(r.Evidence))
			for i, e := range r.Evidence {
				ev[i] = "`" + strings.ReplaceAll(e, "|", "\\|") + "`"
			}
			fmt.Fprintf(&b, " %s |", strings.Join(ev, "<br/>"))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func stateLabel(state string) string {
	switch state {
	case soa.StatusApproved:
		return "Approved"
	case soa.StatusInReview:
		return "In review"
	default:
		return "Draft"
	}
}

// publishable returns the statements that may appear on the given profile:
// every statement on the private site, only approved ones on the public site.
func publishable(soas []*soa.Loaded, isPublic bool) []*soa.Loaded {
	if !isPublic {
		return soas
	}
	var out []*soa.Loaded
	for _, s := range soas {
		if s.SoA.Published() {
			out = append(out, s)
		}
	}
	return out
}

// cell makes s safe inside a Markdown (MDX) table cell.
func cell(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.NewReplacer("|", "\\|", "<", "&lt;", ">", "&gt;", "{", "&#123;", "}", "&#125;").Replace(s)
	return s
}

// cycleSlug is the URL slug of an audit cycle directory name: lowercase,
// with runs of anything but letters and digits collapsed to "-".
func cycleSlug(cycle string) string {
	return strings.Trim(nonAlnum.ReplaceAllString(strings.ToLower(cycle), "-"), "-")
}
