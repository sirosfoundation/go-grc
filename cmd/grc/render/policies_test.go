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
