package render

import (
	"fmt"
	"strings"

	"github.com/sirosfoundation/go-grc/pkg/soa"
)

func renderSoAPage(s *soa.Loaded, isPublic bool) string {
	h := s.SoA.Header
	title := h.Title
	if title == "" {
		title = h.ID
	}
	var b strings.Builder
	fmt.Fprintf(&b, "---\ntitle: %q\nsidebar_label: %q\n---\n\n# %s\n\n", title, h.ID, title)

	switch {
	case s.Kind == soa.KindAssessment:
		b.WriteString(":::caution Internal assessment\nThis statement belongs to an internal assessment. It is never published on the public site.\n:::\n\n")
	case !s.SoA.Published():
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
