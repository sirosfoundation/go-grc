package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePolicy(t *testing.T, body string) *Policy {
	t.Helper()
	f := filepath.Join(t.TempDir(), "x.md")
	if err := os.WriteFile(f, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := parse(f)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestMetadataFieldsAreTakenFromFrontMatter(t *testing.T) {
	p := writePolicy(t, "---\nvisibility: public\nstatus: approved\napproved_date: 2026-09-10\ndocument_id: SID-POL-X\nowner: Tech Lead\napprover: CISO\nreview_cycle: annual\nversion: \"1.1\"\nsidebar_label: X\n---\n\n# X\n")
	if p.DocID != "SID-POL-X" || p.Owner != "Tech Lead" || p.Approver != "CISO" || p.ReviewCycle != "annual" || p.Version != "1.1" {
		t.Errorf("fields not read: %+v", p)
	}
	// None of them may reach Docusaurus: "id" would change the page slug.
	for _, k := range []string{"document_id", "owner", "approver", "review_cycle", "version", "id"} {
		if _, leaked := p.Meta[k]; leaked {
			t.Errorf("%q passed through to the site front matter", k)
		}
	}
	if p.Meta["sidebar_label"] != "X" {
		t.Error("unrelated front matter must still pass through")
	}
}

func TestVersionAsNumberKeepsItsText(t *testing.T) {
	p := writePolicy(t, "---\nvisibility: private\nstatus: draft\nversion: 1.10\n---\n# X\n")
	if p.Version != "1.1" && p.Version != "1.10" {
		t.Errorf("version = %q", p.Version)
	}
}

func TestParseReviewCycle(t *testing.T) {
	for in, want := range map[string]int{"annual": 12, "Annual": 12, "yearly": 12, "quarterly": 3, "semi-annual": 6, "6 months": 6, "2 years": 24, "1 month": 1} {
		if got, ok := ParseReviewCycle(in); !ok || got != want {
			t.Errorf("ParseReviewCycle(%q) = %d,%v want %d", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "often", "0 months", "-3 months", "every day"} {
		if _, ok := ParseReviewCycle(in); ok {
			t.Errorf("ParseReviewCycle(%q) must not parse", in)
		}
	}
}

func TestNextReview(t *testing.T) {
	p := &Policy{ApprovedDate: "2026-10-13", ReviewCycle: "annual"}
	if got, ok := p.NextReview(); !ok || got.Format("2006-01-02") != "2027-10-13" {
		t.Errorf("NextReview = %v,%v", got, ok)
	}
	if _, ok := (&Policy{ReviewCycle: "annual"}).NextReview(); ok {
		t.Error("no approval date, no next review")
	}
}

func TestValidateMetadata(t *testing.T) {
	base := Policy{Visibility: VisibilityPrivate, Status: StatusDraft}
	bad := base
	bad.ApprovedDate, bad.ReviewCycle = "10/13/2026", "often"
	if got := strings.Join(bad.Validate(), "|"); !strings.Contains(got, "approved_date") || !strings.Contains(got, "review_cycle") {
		t.Errorf("malformed values must be errors: %s", got)
	}
	if len(base.Validate()) != 0 {
		t.Errorf("missing optional metadata is not an error: %v", base.Validate())
	}
	if w := strings.Join(base.Warnings(), "|"); !strings.Contains(w, "document_id") || !strings.Contains(w, "owner") {
		t.Errorf("missing metadata should warn: %s", w)
	}
	approved := Policy{Visibility: VisibilityPublic, Status: StatusApproved, ApprovedDate: "2026-01-01", DocID: "D", Owner: "o", Approver: "a", ReviewCycle: "annual"}
	if w := approved.Warnings(); len(w) != 1 || !strings.Contains(w[0], "version") {
		t.Errorf("approved without version should warn once: %v", w)
	}
}
