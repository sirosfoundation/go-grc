package serve

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/sirosfoundation/go-grc/pkg/policy"
)

func readPolicyResource(t *testing.T, data *complianceData, uri string) (string, bool) {
	t.Helper()
	s := mcpserver.NewMCPServer("test", "0")
	registerPolicyResources(s, data)
	req, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "resources/read", "params": map[string]any{"uri": uri}})
	raw, _ := json.Marshal(s.HandleMessage(context.Background(), req))
	var resp struct {
		Result struct {
			Contents []struct {
				Text string `json:"text"`
			} `json:"contents"`
		} `json:"result"`
		Error *struct{ Message string } `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decoding %s: %v", raw, err)
	}
	if resp.Error != nil {
		return resp.Error.Message, false
	}
	return resp.Result.Contents[0].Text, true
}

func testPolicies() *complianceData {
	return &complianceData{policies: []*policy.Policy{
		{Slug: "sdlc", Title: "SDLC", Visibility: policy.VisibilityPublic, Status: policy.StatusApproved, ApprovedDate: "2025-01-01",
			DocID: "SID-POL-SDLC", Owner: "Tech Lead", Approver: "CISO", ReviewCycle: "annual", Version: "1.1", Body: "# SDLC\n\ntext\n"},
		{Slug: "draft", Title: "Draft", Body: "# Draft\n"},
	}}
}

func TestDocumentRegisterResource(t *testing.T) {
	text, ok := readPolicyResource(t, testPolicies(), "grc://policies")
	if !ok {
		t.Fatal(text)
	}
	var recs []policyRecord
	if err := json.Unmarshal([]byte(text), &recs); err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("want 2 records, got %d", len(recs))
	}
	sdlc, draft := recs[0], recs[1]
	if sdlc.Version != "1.1" || sdlc.Owner != "Tech Lead" || sdlc.NextReview != "2026-01-01" || !sdlc.Overdue {
		t.Errorf("approved record wrong (review is long overdue): %+v", sdlc)
	}
	if draft.Visibility != policy.VisibilityPrivate || draft.Status != policy.StatusDraft {
		t.Errorf("unset visibility must read private and status draft: %+v", draft)
	}
	if len(draft.Missing) == 0 {
		t.Error("a document without metadata must report what is missing")
	}
}

func TestPolicyResourceHasBodyAndUnknownFails(t *testing.T) {
	text, ok := readPolicyResource(t, testPolicies(), "grc://policy/sdlc")
	if !ok || !strings.Contains(text, "# SDLC") || !strings.Contains(text, "id=SID-POL-SDLC") {
		t.Errorf("policy resource incomplete (ok=%v):\n%s", ok, text)
	}
	if strings.Contains(text, "visibility: public\n---") {
		t.Error("front matter must not be repeated in the body")
	}
	if _, ok := readPolicyResource(t, testPolicies(), "grc://policy/nope"); ok {
		t.Error("unknown policy must be an error")
	}
}

func TestInstructionsCoverDocumentWorkflow(t *testing.T) {
	for _, want := range []string{
		"grc://policies", "has no version, only a revision",
		"never mark a document approved", "Audits and assessments are different",
	} {
		if !strings.Contains(serverInstructions, want) {
			t.Errorf("instructions lack %q", want)
		}
	}
}
