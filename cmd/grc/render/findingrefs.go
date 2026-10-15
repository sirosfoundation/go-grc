package render

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/sirosfoundation/go-grc/pkg/audit"
)

// findingHasPage reports whether the private site has a page for the
// finding: it is tracked as an issue and not yet closed out.
func findingHasPage(f *audit.Finding) bool {
	return f.TrackingIssue != nil && !f.IsTerminal()
}

// findingRef links a finding to the most useful place: its page on the
// site, else its tracking issue, else just its ID.
func findingRef(f *audit.Finding) string {
	if findingHasPage(f) {
		return fmt.Sprintf("[%s](/findings/%s)", f.ID, idSlug(f.ID))
	}
	return findingLink(f)
}

// issueRef renders the tracking issue of a finding, or a dash.
func issueRef(f *audit.Finding) string {
	if f.TrackingIssue == nil {
		return "–"
	}
	return fmt.Sprintf("[#%d](https://github.com/%s/issues/%d)", f.TrackingIssue.Number, f.TrackingIssue.Repo, f.TrackingIssue.Number)
}

// sortedFindings returns the findings ordered by severity, then ID.
func sortedFindings(fs []audit.Finding) []*audit.Finding {
	out := make([]*audit.Finding, len(fs))
	for i := range fs {
		out[i] = &fs[i]
	}
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := sevRank(out[i].Severity), sevRank(out[j].Severity); a != b {
			return a > b
		}
		return naturalLess(out[i].ID, out[j].ID)
	})
	return out
}

// findingStatusSummary counts findings per status, e.g. "2 resolved, 1 open".
func findingStatusSummary(fs []*audit.Finding) string {
	counts := map[string]int{}
	for _, f := range fs {
		counts[f.Status]++
	}
	var parts []string
	for _, s := range []string{"open", "in_progress", "resolved", "accepted"} {
		if n := counts[s]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, strings.ReplaceAll(s, "_", " ")))
			delete(counts, s)
		}
	}
	var rest []string
	for s := range counts {
		rest = append(rest, s)
	}
	sort.Strings(rest)
	for _, s := range rest {
		parts = append(parts, fmt.Sprintf("%d %s", counts[s], s))
	}
	return strings.Join(parts, ", ")
}

// renderFindingTable renders findings for cross-reference: every finding of
// an audit or assessment, with its page and tracking issue.
func renderFindingTable(fs []*audit.Finding) string {
	var b strings.Builder
	b.WriteString("| Finding | Title | Severity | Status | Issue | Owner |\n|---|---|---|---|---|---|\n")
	for _, f := range fs {
		fmt.Fprintf(&b, "| %s | %s | %s %s | %s | %s | %s |\n",
			findingRef(f), cell(f.Title), sevIcon(f.Severity), f.Severity, f.Status, issueRef(f), ownerBadge(f.Owner))
	}
	return b.String()
}

// naturalLess orders IDs such as ISO-DEV-2 before ISO-DEV-10: the trailing
// number is compared numerically when the prefixes agree.
func naturalLess(a, b string) bool {
	split := func(s string) (string, int, bool) {
		i := strings.LastIndexAny(s, "-_")
		if i < 0 {
			return s, 0, false
		}
		n, err := strconv.Atoi(s[i+1:])
		return s[:i], n, err == nil
	}
	pa, na, oka := split(a)
	pb, nb, okb := split(b)
	if oka && okb && pa == pb {
		return na < nb
	}
	return a < b
}
