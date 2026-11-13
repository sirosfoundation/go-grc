package render

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/sirosfoundation/go-grc/pkg/audit"
	"github.com/sirosfoundation/go-grc/pkg/catalog"
	"github.com/sirosfoundation/go-grc/pkg/config"
	"github.com/sirosfoundation/go-grc/pkg/mapping"
	"github.com/sirosfoundation/go-grc/pkg/soa"
)

// matrixRepo is the project's GitHub repository (org/name), used to link
// evidence files. Set for the private profile before the audit pages render.
var matrixRepo string

// statusNotAssessed marks a requirement of a catalog-derived statement that
// has not been assessed. It never appears in an explicit soa.yaml.
const statusNotAssessed = "not_assessed"

// matrixReq is one requirement of the matrix, from an explicit statement of
// applicability or derived from an assessment's catalogs.
type matrixReq struct {
	ID            string
	Title         string
	Group         string
	Anchor        string
	Applicable    soa.Applicability
	Status        string
	Justification string
	Evidence      []string // repository paths, URLs or plain text
	Controls      []string // catalog control ids (catalog-derived only)
	FwID          string   // framework id (catalog-derived only)
	URL           string   // requirement page, when one exists
	Broader       *soa.BroaderScope
}

// matrixPage is everything the matrix page needs.
type matrixPage struct {
	Title    string
	State    string
	Reqs     []matrixReq
	Findings []*audit.Finding
	Repo     string
	SoAPath  string // the plain SoA table page, when there is one
	Implicit bool
}

var annexARe = regexp.MustCompile(`^A\.([5-8])\.`)

var annexThemes = map[string]string{
	"5": "5 Organizational controls",
	"6": "6 People controls",
	"7": "7 Physical controls",
	"8": "8 Technological controls",
}

// groupOf names the group of a requirement: its section, else the ISO 27001
// Annex A theme of its id, else none.
func groupOf(section, id string) string {
	if section != "" {
		return section
	}
	if m := annexARe.FindStringSubmatch(id); m != nil {
		return annexThemes[m[1]]
	}
	return ""
}

// matrixFromSoA is the model of an explicit statement.
func matrixFromSoA(s *soa.SoA) []matrixReq {
	out := make([]matrixReq, 0, len(s.Requirements))
	for _, r := range s.Requirements {
		out = append(out, matrixReq{
			ID: r.AnnexA, Title: r.Title, Group: groupOf(r.Section, r.AnnexA), Anchor: "r-" + entrySlug(r.AnnexA),
			Applicable: r.Applicable, Status: r.Status, Justification: r.Justification,
			Evidence: r.Evidence, Broader: r.BroaderScope,
		})
	}
	return out
}

// coverageToStatus maps the status vocabularies of the framework mappings
// (coverage, result, mitigation) onto the statement's.
func coverageToStatus(s string) string {
	switch s {
	case "full", "compliant", "implemented", "done":
		return "implemented"
	case "partial", "partially_compliant":
		return "partial"
	case "none", "non_compliant", "gap":
		return "gap"
	case "not_applicable":
		return "not_applicable"
	}
	return statusNotAssessed
}

// matrixFromCatalogs derives the implicit statement of an assessment: every
// requirement of every catalog it lists, as the framework mapping assesses it.
func matrixFromCatalogs(catalogs []string, fws []config.FrameworkConfig, maps mapping.Mappings, fwCats map[string]*catalog.FrameworkCatalog) []matrixReq {
	var out []matrixReq
	for _, id := range catalogs {
		var fw *config.FrameworkConfig
		for i := range fws {
			if fws[i].ID == id {
				fw = &fws[i]
			}
		}
		fm := maps[id]
		if fw == nil || fm == nil {
			continue
		}
		for _, e := range fm.Entries {
			r := matrixReq{
				ID: e.Key, Title: e.Key, Anchor: "r-" + entrySlug(id+"_"+e.Key), FwID: id,
				Applicable: soa.Applicable, Status: coverageToStatus(e.Status),
				Justification: e.Notes, Controls: e.Controls,
			}
			section := ""
			if c := fwCats[id]; c != nil {
				if cr := c.ByID[e.Key]; cr != nil {
					r.Title, section = cr.Title, cr.Section
				}
			}
			if r.Status == "not_applicable" {
				r.Applicable = soa.NotApplicable
			}
			// Annex A ids group by theme; other catalogs by their own section.
			if r.Group = groupOf("", e.Key); r.Group == "" {
				r.Group = section
			}
			if len(catalogs) > 1 {
				r.Group = strings.TrimSpace(fw.Name + " / " + r.Group)
				r.Group = strings.TrimSuffix(r.Group, " /")
			}
			if urls := frameworkURLs[id]; urls != nil {
				r.URL = urls[e.Key]
			}
			out = append(out, r)
		}
	}
	return out
}

// attachMatrices fills in the matrix model of every audit and assessment.
func attachMatrices(audits []*auditView, assessments []*assessmentView, fws []config.FrameworkConfig, maps mapping.Mappings, fwCats map[string]*catalog.FrameworkCatalog) {
	for _, v := range audits {
		if v.SoA != nil {
			v.Matrix = matrixFromSoA(&v.SoA.SoA)
		}
	}
	for _, v := range assessments {
		if v.SoA != nil {
			v.Matrix = matrixFromSoA(&v.SoA.SoA)
		} else {
			v.Matrix = matrixFromCatalogs(v.Catalogs, fws, maps, fwCats)
		}
	}
}

// notApplicable reports whether the requirement is out of scope.
func (r *matrixReq) notApplicable() bool {
	return r.Applicable == soa.NotApplicable || r.Status == "not_applicable"
}

// effective is the status the matrix shows: not applicable wins.
func (r *matrixReq) effective() string {
	if r.notApplicable() {
		return "not_applicable"
	}
	return r.Status
}

func (r *matrixReq) isGap() bool {
	return !r.notApplicable() && (r.Status == "gap" || r.Status == "partial")
}

var matrixStatuses = []struct{ key, label, icon, bg, fg string }{
	{"implemented", "Implemented", "✓", "#2e7d32", "#ffffff"},
	{"partial", "Partial", "◐", "#f9a825", "#1a1a1a"},
	{"gap", "Gap", "✗", "#c62828", "#ffffff"},
	{statusNotAssessed, "Not assessed", "?", "#eceff1", "#37474f"},
	{"not_applicable", "Not applicable", "–", "#9e9e9e", "#ffffff"},
}

func statusStyle(key string) (label, icon, bg, fg string) {
	for _, s := range matrixStatuses {
		if s.key == key {
			return s.label, s.icon, s.bg, s.fg
		}
	}
	return key, "?", "#eceff1", "#37474f"
}

// matrixCell renders one compact cell. The inline style makes it readable
// without any site CSS; the classes let the site restyle it.
func matrixCell(r *matrixReq, label string, status string, app soa.Applicability, delta, tip string) string {
	_, icon, bg, fg := statusStyle(status)
	extra := ""
	switch {
	case status == "not_applicable":
		extra = "opacity:.45;"
	case app == soa.Partial:
		extra = "border:2px dashed #1a1a1a;"
	}
	marker := ""
	if delta != "" && delta != soa.DeltaNone {
		marker = " ▲"
	}
	return fmt.Sprintf(`<a href="#%s" class="soa-cell soa-%s" title="%s" style="display:inline-block;min-width:3.2em;padding:2px 6px;border-radius:4px;text-align:center;text-decoration:none;font-size:.8em;background:%s;color:%s;%s">%s %s%s</a>`,
		r.Anchor, status, html.EscapeString(tip), bg, fg, extra, icon, html.EscapeString(label), marker)
}

func shortID(id string) string { return strings.TrimPrefix(id, "A.") }

func cellTip(r *matrixReq, status string, app soa.Applicability, extra string) string {
	label, _, _, _ := statusStyle(status)
	tip := r.ID + " " + r.Title + " — " + label
	if app == soa.Partial {
		tip += ", partially applicable"
	}
	if extra != "" {
		tip += " (" + extra + ")"
	}
	return tip
}

// evidenceLink renders one evidence reference: a policy page, a repository
// file, a URL, or plain text.
func evidenceLink(e, repo string) string {
	path, _, _ := strings.Cut(e, "#")
	switch {
	case strings.HasPrefix(e, "http://") || strings.HasPrefix(e, "https://"):
		return fmt.Sprintf("[%s](%s)", cell(e), e)
	case strings.HasPrefix(path, "policies/") && strings.HasSuffix(path, ".md") && !strings.Contains(strings.TrimPrefix(path, "policies/"), "/"):
		slug := strings.TrimSuffix(strings.TrimPrefix(path, "policies/"), ".md")
		return fmt.Sprintf("[`%s`](/policies/%s)", strings.ReplaceAll(e, "|", "\\|"), slug)
	case repo != "" && looksLikePath(path):
		return fmt.Sprintf("[`%s`](https://github.com/%s/blob/main/%s)", strings.ReplaceAll(e, "|", "\\|"), repo, strings.ReplaceAll(path, " ", "%20"))
	}
	return "`" + strings.ReplaceAll(e, "|", "\\|") + "`"
}

func looksLikePath(p string) bool {
	return strings.Contains(p, "/") && !strings.ContainsAny(p, " ()<>\"") && !strings.HasPrefix(p, "/") && !strings.Contains(p, "..")
}

// mentionsID reports whether text names the requirement id as a whole token,
// so that A.5.1 does not match A.5.10 or A.5.1.2.
func mentionsID(text, id string) bool {
	if id == "" {
		return false
	}
	re := regexp.MustCompile(`(?:^|[^A-Za-z0-9._-])` + regexp.QuoteMeta(id) + `(?:$|[^A-Za-z0-9_.-]|\.(?:[^A-Za-z0-9]|$))`)
	return re.MatchString(text)
}

// findingsFor returns the findings that reference the requirement: through
// the framework references, the finding's controls, or by naming its id.
func findingsFor(r *matrixReq, fs []*audit.Finding) []*audit.Finding {
	var out []*audit.Finding
	for _, f := range fs {
		if findingReferences(f, r) {
			out = append(out, f)
		}
	}
	return out
}

func findingReferences(f *audit.Finding, r *matrixReq) bool {
	if r.FwID != "" && f.MatchesReq(r.FwID, r.ID) {
		return true
	}
	if annexARe.MatchString(r.ID) && f.MatchesReq("iso27001", r.ID) {
		return true
	}
	for _, c := range f.Controls {
		if c == r.ID {
			return true
		}
		for _, rc := range r.Controls {
			if c == rc {
				return true
			}
		}
	}
	return mentionsID(f.Title+"\n"+f.Description, r.ID)
}

// findingCell lists findings with their tracking issues.
func findingCell(fs []*audit.Finding) string {
	parts := make([]string, 0, len(fs))
	for _, f := range fs {
		s := fmt.Sprintf("%s %s", sevIcon(f.Severity), findingRef(f))
		if f.TrackingIssue != nil {
			s += " " + issueRef(f)
		}
		if f.Status != "" {
			s += " (" + strings.ReplaceAll(f.Status, "_", " ") + ")"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "<br/>")
}

func renderSoAMatrixPage(p matrixPage) string {
	var b strings.Builder
	title := p.Title + " — matrix"
	fmt.Fprintf(&b, "---\ntitle: %q\nsidebar_label: \"SoA matrix\"\n---\n\n# %s\n\n", title, cell(title))
	b.WriteString(":::caution Internal\nThe matrix view is part of the private site only.\n:::\n\n")
	if p.Implicit {
		b.WriteString("This assessment has no explicit statement of applicability; every requirement of its catalogs is shown as assessed in the framework mapping.\n\n")
	} else if p.State != "" {
		fmt.Fprintf(&b, "Status: %s", p.State)
		if p.SoAPath != "" {
			fmt.Fprintf(&b, " · [plain statement table](%s)", p.SoAPath)
		}
		b.WriteString("\n\n")
	}

	// Group, keeping first-seen order.
	var order []string
	groups := map[string][]*matrixReq{}
	broader := false
	for i := range p.Reqs {
		r := &p.Reqs[i]
		if r.Broader != nil {
			broader = true
		}
		g := r.Group
		if _, ok := groups[g]; !ok {
			order = append(order, g)
		}
		groups[g] = append(groups[g], r)
	}
	if len(p.Reqs) == 0 {
		b.WriteString("No requirements.\n")
		return b.String()
	}
	groupName := func(g string) string {
		if g == "" {
			return "Requirements"
		}
		return g
	}

	// Legend and summary.
	b.WriteString("**Legend:** ")
	var lg []string
	for _, s := range matrixStatuses {
		_, icon, bg, fg := statusStyle(s.key)
		lg = append(lg, fmt.Sprintf(`<span class="soa-cell soa-%s" style="padding:2px 6px;border-radius:4px;font-size:.8em;background:%s;color:%s">%s %s</span>`, s.key, bg, fg, icon, s.label))
	}
	b.WriteString(strings.Join(lg, " "))
	b.WriteString(` <span style="font-size:.8em;border:2px dashed #1a1a1a;padding:0 6px;border-radius:4px">dashed = partially applicable</span>`)
	if broader {
		b.WriteString(" ▲ = differs in the broader scope")
	}
	b.WriteString("\n\n")

	b.WriteString("## Summary\n\n")
	summary := func(pick func(*matrixReq) string) {
		b.WriteString("| Group |")
		sep := "|---|"
		for _, s := range matrixStatuses {
			fmt.Fprintf(&b, " %s |", s.label)
			sep += "---|"
		}
		b.WriteString(" Total |\n" + sep + "---|\n")
		totals := map[string]int{}
		for _, g := range order {
			counts := map[string]int{}
			for _, r := range groups[g] {
				counts[pick(r)]++
				totals[pick(r)]++
			}
			fmt.Fprintf(&b, "| %s |", cell(groupName(g)))
			for _, s := range matrixStatuses {
				fmt.Fprintf(&b, " %d |", counts[s.key])
			}
			fmt.Fprintf(&b, " %d |\n", len(groups[g]))
		}
		b.WriteString("| **All** |")
		for _, s := range matrixStatuses {
			fmt.Fprintf(&b, " **%d** |", totals[s.key])
		}
		fmt.Fprintf(&b, " **%d** |\n\n", len(p.Reqs))
	}
	summary(func(r *matrixReq) string { return r.effective() })
	if broader {
		b.WriteString("Broader scope (including the deployment platform; requirements without broader-scope data count as in the main scope):\n\n")
		summary(func(r *matrixReq) string {
			if r.Broader == nil {
				return r.effective()
			}
			rr := *r
			if r.Broader.Status != "" {
				rr.Status = r.Broader.Status
			}
			if r.Broader.Applicable != "" {
				rr.Applicable = r.Broader.Applicable
			}
			return rr.effective()
		})
	}

	// Matrix.
	b.WriteString("## Matrix\n\n")
	for _, g := range order {
		fmt.Fprintf(&b, "### %s\n\n", cell(groupName(g)))
		rs := groups[g]
		if broader {
			b.WriteString(`<div><strong>Main scope</strong></div>` + "\n")
		}
		b.WriteString(`<div class="soa-row" style="display:flex;flex-wrap:wrap;gap:4px;margin-bottom:8px">`)
		for _, r := range rs {
			b.WriteString(matrixCell(r, shortID(r.ID), r.effective(), r.Applicable, "", cellTip(r, r.effective(), r.Applicable, "")))
		}
		b.WriteString("</div>\n\n")
		if broader {
			b.WriteString(`<div><strong>Broader scope (incl. deployment platform)</strong></div>` + "\n")
			b.WriteString(`<div class="soa-row soa-broader" style="display:flex;flex-wrap:wrap;gap:4px;margin-bottom:8px">`)
			for _, r := range rs {
				bs := r.Broader
				if bs == nil {
					b.WriteString(matrixCell(r, shortID(r.ID), r.effective(), r.Applicable, "", cellTip(r, r.effective(), r.Applicable, "same as main scope")))
					continue
				}
				rr := *r
				if bs.Status != "" {
					rr.Status = bs.Status
				}
				if bs.Applicable != "" {
					rr.Applicable = bs.Applicable
				}
				extra := bs.Delta
				if bs.Note != "" {
					extra += ": " + bs.Note
				}
				b.WriteString(matrixCell(r, shortID(r.ID), rr.effective(), rr.Applicable, bs.Delta, cellTip(r, rr.effective(), rr.Applicable, extra)))
			}
			b.WriteString("</div>\n\n")
		}
	}

	// Applicable requirements.
	nApplicable := 0
	for i := range p.Reqs {
		if !p.Reqs[i].notApplicable() {
			nApplicable++
		}
	}
	fmt.Fprintf(&b, "## Applicable requirements (%d)\n\n", nApplicable)
	for _, g := range order {
		var rs []*matrixReq
		for _, r := range groups[g] {
			if !r.notApplicable() {
				rs = append(rs, r)
			}
		}
		if len(rs) == 0 {
			continue
		}
		fmt.Fprintf(&b, "### %s\n\n", cell(groupName(g)))
		b.WriteString("| Requirement | Title | Applicable | Status | Notes | Evidence | Findings and issues |")
		sep := "|---|---|---|---|---|---|---|"
		if broader {
			b.WriteString(" Broader scope |")
			sep += "---|"
		}
		b.WriteString("\n" + sep + "\n")
		for _, r := range rs {
			label, icon, _, _ := statusStyle(r.Status)
			st := icon + " " + label
			if r.isGap() {
				st = "⚠ " + st
			}
			id := cell(r.ID)
			if r.URL != "" {
				id = fmt.Sprintf("[%s](%s)", id, r.URL)
			}
			fmt.Fprintf(&b, "| <a id=%q></a>%s | %s | %s | %s | %s | %s | %s |", r.Anchor, id, cell(r.Title), cell(string(r.Applicable)), st, cell(r.Justification), evidenceCell(r, p.Repo), findingCell(findingsFor(r, p.Findings)))
			if broader {
				fmt.Fprintf(&b, " %s |", broaderCell(r))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Not applicable requirements, collapsed.
	var na []*matrixReq
	for i := range p.Reqs {
		if p.Reqs[i].notApplicable() {
			na = append(na, &p.Reqs[i])
		}
	}
	if len(na) > 0 {
		fmt.Fprintf(&b, "<details>\n<summary>Not applicable requirements (%d)</summary>\n\n", len(na))
		b.WriteString("| Requirement | Title | Justification |\n|---|---|---|\n")
		for _, r := range na {
			fmt.Fprintf(&b, "| <a id=%q></a>%s | %s | %s |\n", r.Anchor, cell(r.ID), cell(r.Title), cell(r.Justification))
		}
		b.WriteString("\n</details>\n")
	}
	return b.String()
}

func evidenceCell(r *matrixReq, repo string) string {
	parts := make([]string, 0, len(r.Evidence)+len(r.Controls))
	for _, e := range r.Evidence {
		parts = append(parts, evidenceLink(e, repo))
	}
	for _, c := range r.Controls {
		parts = append(parts, controlLink(c))
	}
	return strings.Join(parts, "<br/>")
}

func broaderCell(r *matrixReq) string {
	bs := r.Broader
	if bs == nil {
		return ""
	}
	var parts []string
	if bs.Status != "" {
		label, icon, _, _ := statusStyle(bs.Status)
		parts = append(parts, icon+" "+label)
	}
	if bs.Applicable != "" {
		parts = append(parts, "applicable: "+string(bs.Applicable))
	}
	if bs.Delta != "" && bs.Delta != soa.DeltaNone {
		parts = append(parts, "▲ "+bs.Delta)
	}
	s := strings.Join(parts, "; ")
	if bs.Note != "" {
		s += " — " + cell(bs.Note)
	}
	return s
}
