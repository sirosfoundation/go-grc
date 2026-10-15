package render

import (
	"strings"
	"testing"
	"time"

	"github.com/sirosfoundation/go-grc/pkg/policy"
)

func TestPolicyIndexSplitByVisibility(t *testing.T) {
	pols := []*policy.Policy{
		{Slug: "pub", Title: "Pub", Visibility: policy.VisibilityPublic, Status: policy.StatusApproved},
		{Slug: "priv", Title: "Priv", Visibility: policy.VisibilityPrivate, Status: policy.StatusDraft},
	}
	changes := map[string]*policy.Change{
		"pub":  {SHA: "abc123", Time: time.Date(2026, 10, 15, 12, 3, 0, 0, time.UTC)},
		"priv": nil,
	}

	priv := renderPolicyIndex(pols, changes, "o/r", false)
	ipub, ipriv := strings.Index(priv, "## Public policies"), strings.Index(priv, "## Private policies")
	if ipub < 0 || ipriv < 0 || ipub > ipriv {
		t.Fatalf("private index must have public then private tables:\n%s", priv)
	}
	if !strings.Contains(priv[ipub:ipriv], "[Pub]") || strings.Contains(priv[ipub:ipriv], "[Priv]") {
		t.Errorf("policy in the wrong table:\n%s", priv)
	}
	if !strings.Contains(priv, "[2026-10-15 12:03 UTC](https://github.com/o/r/commit/abc123)") {
		t.Errorf("private index must link the last commit:\n%s", priv)
	}
	if !strings.Contains(priv[ipriv:], "| – |") {
		t.Errorf("unknown change must read as a dash:\n%s", priv)
	}

	pub := renderPolicyIndex(pols, changes, "o/r", true)
	if strings.Contains(pub, "Priv") || strings.Contains(pub, "commit/") || strings.Contains(pub, "github.com") {
		t.Errorf("public index leaks a private policy or repository link:\n%s", pub)
	}
	if !strings.Contains(pub, "2026-10-15 12:03 UTC") {
		t.Errorf("public index should still show the time:\n%s", pub)
	}
}

func cardPolicy() *policy.Policy {
	return &policy.Policy{
		Slug: "sdlc", Title: "SDLC", Visibility: policy.VisibilityPublic, Status: policy.StatusApproved,
		ApprovedDate: "2026-09-10", DocID: "SID-POL-SDLC", Owner: "Tech Lead", Approver: "CISO",
		ReviewCycle: "annual", Version: "1.1",
	}
}

func TestPolicyCardPublicShowsOnlyOwnerVersionPublished(t *testing.T) {
	c := &policy.Change{SHA: "abc123", Time: time.Date(2026, 10, 15, 12, 3, 0, 0, time.UTC)}
	out := policyCard(cardPolicy(), c, 7, "o/r", true, time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC))
	for _, want := range []string{"<dt>Owner</dt><dd>Tech Lead</dd>", "<dt>Version</dt><dd>1.1</dd>", "<dt>Published</dt><dd>2026-09-10</dd>"} {
		if !strings.Contains(out, want) {
			t.Errorf("public card lacks %q:\n%s", want, out)
		}
	}
	for _, leak := range []string{"SID-POL-SDLC", "CISO", "annual", "abc123", "github.com", "revision", "Next review"} {
		if strings.Contains(out, leak) {
			t.Errorf("public card leaks %q:\n%s", leak, out)
		}
	}
}

func TestPolicyCardPrivateShowsEverythingAndOverdue(t *testing.T) {
	c := &policy.Change{SHA: "abc123", Time: time.Date(2026, 10, 15, 12, 3, 0, 0, time.UTC)}
	out := policyCard(cardPolicy(), c, 7, "o/r", false, time.Date(2027, 10, 1, 0, 0, 0, 0, time.UTC))
	for _, want := range []string{"SID-POL-SDLC", "CISO", "annual", `<a href="https://github.com/o/r/commit/abc123">2026-10-15 12:03 UTC</a>`, "2027-09-10 (overdue)"} {
		if !strings.Contains(out, want) {
			t.Errorf("private card lacks %q:\n%s", want, out)
		}
	}
}

func TestPolicyCardDraftShowsRevisionAndEscapes(t *testing.T) {
	p := &policy.Policy{Slug: "d", Title: "D", Visibility: policy.VisibilityPrivate, Status: policy.StatusDraft, Owner: "A & <B>"}
	out := policyCard(p, nil, 4, "", false, time.Now())
	if !strings.Contains(out, "Draft · revision 4") {
		t.Errorf("draft must show its revision:\n%s", out)
	}
	if !strings.Contains(out, "A &amp; &lt;B&gt;") || strings.Contains(out, "<B>") {
		t.Errorf("values must be HTML-escaped:\n%s", out)
	}
	if policyCard(&policy.Policy{}, nil, 0, "", true, time.Now()) != "" {
		t.Error("nothing to show, no card")
	}
}

func TestPolicyPagePutsCardBeforeHeading(t *testing.T) {
	p := cardPolicy()
	p.Body = "\n# SDLC\n\ntext\n"
	page, err := renderPolicyPage(p, nil, 0, "o/r", true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ic, ih := strings.Index(page, "<aside"), strings.Index(page, "# SDLC")
	if ic < 0 || ih < 0 || ic > ih {
		t.Errorf("card must come before the heading:\n%s", page)
	}
	if strings.Contains(page, "\nid:") || strings.Contains(page, "document_id") {
		t.Errorf("metadata leaked into front matter:\n%s", page)
	}
}
