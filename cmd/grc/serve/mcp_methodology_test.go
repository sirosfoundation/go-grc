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
