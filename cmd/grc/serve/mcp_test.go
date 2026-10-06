package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// isoMapping overrides the testdata ISO mapping so that every mapping status
// (covered, partial, missing) is exercised by the gap analysis.
const isoMapping = `mappings:
  - annex_a: "A.8.9"
    controls: ["SEC-AUTH-01"]
    coverage: "covered"
    owner: "platform"
  - annex_a: "A.8.20"
    controls: ["SEC-WEB-01"]
    coverage: "partial"
    notes: "CSP only"
    owner: "platform"
  - annex_a: "A.8.24"
    controls: ["SEC-KEY-01"]
    coverage: "not_assessed"
    owner: "platform"
`

// fixtureRoot copies the repo testdata into a temp dir and adds an
// architecture directory, so tests can mutate it freely.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	return populateFixture(t, t.TempDir())
}

func populateFixture(t *testing.T, root string) string {
	t.Helper()
	_, f, _, _ := runtime.Caller(0)
	src := filepath.Join(filepath.Dir(f), "..", "..", "..", "testdata")
	if err := os.CopyFS(root, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("mappings/iso27001-annexa.yaml", isoMapping)
	write("architecture/threat-model.md", "# Threat model\nSTRIDE analysis")
	write("architecture/notes.txt", "not markdown")
	write("secret.md", "TOP SECRET")
	// The renderer swaps its output into an existing site/docs directory.
	if err := os.MkdirAll(filepath.Join(root, "site", "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func loadedData(t *testing.T) *complianceData {
	t.Helper()
	d := newComplianceData(fixtureRoot(t), "private")
	if err := d.reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	return d
}

func fullServer(data *complianceData) *mcpserver.MCPServer {
	s := mcpserver.NewMCPServer("test", "0")
	registerResources(s, data)
	registerResourceTemplates(s, data)
	registerTools(s, data)
	registerBidTools(s, data)
	registerPrompts(s, data)
	return s
}

func rpc(t *testing.T, s *mcpserver.MCPServer, method string, params any) (json.RawMessage, string) {
	t.Helper()
	req, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	raw, err := json.Marshal(s.HandleMessage(context.Background(), req))
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decoding %s: %v", raw, err)
	}
	if resp.Error != nil {
		return nil, resp.Error.Message
	}
	return resp.Result, ""
}

// callTool returns the text of the tool result and whether it was an error result.
func callTool(t *testing.T, s *mcpserver.MCPServer, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, errMsg := rpc(t, s, "tools/call", map[string]any{"name": name, "arguments": args})
	if errMsg != "" {
		t.Fatalf("tools/call %s: protocol error %s", name, errMsg)
	}
	var out struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Content) != 1 {
		t.Fatalf("expected one content item, got %s", res)
	}
	return out.Content[0].Text, out.IsError
}

func callJSON(t *testing.T, s *mcpserver.MCPServer, name string, args map[string]any, v any) {
	t.Helper()
	text, isErr := callTool(t, s, name, args)
	if isErr {
		t.Fatalf("%s returned error result: %s", name, text)
	}
	if err := json.Unmarshal([]byte(text), v); err != nil {
		t.Fatalf("%s: bad JSON %q: %v", name, text, err)
	}
}

func getPrompt(t *testing.T, s *mcpserver.MCPServer, name string, args map[string]string) string {
	t.Helper()
	res, errMsg := rpc(t, s, "prompts/get", map[string]any{"name": name, "arguments": args})
	if errMsg != "" {
		t.Fatalf("prompts/get %s: %s", name, errMsg)
	}
	var out struct {
		Description string `json:"description"`
		Messages    []struct {
			Role    string `json:"role"`
			Content struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Messages) != 1 || out.Messages[0].Role != "user" {
		t.Fatalf("unexpected prompt result %s", res)
	}
	return out.Description + "\n" + out.Messages[0].Content.Text
}

func ids(items []map[string]any, key string) []string {
	var out []string
	for _, it := range items {
		out = append(out, it[key].(string))
	}
	return out
}

func containsAll(t *testing.T, got []string, want ...string) {
	t.Helper()
	set := map[string]bool{}
	for _, g := range got {
		set[g] = true
	}
	for _, w := range want {
		if !set[w] {
			t.Errorf("missing %q in %v", w, got)
		}
	}
}

func TestReloadLoadsAllData(t *testing.T) {
	d := loadedData(t)
	if d.cfg.Project.Repo != "test/compliance" {
		t.Errorf("repo = %q", d.cfg.Project.Repo)
	}
	if len(d.catalog.Controls) != 5 {
		t.Errorf("controls = %d, want 5", len(d.catalog.Controls))
	}
	if len(d.audits.FindingsByID) != 4 {
		t.Errorf("findings = %d, want 4", len(d.audits.FindingsByID))
	}
	if d.risks == nil || len(d.risks.RisksByID) != 1 {
		t.Errorf("risks not loaded: %+v", d.risks)
	}
	if len(d.mappings) != 3 {
		t.Errorf("mappings = %d, want 3", len(d.mappings))
	}
	if fc := d.fwCats["eudi"]; fc == nil || fc.ByID["WTE_07"] == nil {
		t.Errorf("eudi framework catalog not loaded: %+v", d.fwCats)
	}
	if len(d.cycles) != 0 {
		t.Errorf("no year cycle configured, got %d", len(d.cycles))
	}
}

func TestReloadErrors(t *testing.T) {
	d := newComplianceData(t.TempDir(), "private")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".grc.yaml"), []byte("project: [unterminated"), 0o644); err != nil {
		t.Fatal(err)
	}
	d.root = root
	if err := d.reload(); err == nil || !strings.Contains(err.Error(), "loading config") {
		t.Errorf("expected config error, got %v", err)
	}

	// Broken mapping file surfaces as a mappings error.
	root = fixtureRoot(t)
	if err := os.WriteFile(filepath.Join(root, "mappings", "gdpr.yaml"), []byte("mappings: [unterminated"), 0o644); err != nil {
		t.Fatal(err)
	}
	d = newComplianceData(root, "private")
	if err := d.reload(); err == nil || !strings.Contains(err.Error(), "loading mappings") {
		t.Errorf("expected mappings error, got %v", err)
	}

	// Broken audit file surfaces as an audits error.
	root = fixtureRoot(t)
	if err := os.WriteFile(filepath.Join(root, "audits", "sa-2025-001.yaml"), []byte("audit: [unterminated"), 0o644); err != nil {
		t.Fatal(err)
	}
	d = newComplianceData(root, "private")
	if err := d.reload(); err == nil || !strings.Contains(err.Error(), "loading audits") {
		t.Errorf("expected audits error, got %v", err)
	}
}

func TestReloadToleratesBadRiskRegisterAndYearCycle(t *testing.T) {
	root := fixtureRoot(t)
	if err := os.WriteFile(filepath.Join(root, "risk-register", "platform.yaml"), []byte("risks: [unterminated"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(root, ".grc.yaml")
	b, _ := os.ReadFile(cfgPath)
	b = append(b, []byte("\nyear_cycle:\n  source: missing/cycle.yaml\n  title: Cycle\n")...)
	if err := os.WriteFile(cfgPath, b, 0o644); err != nil {
		t.Fatal(err)
	}
	d := newComplianceData(root, "private")
	if err := d.reload(); err != nil {
		t.Fatalf("reload should only warn: %v", err)
	}
	if len(d.cycles) != 0 {
		t.Errorf("failed year cycle must not be added, got %d", len(d.cycles))
	}
	// risk_summary / register must still work without panicking.
	s := fullServer(d)
	if _, isErr := callTool(t, s, "risk_summary", nil); isErr {
		t.Error("risk_summary errored")
	}
}

func TestReloadLoadsYearCycle(t *testing.T) {
	root := fixtureRoot(t)
	ics := "BEGIN:VCALENDAR\nVERSION:2.0\nPRODID:-//Test//Test//EN\nBEGIN:VEVENT\nDTSTART:20260115T090000Z\nSUMMARY:DPIA Review\nRRULE:FREQ=YEARLY\nEND:VEVENT\nEND:VCALENDAR\n"
	icsPath := filepath.Join(root, "cycle.ics")
	if err := os.WriteFile(icsPath, []byte(ics), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(root, ".grc.yaml")
	cb, _ := os.ReadFile(cfgPath)
	cb = append(cb, []byte("\nyear_cycle:\n  source: "+icsPath+"\n  title: My Cycle\n")...)
	if err := os.WriteFile(cfgPath, cb, 0o644); err != nil {
		t.Fatal(err)
	}
	d := newComplianceData(root, "private")
	if err := d.reload(); err != nil {
		t.Fatal(err)
	}
	if len(d.cycles) != 1 || d.cycles[0].Title != "My Cycle" || len(d.cycles[0].Events) != 1 {
		t.Errorf("year cycle not loaded: %+v", d.cycles)
	}
}

func TestResourcesRead(t *testing.T) {
	d := loadedData(t)

	text, ok := readResource(t, d, "grc://config")
	if !ok || !strings.Contains(text, "Test Dashboard") || !strings.Contains(text, "native_only") || !strings.Contains(text, "Test Service") {
		t.Errorf("config resource: ok=%v %s", ok, text)
	}

	text, ok = readResource(t, d, "grc://catalog")
	if !ok {
		t.Fatal(text)
	}
	var controls []map[string]any
	if err := json.Unmarshal([]byte(text), &controls); err != nil {
		t.Fatal(err)
	}
	if len(controls) != 5 {
		t.Fatalf("catalog has %d controls, want 5", len(controls))
	}
	byID := map[string]map[string]any{}
	for _, c := range controls {
		byID[c["id"].(string)] = c
	}
	if byID["SEC-AUTH-01"]["status"] != "verified" {
		t.Errorf("SEC-AUTH-01 status = %v", byID["SEC-AUTH-01"]["status"])
	}
	if byID["SEC-AUTH-01"]["group"] != "Authentication Controls" {
		t.Errorf("group = %v", byID["SEC-AUTH-01"]["group"])
	}
	if got := byID["SEC-AUTH-01"]["url"]; got != "https://test.example.com/controls/technical/sec_auth_01" {
		t.Errorf("technical url = %v", got)
	}
	if got := byID["GOV-POL-01"]["url"]; got != "https://test.example.com/controls/organizational/gov_pol_01" {
		t.Errorf("organizational url = %v", got)
	}

	text, ok = readResource(t, d, "grc://audit/findings")
	if !ok {
		t.Fatal(text)
	}
	var findings []map[string]any
	if err := json.Unmarshal([]byte(text), &findings); err != nil {
		t.Fatal(err)
	}
	if got := ids(findings, "id"); strings.Join(got, ",") != "F-001,F-002,F-003,F-004" {
		t.Errorf("findings not sorted/complete: %v", got)
	}
	if findings[0]["severity"] != "high" || findings[0]["status"] != "resolved" {
		t.Errorf("F-001 = %v", findings[0])
	}

	text, ok = readResource(t, d, "grc://risk/register")
	if !ok || !strings.Contains(text, "RSK-P-001") || !strings.Contains(text, "Platform Risk Register") {
		t.Errorf("register resource: ok=%v %s", ok, text)
	}

	// Without a risk register the resource is an empty list, not an error.
	d.risks = nil
	text, ok = readResource(t, d, "grc://risk/register")
	if !ok || !strings.Contains(text, `"risks": []`) {
		t.Errorf("nil register: ok=%v %s", ok, text)
	}
}

func TestResourceTemplates(t *testing.T) {
	d := loadedData(t)

	s := fullServer(d)
	read := func(uri string) (string, string) {
		res, errMsg := rpc(t, s, "resources/read", map[string]any{"uri": uri})
		if errMsg != "" {
			return "", errMsg
		}
		var out struct {
			Contents []struct {
				Text string `json:"text"`
				URI  string `json:"uri"`
			} `json:"contents"`
		}
		if err := json.Unmarshal(res, &out); err != nil {
			t.Fatal(err)
		}
		return out.Contents[0].Text, ""
	}

	body, errMsg := read("grc://catalog/control/SEC-WEB-01")
	if errMsg != "" || !strings.Contains(body, "HTTP security headers") {
		t.Errorf("control detail: %q %q", body, errMsg)
	}
	if _, errMsg = read("grc://catalog/control/NOPE-01"); !strings.Contains(errMsg, `control "NOPE-01" not found`) {
		t.Errorf("unknown control: %q", errMsg)
	}

	body, errMsg = read("grc://audit/finding/F-003")
	if errMsg != "" || !strings.Contains(body, "No key rotation policy") {
		t.Errorf("finding detail: %q %q", body, errMsg)
	}
	if _, errMsg = read("grc://audit/finding/F-999"); !strings.Contains(errMsg, `finding "F-999" not found`) {
		t.Errorf("unknown finding: %q", errMsg)
	}

	body, errMsg = read("grc://mapping/iso27001")
	if errMsg != "" || !strings.Contains(body, "A.8.20") || !strings.Contains(body, "partial") {
		t.Errorf("mapping: %q %q", body, errMsg)
	}
	if _, errMsg = read("grc://mapping/nope"); !strings.Contains(errMsg, `framework mapping "nope" not found`) {
		t.Errorf("unknown mapping: %q", errMsg)
	}

	// Architecture docs: with and without extension.
	for _, uri := range []string{"grc://architecture/threat-model", "grc://architecture/threat-model.md"} {
		body, errMsg = read(uri)
		if errMsg != "" || !strings.Contains(body, "STRIDE analysis") {
			t.Errorf("%s: %q %q", uri, body, errMsg)
		}
	}
	if _, errMsg = read("grc://architecture/missing"); !strings.Contains(errMsg, `architecture document "missing.md" not found`) {
		t.Errorf("missing doc: %q", errMsg)
	}
	// Path traversal must be neutralized: ../secret resolves inside architecture/.
	for _, uri := range []string{"grc://architecture/../secret", "grc://architecture/..%2Fsecret", "grc://architecture/../../secret.md"} {
		body, errMsg = read(uri)
		if strings.Contains(body, "TOP SECRET") {
			t.Fatalf("path traversal leaked file via %s", uri)
		}
		if errMsg == "" {
			t.Errorf("%s: expected not-found error, got body %q", uri, body)
		}
	}
}

func TestSearchControlsTool(t *testing.T) {
	s := fullServer(loadedData(t))
	search := func(args map[string]any) []map[string]any {
		var res []map[string]any
		callJSON(t, s, "search_controls", args, &res)
		return res
	}

	if got := ids(search(nil), "id"); len(got) != 5 {
		t.Errorf("unfiltered = %v", got)
	}
	// SEC-SESS-01 is "to_do" in the catalog but derives to verified from its
	// resolved finding F-001.
	got := ids(search(map[string]any{"status": "verified"}), "id")
	sort.Strings(got)
	if strings.Join(got, ",") != "SEC-AUTH-01,SEC-SESS-01" {
		t.Errorf("verified = %v", got)
	}
	got = ids(search(map[string]any{"query": "session"}), "id")
	if strings.Join(got, ",") != "SEC-SESS-01" {
		t.Errorf("query session = %v", got)
	}
	res := search(map[string]any{"query": "KEY", "status": "TO_DO"})
	if len(res) != 1 || res[0]["id"] != "SEC-KEY-01" || res[0]["group"] != "Web Security Controls" {
		t.Errorf("combined filter = %v", res)
	}
	if res := search(map[string]any{"query": "zzz-no-match"}); len(res) != 0 {
		t.Errorf("no-match = %v", res)
	}
	for _, c := range search(map[string]any{"category": "no-such-category"}) {
		t.Errorf("unexpected category match %v", c)
	}
}

func TestSearchFindingsTool(t *testing.T) {
	s := fullServer(loadedData(t))
	search := func(args map[string]any) []map[string]any {
		var res []map[string]any
		callJSON(t, s, "search_findings", args, &res)
		return res
	}
	if got := ids(search(nil), "id"); strings.Join(got, ",") != "F-001,F-002,F-003,F-004" {
		t.Errorf("all = %v", got)
	}
	if got := ids(search(map[string]any{"severity": "HIGH"}), "id"); strings.Join(got, ",") != "F-001,F-003" {
		t.Errorf("high = %v", got)
	}
	if got := ids(search(map[string]any{"status": "open"}), "id"); strings.Join(got, ",") != "F-003" {
		t.Errorf("open = %v", got)
	}
	res := search(map[string]any{"query": "csp"})
	if len(res) != 1 || res[0]["id"] != "F-002" {
		t.Errorf("query csp = %v", res)
	}
	if res := search(map[string]any{"query": "session", "severity": "medium"}); len(res) != 1 || res[0]["id"] != "F-004" {
		t.Errorf("session+medium = %v", res)
	}
}

func TestGapAnalysisTool(t *testing.T) {
	s := fullServer(loadedData(t))
	var out struct {
		Framework string         `json:"framework"`
		Summary   map[string]int `json:"summary"`
		Gaps      []struct {
			Key    string `json:"requirement_key"`
			Status string `json:"mapping_status"`
		} `json:"gaps"`
	}
	callJSON(t, s, "compliance_gap_analysis", map[string]any{"framework": "ISO27001"}, &out)
	if out.Framework != "iso27001" {
		t.Errorf("framework = %q", out.Framework)
	}
	want := map[string]int{"total": 3, "covered": 1, "partial": 1, "missing": 1}
	for k, v := range want {
		if out.Summary[k] != v {
			t.Errorf("summary[%s] = %d, want %d (%v)", k, out.Summary[k], v, out.Summary)
		}
	}
	if len(out.Gaps) != 2 {
		t.Fatalf("gaps = %+v", out.Gaps)
	}
	keys := []string{out.Gaps[0].Key, out.Gaps[1].Key}
	containsAll(t, keys, "A.8.20", "A.8.24")

	text, isErr := callTool(t, s, "compliance_gap_analysis", map[string]any{"framework": "nope"})
	if !isErr || !strings.Contains(text, `framework "nope" not found; available: eudi, gdpr, iso27001`) {
		t.Errorf("unknown framework: isErr=%v %q", isErr, text)
	}
	text, isErr = callTool(t, s, "compliance_gap_analysis", map[string]any{})
	if !isErr || !strings.Contains(text, "framework parameter is required") {
		t.Errorf("missing framework: isErr=%v %q", isErr, text)
	}
}

func TestFindingStatisticsTool(t *testing.T) {
	s := fullServer(loadedData(t))
	var out struct {
		Total      int            `json:"total"`
		Active     int            `json:"active"`
		Resolved   int            `json:"resolved"`
		BySeverity map[string]int `json:"by_severity"`
		ByStatus   map[string]int `json:"by_status"`
	}
	callJSON(t, s, "finding_statistics", nil, &out)
	if out.Total != 4 {
		t.Errorf("total = %d", out.Total)
	}
	if out.BySeverity["high"] != 2 || out.BySeverity["medium"] != 2 {
		t.Errorf("by_severity = %v", out.BySeverity)
	}
	if out.ByStatus["resolved"] != 1 || out.ByStatus["open"] != 1 || out.ByStatus["in_progress"] != 1 || out.ByStatus["accepted"] != 1 {
		t.Errorf("by_status = %v", out.ByStatus)
	}
	if out.Resolved != 1 {
		t.Errorf("resolved = %d", out.Resolved)
	}
	if out.Active != 1 {
		t.Errorf("active = %d (only in_progress counts as active)", out.Active)
	}
}

func TestRiskSummaryTool(t *testing.T) {
	d := loadedData(t)
	s := fullServer(d)
	var out struct {
		Methodology string         `json:"methodology"`
		Total       int            `json:"total_risks"`
		ByStatus    map[string]int `json:"by_status"`
		BySeverity  map[string]int `json:"by_residual_severity"`
		ByOwner     map[string]int `json:"by_owner"`
		Overdue     int            `json:"overdue_registers"`
	}
	callJSON(t, s, "risk_summary", nil, &out)
	if out.Total != 1 || out.ByStatus["accepted"] != 1 || out.BySeverity["low"] != 1 || out.ByOwner["Test Owner"] != 1 {
		t.Errorf("summary = %+v", out)
	}
	if out.Methodology != "" {
		t.Errorf("no methodology configured, got %q", out.Methodology)
	}
	if out.Overdue != 0 {
		t.Errorf("overdue = %d", out.Overdue)
	}

	// Configure a methodology, unassigned owner and an overdue register.
	d.cfg.RiskMethodologyPath = "/x/method.md"
	for _, ref := range d.risks.RisksByID {
		ref.Risk.Owner = "  "
		ref.File.Data.Register.NextReview = "2000-01-01"
	}
	callJSON(t, s, "risk_summary", nil, &out)
	if out.Methodology != "grc://risk/methodology" || out.ByOwner["(unassigned)"] != 1 || out.Overdue != 1 {
		t.Errorf("summary after edit = %+v", out)
	}

	d.risks = nil
	text, isErr := callTool(t, s, "risk_summary", nil)
	if isErr || !strings.Contains(text, "no risk register configured") {
		t.Errorf("nil risks: %v %q", isErr, text)
	}
}

func TestListArchitectureDocsTool(t *testing.T) {
	d := loadedData(t)
	s := fullServer(d)
	var docs []map[string]string
	callJSON(t, s, "list_architecture_docs", nil, &docs)
	if len(docs) != 1 || docs[0]["name"] != "threat-model" || docs[0]["uri"] != "grc://architecture/threat-model" {
		t.Errorf("docs = %v (non-.md files must be skipped)", docs)
	}

	d.root = t.TempDir()
	text, isErr := callTool(t, s, "list_architecture_docs", nil)
	if !isErr || !strings.Contains(text, "no architecture directory found") {
		t.Errorf("missing dir: %v %q", isErr, text)
	}
}

func TestControlCoverageTool(t *testing.T) {
	s := fullServer(loadedData(t))
	var out struct {
		ControlID    string `json:"control_id"`
		ReferencedIn int    `json:"referenced_in"`
		Coverage     []struct {
			Framework string `json:"framework"`
			Key       string `json:"requirement_key"`
			Status    string `json:"status"`
		} `json:"coverage"`
	}
	callJSON(t, s, "control_coverage", map[string]any{"control_id": "sec-auth-01"}, &out)
	if out.ReferencedIn != 3 || len(out.Coverage) != 3 {
		t.Fatalf("coverage = %+v", out)
	}
	if out.Coverage[0].Framework != "eudi" || out.Coverage[1].Framework != "gdpr" || out.Coverage[2].Framework != "iso27001" {
		t.Errorf("not sorted by framework: %+v", out.Coverage)
	}
	if out.Coverage[2].Key != "A.8.9" || out.Coverage[2].Status != "covered" {
		t.Errorf("iso entry = %+v", out.Coverage[2])
	}

	callJSON(t, s, "control_coverage", map[string]any{"control_id": "GOV-POL-01"}, &out)
	if out.ReferencedIn != 0 {
		t.Errorf("GOV-POL-01 referenced_in = %d", out.ReferencedIn)
	}
	text, isErr := callTool(t, s, "control_coverage", nil)
	if !isErr || !strings.Contains(text, "control_id parameter is required") {
		t.Errorf("missing id: %v %q", isErr, text)
	}
}

func TestMapBidRequirementTool(t *testing.T) {
	s := fullServer(loadedData(t))
	var out struct {
		Coverage string `json:"coverage_level"`
		Matched  []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Reason string `json:"match_reason"`
			URL    string `json:"url"`
		} `json:"matched_controls"`
		Evidence []struct {
			FindingID string `json:"finding_id"`
		} `json:"evidence"`
		FwRefs []struct {
			Framework string `json:"framework"`
			Key       string `json:"requirement_key"`
		} `json:"framework_refs"`
		OpenIssues []struct {
			FindingID string `json:"finding_id"`
		} `json:"open_issues"`
		Summary map[string]int `json:"summary"`
		ReqID   string         `json:"requirement_id"`
		Class   string         `json:"classification"`
	}
	callJSON(t, s, "map_bid_requirement", map[string]any{
		"requirement_id":   "1.1",
		"requirement_text": "The wallet must provide strong authentication and session management",
		"classification":   "MANDATORY",
	}, &out)
	if out.ReqID != "1.1" || out.Class != "MANDATORY" {
		t.Errorf("echoed fields: %+v", out)
	}
	var matched []string
	for _, m := range out.Matched {
		matched = append(matched, m.ID)
		if m.ID == "SEC-AUTH-01" {
			if m.Status != "verified" || !strings.Contains(m.Reason, "title keyword: authentication") {
				t.Errorf("SEC-AUTH-01 match = %+v", m)
			}
			if m.URL != "https://test.example.com/controls/technical/sec_auth_01" {
				t.Errorf("url = %q", m.URL)
			}
		}
	}
	containsAll(t, matched, "SEC-AUTH-01", "SEC-SESS-01")
	// F-001 (resolved, has evidence) is linked to the auth controls.
	var evIDs []string
	for _, e := range out.Evidence {
		evIDs = append(evIDs, e.FindingID)
	}
	containsAll(t, evIDs, "F-001")
	// F-001 is resolved so it must not be an open issue.
	for _, o := range out.OpenIssues {
		if o.FindingID == "F-001" {
			t.Error("resolved finding listed as open issue")
		}
	}
	if len(out.FwRefs) == 0 {
		t.Error("expected framework references for matched controls")
	}
	if out.Summary["controls_matched"] != len(out.Matched) || out.Summary["evidence_items"] != len(out.Evidence) {
		t.Errorf("summary = %v", out.Summary)
	}
	// Some matched controls are verified, others (e.g. SEC-WEB-01) are not.
	if out.Coverage != "partially_covered" {
		t.Errorf("coverage = %q, want partially_covered", out.Coverage)
	}

	// Nothing relevant: not_covered.
	callJSON(t, s, "map_bid_requirement", map[string]any{
		"requirement_id": "9.9", "requirement_text": "xyz qrs",
	}, &out)
	if out.Coverage != "not_covered" || len(out.Matched) != 0 {
		t.Errorf("unmatched = %+v", out)
	}

	// Only unverified controls match: controls_identified.
	callJSON(t, s, "map_bid_requirement", map[string]any{
		"requirement_id": "2.1", "requirement_text": "HTTP headers",
	}, &out)
	if out.Coverage != "controls_identified" {
		t.Errorf("coverage = %q (%+v)", out.Coverage, out.Matched)
	}
	if len(out.OpenIssues) == 0 {
		t.Error("expected open issues (F-002/F-003) for unverified controls")
	}
}

func TestAssessBidRequirementsTool(t *testing.T) {
	s := fullServer(loadedData(t))
	reqs := `[
	  {"id":"1","text":"Security policy documentation","classification":"MANDATORY"},
	  {"id":"2","text":"HTTP headers"},
	  {"id":"3","text":"xyz qrs"},
	  {"id":"4","text":"strong authentication of wallet users"}
	]`
	var out struct {
		Total   int            `json:"total_requirements"`
		Summary map[string]int `json:"coverage_summary"`
		Reqs    []struct {
			ID       string   `json:"id"`
			Coverage string   `json:"coverage_level"`
			Controls []string `json:"control_ids"`
			Evidence int      `json:"evidence_count"`
			Open     int      `json:"open_issues"`
			Class    string   `json:"classification"`
		} `json:"requirements"`
	}
	callJSON(t, s, "assess_bid_requirements", map[string]any{"requirements_json": reqs}, &out)
	if out.Total != 4 || len(out.Reqs) != 4 {
		t.Fatalf("out = %+v", out)
	}
	if out.Reqs[0].Class != "MANDATORY" {
		t.Errorf("classification = %q", out.Reqs[0].Class)
	}
	containsAll(t, out.Reqs[0].Controls, "GOV-POL-01")
	if out.Reqs[0].Coverage == "not_covered" {
		t.Errorf("req 1 should match a control: %+v", out.Reqs[0])
	}
	if out.Reqs[1].Coverage != "controls_identified" || out.Reqs[1].Open == 0 {
		t.Errorf("req 2 = %+v", out.Reqs[1])
	}
	if out.Reqs[2].Coverage != "not_covered" || len(out.Reqs[2].Controls) != 0 {
		t.Errorf("req 3 = %+v", out.Reqs[2])
	}
	if out.Reqs[3].Evidence == 0 {
		t.Errorf("req 4 should have evidence from F-001: %+v", out.Reqs[3])
	}
	sum := 0
	for _, v := range out.Summary {
		sum += v
	}
	if sum != 4 || out.Summary["not_covered"] != 1 {
		t.Errorf("coverage summary = %v", out.Summary)
	}

	text, isErr := callTool(t, s, "assess_bid_requirements", map[string]any{"requirements_json": "not json"})
	if !isErr || !strings.Contains(text, "invalid JSON") {
		t.Errorf("bad json: %v %q", isErr, text)
	}
	text, isErr = callTool(t, s, "assess_bid_requirements", nil)
	if !isErr || !strings.Contains(text, "requirements_json parameter is required") {
		t.Errorf("missing arg: %v %q", isErr, text)
	}
}

func TestGenerateEvidenceSummaryTool(t *testing.T) {
	s := fullServer(loadedData(t))
	var out struct {
		ControlID string `json:"control_id"`
		Title     string `json:"title"`
		Status    string `json:"status"`
		URL       string `json:"url"`
		Evidence  []struct {
			FindingID string `json:"finding_id"`
			Evidence  []struct {
				Type string `json:"type"`
				Ref  string `json:"ref"`
			} `json:"evidence"`
		} `json:"evidence"`
		FwCoverage []struct {
			Framework string `json:"framework"`
		} `json:"framework_coverage"`
		Total int `json:"total_evidence_items"`
	}
	callJSON(t, s, "generate_evidence_summary", map[string]any{"control_id": "SEC-AUTH-01"}, &out)
	if out.ControlID != "SEC-AUTH-01" || out.Title != "Authentication mechanism" || out.Status != "verified" {
		t.Errorf("header = %+v", out)
	}
	if out.Total != 1 || out.Evidence[0].FindingID != "F-001" || out.Evidence[0].Evidence[0].Type != "merged_pr" ||
		out.Evidence[0].Evidence[0].Ref != "sirosfoundation/wallet-frontend#74" {
		t.Errorf("evidence = %+v", out.Evidence)
	}
	if len(out.FwCoverage) != 3 {
		t.Errorf("framework coverage = %+v", out.FwCoverage)
	}
	if out.URL != "https://test.example.com/controls/technical/sec_auth_01" {
		t.Errorf("url = %q", out.URL)
	}

	callJSON(t, s, "generate_evidence_summary", map[string]any{"control_id": "SEC-WEB-01"}, &out)
	// F-004 (accepted) is linked to SEC-WEB-01 and carries evidence; F-002 has none.
	if out.Total != 1 || out.Evidence[0].FindingID != "F-004" {
		t.Errorf("SEC-WEB-01 evidence = %+v", out.Evidence)
	}

	text, isErr := callTool(t, s, "generate_evidence_summary", map[string]any{"control_id": "NOPE"})
	if !isErr || !strings.Contains(text, `control "NOPE" not found`) {
		t.Errorf("unknown control: %v %q", isErr, text)
	}
	text, isErr = callTool(t, s, "generate_evidence_summary", nil)
	if !isErr || !strings.Contains(text, "control_id parameter is required") {
		t.Errorf("missing control_id: %v %q", isErr, text)
	}
}

func TestPrompts(t *testing.T) {
	s := fullServer(loadedData(t))

	p := getPrompt(t, s, "compliance_assessment", map[string]string{"framework": "iso27001"})
	for _, want := range []string{"Compliance assessment prompt for iso27001", "against the iso27001 framework", `framework="iso27001"`, "compliance_gap_analysis"} {
		if !strings.Contains(p, want) {
			t.Errorf("compliance_assessment lacks %q", want)
		}
	}
	p = getPrompt(t, s, "audit_preparation", map[string]string{"framework": "gdpr"})
	for _, want := range []string{"Audit preparation prompt for gdpr", `for "gdpr"`, "list_architecture_docs"} {
		if !strings.Contains(p, want) {
			t.Errorf("audit_preparation lacks %q", want)
		}
	}
	p = getPrompt(t, s, "risk_review", nil)
	for _, want := range []string{"Risk register review prompt", "grc://risk/methodology", "risk_summary"} {
		if !strings.Contains(p, want) {
			t.Errorf("risk_review lacks %q", want)
		}
	}
	p = getPrompt(t, s, "bid_response", map[string]string{"bid_name": "Acme EUDI", "component_scope": "Wallet"})
	for _, want := range []string{"Bid response workflow for Acme EUDI", "Scope: Wallet", "# Bid Response: Acme EUDI", "assess_bid_requirements"} {
		if !strings.Contains(p, want) {
			t.Errorf("bid_response lacks %q", want)
		}
	}
	p = getPrompt(t, s, "bid_response", map[string]string{"bid_name": "Acme"})
	if !strings.Contains(p, "Scope: all components") {
		t.Error("default scope not applied")
	}

	if _, errMsg := rpc(t, s, "prompts/get", map[string]any{"name": "nope"}); errMsg == "" {
		t.Error("unknown prompt should error")
	}
}

func TestListingsAdvertiseEverything(t *testing.T) {
	s := fullServer(loadedData(t))
	check := func(method, key string, want ...string) {
		res, errMsg := rpc(t, s, method, map[string]any{})
		if errMsg != "" {
			t.Fatal(errMsg)
		}
		var m map[string][]map[string]any
		if err := json.Unmarshal(res, &m); err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, it := range m[key] {
			for _, k := range []string{"name", "uriTemplate", "uri"} {
				if v, ok := it[k].(string); ok {
					got = append(got, v)
				}
			}
		}
		containsAll(t, got, want...)
	}
	check("tools/list", "tools", "search_controls", "search_findings", "compliance_gap_analysis", "finding_statistics",
		"risk_summary", "list_architecture_docs", "control_coverage", "map_bid_requirement", "assess_bid_requirements", "generate_evidence_summary")
	check("prompts/list", "prompts", "compliance_assessment", "audit_preparation", "risk_review", "bid_response")
	check("resources/list", "resources", "grc://config", "grc://catalog", "grc://audit/findings", "grc://risk/methodology", "grc://risk/register")
	check("resources/templates/list", "resourceTemplates", "grc://catalog/control/{controlId}", "grc://audit/finding/{findingId}", "grc://mapping/{frameworkId}", "grc://architecture/{document}")
}

func TestNewMCPHandlerServesInitialize(t *testing.T) {
	d := loadedData(t)
	h := newMCPHandler(d)
	if h == nil {
		t.Fatal("nil handler")
	}
	// Exercise the HTTP transport end to end: initialize returns the instructions.
	rec := newRecorder()
	h.ServeHTTP(rec, newPostRequest(t, initializeBody()))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "grc-compliance") || !strings.Contains(rec.Body.String(), "grc://risk/methodology") {
		t.Errorf("initialize: %d %s", rec.Code, rec.Body.String())
	}
}

func TestControlURLHelpers(t *testing.T) {
	d := loadedData(t)
	if got := controlURL("", d.catalog, "SEC-AUTH-01"); got != "" {
		t.Errorf("empty base URL must give empty URL, got %q", got)
	}
	if got := controlURL("https://x.example/", d.catalog, "GOV-POL-01"); got != "https://x.example/controls/organizational/gov_pol_01" {
		t.Errorf("got %q", got)
	}
	// Unknown controls default to the technical group.
	if got := controlGroupDir(d.catalog, "UNKNOWN"); got != "technical" {
		t.Errorf("got %q", got)
	}
	if got := availableFrameworks(d.mappings); got != "eudi, gdpr, iso27001" {
		t.Errorf("got %q", got)
	}
}

func TestMatchKeywords(t *testing.T) {
	d := loadedData(t)
	c := d.catalog.Controls["SEC-AUTH-01"]
	c.References = []string{"ETSI TS 119 472-1"}
	c.Components = []string{"Wallet"}
	search := "etsi ts 119 472-1 wallet authentication with oauth access tokens"
	control := strings.ToLower(c.ID + " " + c.Title + " " + c.Description + " etsi ts 119 472-1 wallet")
	reasons := strings.Join(matchKeywords(search, control, c), "|")
	for _, want := range []string{"title keyword: authentication", "component: Wallet", "reference: ETSI TS 119 472-1", "reference fragment:", "domain match:"} {
		if !strings.Contains(reasons, want) {
			t.Errorf("reasons lack %q: %s", want, reasons)
		}
	}
	if got := matchKeywords("zzz", "yyy", c); len(got) != 0 {
		t.Errorf("unexpected reasons %v", got)
	}
}

func newRecorder() *httptest.ResponseRecorder { return httptest.NewRecorder() }

func newPostRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	return r
}

func initializeBody() string {
	return `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"` + mcp.LATEST_PROTOCOL_VERSION + `","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`
}
