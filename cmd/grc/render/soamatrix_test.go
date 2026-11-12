package render

import (
	"strings"
	"testing"

	"github.com/sirosfoundation/go-grc/pkg/audit"
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
			"border:2px dashed", "[`policies/p.md`](/policies/p)", "[F-1](/findings/f_1)", "[#7](https://github.com/o/r/issues/7)",
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
