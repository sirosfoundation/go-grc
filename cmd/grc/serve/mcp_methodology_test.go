package serve

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/sirosfoundation/go-grc/pkg/config"
	"github.com/sirosfoundation/go-grc/pkg/risk"
)

func readResource(t *testing.T, data *complianceData, uri string) (string, bool) {
	t.Helper()
	s := mcpserver.NewMCPServer("test", "0")
	registerResources(s, data)
	req, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "resources/read",
		"params": map[string]any{"uri": uri},
	})
	raw, err := json.Marshal(s.HandleMessage(context.Background(), req))
	if err != nil {
		t.Fatal(err)
	}
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

func TestRiskMethodologyResource(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".grc.yaml"), []byte("risk_register:\n  dir: risk-register\n  methodology: policies/method.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "policies"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "policies", "method.md"), []byte("# Methodology v9"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.New(root)
	if err != nil {
		t.Fatal(err)
	}

	text, ok := readResource(t, &complianceData{cfg: cfg, root: root}, "grc://risk/methodology")
	if !ok || text != "# Methodology v9" {
		t.Errorf("expected methodology content, got ok=%v %q", ok, text)
	}

	cfg2, err := config.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	msg, ok := readResource(t, &complianceData{cfg: cfg2}, "grc://risk/methodology")
	if ok || !strings.Contains(msg, "no risk methodology configured") {
		t.Errorf("expected an unconfigured error, got ok=%v %q", ok, msg)
	}
}

func TestInstructionsMakeMethodologyAuthoritative(t *testing.T) {
	for _, want := range []string{"grc://risk/methodology", "authoritative", "must not diverge", "same change"} {
		if !strings.Contains(serverInstructions, want) {
			t.Errorf("server instructions lack %q", want)
		}
	}
}

func TestRiskRegisterResourceIsComplete(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "rr"), 0o755); err != nil {
		t.Fatal(err)
	}
	reg := `risk_register:
  id: platform
  title: Platform
  owner: platform
  last_review: "2026-06-02"
  next_review: "2026-09-02"
risks:
  - id: R1
    finding: F1
    title: T
    owner: Someone
    description: Desc
    consequence: medium
    likelihood: possible
    residual_likelihood: unlikely
    severity: medium
    residual_severity: low
    status: accepted
    decision:
      date: "2026-06-01"
      reviewer: ciso
      owner_accepted_date: "2026-06-02"
      review_interval: quarterly
    treatment_action:
      action: "Rotate keys"
      responsible: "Ops"
      status: done
      completed_date: "2026-07-01"
`
	if err := os.WriteFile(filepath.Join(root, "rr", "platform.yaml"), []byte(reg), 0o644); err != nil {
		t.Fatal(err)
	}
	rs, err := risk.Load(filepath.Join(root, "rr"), []string{"platform.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	text, ok := readResource(t, &complianceData{cfg: &config.Config{}, risks: rs}, "grc://risk/register")
	if !ok {
		t.Fatal(text)
	}
	for _, want := range []string{`"description": "Desc"`, `"reviewer": "ciso"`, `"review_interval": "quarterly"`, `"next_review": "2026-09-02"`, `"residual_likelihood": "unlikely"`, `"owner": "Someone"`, `"owner_accepted_date": "2026-06-02"`, `"treatment_action"`, `"action": "Rotate keys"`, `"responsible": "Ops"`, `"status": "done"`, `"completed_date": "2026-07-01"`} {
		if !strings.Contains(text, want) {
			t.Errorf("register resource lacks %s:\n%s", want, text)
		}
	}
}
