package render

import (
	"strings"
	"testing"
	"time"

	"github.com/sirosfoundation/go-grc/pkg/audit"
	"github.com/sirosfoundation/go-grc/pkg/catalog"
	"github.com/sirosfoundation/go-grc/pkg/config"
	"github.com/sirosfoundation/go-grc/pkg/mapping"
	"github.com/sirosfoundation/go-grc/pkg/soa"
)

func TestMentionsID(t *testing.T) {
	cases := []struct {
		text, id string
		want     bool
	}{
		{"see A.5.1 for details", "A.5.1", true},
		{"covers A.5.1.", "A.5.1", true},
		{"A.5.1", "A.5.1", true},
		{"(A.5.1)", "A.5.1", true},
		{"A.5.10 only", "A.5.1", false},
		{"A.5.1.2 only", "A.5.1", false},
		{"XA.5.1", "A.5.1", false},
		{"nothing", "A.5.1", false},
		{"anything", "", false},
	}
	for _, c := range cases {
		if got := mentionsID(c.text, c.id); got != c.want {
			t.Errorf("mentionsID(%q, %q) = %v, want %v", c.text, c.id, got, c.want)
		}
	}
}

func TestGroupOf(t *testing.T) {
	cases := []struct{ section, id, want string }{
		{"", "A.5.1", "5 Organizational controls"},
		{"", "A.6.3", "6 People controls"},
		{"", "A.7.14", "7 Physical controls"},
		{"", "A.8.28", "8 Technological controls"},
		{"Own", "A.5.1", "Own"},
		{"", "GEN-5-01", ""},
	}
	for _, c := range cases {
		if got := groupOf(c.section, c.id); got != c.want {
			t.Errorf("groupOf(%q, %q) = %q, want %q", c.section, c.id, got, c.want)
		}
	}
}

func TestCoverageToStatus(t *testing.T) {
	for in, want := range map[string]string{
		"full": "implemented", "compliant": "implemented", "partial": "partial", "partially_compliant": "partial",
		"none": "gap", "non_compliant": "gap", "not_applicable": "not_applicable", "not_assessed": "not_assessed", "": "not_assessed",
	} {
		if got := coverageToStatus(in); got != want {
			t.Errorf("coverageToStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEvidenceLink(t *testing.T) {
	cases := []struct{ in, want string }{
		{"policies/sdlc-policy.md", "[`policies/sdlc-policy.md`](/policies/sdlc-policy)"},
		{"policies/sdlc-policy.md#3.1", "[`policies/sdlc-policy.md#3.1`](/policies/sdlc-policy)"},
		{"https://example.org/x", "[https://example.org/x](https://example.org/x)"},
		{"evidence/a/b.png", "[`evidence/a/b.png`](https://github.com/org/repo/blob/main/evidence/a/b.png)"},
		{"just words here", "`just words here`"},
		{"../escape/x", "`../escape/x`"},
	}
	for _, c := range cases {
		if got := evidenceLink(c.in, "org/repo"); got != c.want {
			t.Errorf("evidenceLink(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func matrixFindings() []*audit.Finding {
	return []*audit.Finding{
		{ID: "F-1", Title: "Gap", Severity: "high", Status: "open", AnnexA: []string{"A.5.2"}, TrackingIssue: &audit.IssueRef{Repo: "o/r", Number: 7}},
		{ID: "F-2", Title: "Mentions A.8.1 in text", Severity: "low", Status: "open"},
		{ID: "F-3", Title: "Control based", Status: "open", Controls: []string{"SID-X-1"}},
		{ID: "F-4", Title: "Closed", Status: "resolved", AnnexA: []string{"A.5.1"}, TrackingIssue: &audit.IssueRef{Repo: "o/r", Number: 9}},
	}
}

func TestFindingsFor(t *testing.T) {
	cases := []struct {
		req  matrixReq
		want []string
	}{
		{matrixReq{ID: "A.5.2"}, []string{"F-1"}},
		{matrixReq{ID: "A.8.1"}, []string{"F-2"}},
		{matrixReq{ID: "A.8.10"}, nil},
		{matrixReq{ID: "R-9", Controls: []string{"SID-X-1"}}, []string{"F-3"}},
		{matrixReq{ID: "SID-X-1"}, []string{"F-3"}},
	}
	for _, c := range cases {
		var got []string
		for _, f := range findingsFor(&c.req, matrixFindings()) {
			got = append(got, f.ID)
		}
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("findingsFor(%s) = %v, want %v", c.req.ID, got, c.want)
		}
	}
}

func testSoA(withBroader bool) *soa.SoA {
	s := &soa.SoA{Requirements: []soa.Requirement{
		{AnnexA: "A.5.1", Title: "Policies <x>", Applicable: soa.Applicable, Status: "implemented", Justification: "ok", Evidence: []string{"policies/p.md"}},
		{AnnexA: "A.5.2", Title: "Roles", Applicable: soa.Partial, Status: "gap", Justification: "reduced"},
		{AnnexA: "A.7.1", Title: "Perimeters", Applicable: soa.NotApplicable, Status: "gap", Justification: "remote only"},
		{AnnexA: "A.8.1", Title: "Endpoints", Applicable: soa.Applicable, Status: "partial", Justification: "some"},
	}}
	if withBroader {
		s.Requirements[3].BroaderScope = &soa.BroaderScope{Status: "gap", Delta: soa.DeltaNewGap, Note: "platform"}
	}
	return s
}

func TestRenderSoAMatrixPage(t *testing.T) {
	cases := []struct {
		name    string
		broader bool
		want    []string
		absent  []string
	}{
		{"single scope", false, []string{
			"## Summary", "5 Organizational controls", "7 Physical controls", "8 Technological controls",
			`href="#r-a_5_1"`, `title="A.5.1 Policies &lt;x&gt; — Implemented"`, `class="soa-cell soa-gap"`,
			"border:2px dashed", "[`policies/p.md`](/policies/p)", "[F-1](/findings/f_1)", "[#7](https://github.com/o/r/issues/7)", "[#9](https://github.com/o/r/issues/9)",
			"⚠ ◐ Partial", "<summary>Not applicable requirements (1)</summary>", `<a id="r-a_7_1"></a>`,
			"Applicable requirements (3)", "[plain statement table](/audits/x/soa)",
		}, []string{"Broader scope", "▲"}},
		{"with broader scope", true, []string{"Broader scope (incl. deployment platform)", "▲", "| Broader scope |", "platform"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := testSoA(c.broader)
			out := renderSoAMatrixPage(matrixPage{Title: "T", State: "Draft", Reqs: matrixFromSoA(s), Findings: matrixFindings(), Repo: "o/r", SoAPath: "/audits/x/soa"})
			for _, w := range c.want {
				w = strings.ReplaceAll(w, `—`, "—")
				if !strings.Contains(out, w) {
					t.Errorf("missing %q in:\n%s", w, out)
				}
			}
			for _, a := range c.absent {
				if strings.Contains(out, a) {
					t.Errorf("unexpected %q in:\n%s", a, out)
				}
			}
			// The not applicable control is not in the applicable list.
			applicable := out[strings.Index(out, "## Applicable requirements"):strings.Index(out, "<details>")]
			if strings.Contains(applicable, "Perimeters") {
				t.Error("not applicable requirement listed as applicable")
			}
		})
	}
}

func TestRenderSoAMatrixPageEmptyAndFlat(t *testing.T) {
	if out := renderSoAMatrixPage(matrixPage{Title: "T"}); !strings.Contains(out, "No requirements") {
		t.Errorf("empty page: %s", out)
	}
	out := renderSoAMatrixPage(matrixPage{Title: "T", Reqs: []matrixReq{{ID: "X-1", Title: "t", Anchor: "r-x_1", Applicable: soa.Applicable, Status: "gap"}}})
	if !strings.Contains(out, "### Requirements") {
		t.Errorf("ungrouped requirements not listed under a flat heading:\n%s", out)
	}
}

func TestRenderSoAMatrixPlanColumns(t *testing.T) {
	old := matrixToday
	matrixToday = func() time.Time { return time.Date(2026, 12, 15, 10, 0, 0, 0, time.UTC) }
	defer func() { matrixToday = old }()

	s := testSoA(false)
	// A.5.2 (gap): full plan, past date. A.8.1 (partial): a plan without a date.
	s.Requirements[1].Activity, s.Requirements[1].Owner, s.Requirements[1].TargetDate, s.Requirements[1].Issue = "Define roles", "CISO", "2026-12-01", "o/r#5"
	s.Requirements[3].Activity, s.Requirements[3].Owner = "Harden endpoints", "Tech Lead"
	out := renderSoAMatrixPage(matrixPage{Title: "T", Reqs: matrixFromSoA(s), Repo: "o/r"})
	for _, w := range []string{
		"| Activity | Owner | Target date |",
		"Define roles<br/>[o/r#5](https://github.com/o/r/issues/5) | CISO | ⏰ 2026-12-01 (overdue) |",
		"Harden endpoints | Tech Lead | – |",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in:\n%s", w, out)
		}
	}
	// A future date is not marked; an implemented requirement shows empty cells.
	s.Requirements[1].TargetDate = "2027-01-14"
	out = renderSoAMatrixPage(matrixPage{Title: "T", Reqs: matrixFromSoA(s), Repo: "o/r"})
	if strings.Contains(out, "overdue") || !strings.Contains(out, "| CISO | 2027-01-14 |") {
		t.Errorf("future date mishandled:\n%s", out)
	}
	if !strings.Contains(out, "| ok | [`policies/p.md`](/policies/p) |  |  |  |  |") {
		t.Errorf("implemented requirement should have empty plan cells:\n%s", out)
	}
	// No plan data anywhere: no extra columns.
	if out := renderSoAMatrixPage(matrixPage{Title: "T", Reqs: matrixFromSoA(testSoA(false)), Repo: "o/r"}); strings.Contains(out, "Target date") {
		t.Errorf("plan columns without plan data:\n%s", out)
	}
}

func TestMatrixFromCatalogsEnumeratesCatalog(t *testing.T) {
	fws := []config.FrameworkConfig{{ID: "fw", Name: "FW"}}
	cat := &catalog.FrameworkCatalog{Requirements: []catalog.FrameworkRequirement{
		{ID: "R-1", Title: "One", Section: "S"}, {ID: "R-2", Title: "Two", Section: "S"},
	}}
	cat.ByID = map[string]*catalog.FrameworkRequirement{"R-1": &cat.Requirements[0], "R-2": &cat.Requirements[1]}
	maps := mapping.Mappings{"fw": &mapping.FrameworkMapping{Entries: []mapping.MappingEntry{
		{Key: "R-1", Status: "full", Controls: []string{"C-1"}}, {Key: "X-9", Status: "gap"},
	}}}
	got := matrixFromCatalogs([]string{"fw"}, fws, maps, map[string]*catalog.FrameworkCatalog{"fw": cat})
	status := map[string]string{}
	for _, r := range got {
		status[r.ID] = r.Status
	}
	want := map[string]string{"R-1": "implemented", "R-2": statusNotAssessed, "X-9": "gap"}
	if len(got) != 3 || status["R-1"] != want["R-1"] || status["R-2"] != want["R-2"] || status["X-9"] != want["X-9"] {
		t.Errorf("rows = %v, want %v", status, want)
	}
	// No mapping at all: the catalog is still listed, all not assessed.
	got = matrixFromCatalogs([]string{"fw"}, fws, mapping.Mappings{}, map[string]*catalog.FrameworkCatalog{"fw": cat})
	if len(got) != 2 || got[0].Status != statusNotAssessed {
		t.Errorf("unmapped catalog rows = %+v", got)
	}
}

func TestResolveExplicitFindsFindingsThroughMappedControls(t *testing.T) {
	maps := mapping.Mappings{"iso27001": &mapping.FrameworkMapping{Entries: []mapping.MappingEntry{
		{Key: "A.8.9", Controls: []string{"SEC-AUTH-01"}},
	}}}
	reqs := []matrixReq{{ID: "A.8.9"}, {ID: "A.8.10"}}
	f := []*audit.Finding{{ID: "F-1", Controls: []string{"SEC-AUTH-01"}}}
	if len(findingsFor(&reqs[0], f)) != 0 {
		t.Fatal("finding matched before the mapping was resolved")
	}
	resolveExplicit(reqs, nil, maps)
	if reqs[0].FwID != "iso27001" || len(findingsFor(&reqs[0], f)) != 1 {
		t.Errorf("A.8.9 not resolved: %+v", reqs[0])
	}
	if len(findingsFor(&reqs[1], f)) != 0 {
		t.Error("unmapped row matched a finding")
	}
}

func TestMatrixBroaderApplicableButMainNot(t *testing.T) {
	s := &soa.SoA{Requirements: []soa.Requirement{
		{AnnexA: "A.7.1", Title: "Perimeters", Applicable: soa.NotApplicable, Status: "not_applicable", Justification: "remote only",
			BroaderScope: &soa.BroaderScope{Applicable: soa.Applicable, Status: "gap", Delta: soa.DeltaNewGap, Note: "platform"}},
		{AnnexA: "A.7.2", Title: "Entry", Applicable: soa.NotApplicable, Status: "not_applicable", Justification: "remote only"},
	}}
	out := renderSoAMatrixPage(matrixPage{Title: "T", Reqs: matrixFromSoA(s), Repo: "o/r"})
	applicable := out[strings.Index(out, "## Applicable requirements"):strings.Index(out, "<details>")]
	if !strings.Contains(applicable, "Applicable requirements (1)") || !strings.Contains(applicable, "Perimeters") || strings.Contains(applicable, "Entry") {
		t.Errorf("applicable list:\n%s", applicable)
	}
	if !strings.Contains(out, "Not applicable requirements (1)") {
		t.Errorf("collapsed list should hold only the requirement not applicable in either scope:\n%s", out)
	}
}
