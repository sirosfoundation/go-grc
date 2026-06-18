package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/sirosfoundation/go-grc/pkg/audit"
	"github.com/sirosfoundation/go-grc/pkg/catalog"
	"github.com/sirosfoundation/go-grc/pkg/config"
	"github.com/sirosfoundation/go-grc/pkg/mapping"
	"github.com/sirosfoundation/go-grc/pkg/risk"
	"github.com/sirosfoundation/go-grc/pkg/yearcycle"
)

// complianceData holds loaded compliance data and provides thread-safe access.
// It is refreshed after each site rebuild.
type complianceData struct {
	mu       sync.RWMutex
	cfg      *config.Config
	catalog  *catalog.Catalog
	audits   *audit.AuditSet
	risks    *risk.RiskSet
	mappings mapping.Mappings
	cycles   []*yearcycle.YearCycle
	fwCats   map[string]*catalog.FrameworkCatalog
	profile  string
	root     string
}

func newComplianceData(root, profile string) *complianceData {
	return &complianceData{root: root, profile: profile}
}

// reload loads or reloads all compliance data from disk.
func (cd *complianceData) reload() error {
	cfg, err := config.New(cd.root)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	cat, err := catalog.Load(cfg.CatalogDir, cfg.CatalogSubdirs...)
	if err != nil {
		return fmt.Errorf("loading catalog: %w", err)
	}

	audits, err := audit.Load(cfg.AuditsDir)
	if err != nil {
		return fmt.Errorf("loading audits: %w", err)
	}

	maps, err := mapping.Load(cfg.MappingsDir, cfg.Frameworks)
	if err != nil {
		return fmt.Errorf("loading mappings: %w", err)
	}

	catalog.DeriveControlStatuses(cat, audits)

	var risks *risk.RiskSet
	if cfg.RiskDir != "" {
		risks, err = risk.Load(cfg.RiskDir, cfg.RiskRegister.Files)
		if err != nil {
			log.Printf("MCP: warning: loading risk register: %v", err)
		}
	}

	var cycles []*yearcycle.YearCycle
	if cfg.YearCycle.Source != "" {
		yc, err := yearcycle.Load(cfg.YearCycle.Source, cfg.YearCycle.Title)
		if err != nil {
			log.Printf("MCP: warning: loading year cycle: %v", err)
		} else {
			cycles = append(cycles, yc)
		}
	}

	fwCats := make(map[string]*catalog.FrameworkCatalog)
	for _, fw := range cfg.Frameworks {
		name := strings.TrimSuffix(fw.CatalogFile, ".yaml")
		name = strings.TrimSuffix(name, ".yml")
		fwCat, _ := catalog.LoadFrameworkCatalog(cfg.CatalogDir, name, fw.CatalogSections)
		if fwCat != nil {
			fwCats[fw.ID] = fwCat
		}
	}

	cd.mu.Lock()
	defer cd.mu.Unlock()
	cd.cfg = cfg
	cd.catalog = cat
	cd.audits = audits
	cd.risks = risks
	cd.mappings = maps
	cd.cycles = cycles
	cd.fwCats = fwCats
	return nil
}

// newMCPHandler creates the MCP server and returns a StreamableHTTPServer
// that can be mounted as an http.Handler.
func newMCPHandler(data *complianceData) *mcpserver.StreamableHTTPServer {
	s := mcpserver.NewMCPServer(
		"grc-compliance",
		"0.11.0",
		mcpserver.WithResourceCapabilities(false, false),
		mcpserver.WithInstructions(`You are a compliance and security assessment assistant with access to the SIROS Foundation's
Governance, Risk & Compliance (GRC) data. You have access to:

- Security controls catalog with implementation status
- Audit findings with severity and remediation status
- Risk register with accepted/transferred risks
- Framework compliance mappings (EUDI, ISO 27001, GDPR, OWASP ASVS)
- Architecture documentation (threat models, crypto inventory, network architecture, etc.)
- Year cycle of compliance activities

Use this data to help with compliance assessments, audit preparation, gap analysis,
risk reviews, architecture security analysis, and bid requirement responses.

When responding to bid requirements:
- Map each requirement to existing controls and evidence
- Clearly distinguish between "fully covered", "partially covered", and "not covered"
- Provide specific evidence references for compliance claims
- Generate explanatory text suitable for inclusion in bid responses
- Highlight gaps that need attention or new controls
- Reference framework mappings when the bid cites known standards

Always cite specific control IDs, finding IDs, and framework requirements when making
recommendations or compliance claims.`),
	)

	registerResources(s, data)
	registerResourceTemplates(s, data)
	registerTools(s, data)
	registerBidTools(s, data)
	registerPrompts(s, data)

	return mcpserver.NewStreamableHTTPServer(s)
}

// --- Resources ---

func registerResources(s *mcpserver.MCPServer, data *complianceData) {
	// Configuration overview
	s.AddResource(
		mcp.NewResource("grc://config", "Configuration",
			mcp.WithResourceDescription("GRC configuration: project, frameworks, profiles, and components"),
			mcp.WithMIMEType("application/json"),
		),
		func(_ context.Context, _ mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			data.mu.RLock()
			defer data.mu.RUnlock()
			return jsonResource("grc://config", map[string]any{
				"project":    data.cfg.Project,
				"frameworks": data.cfg.Frameworks,
				"profiles":   data.cfg.Profiles,
				"components": data.cfg.Components,
			})
		},
	)

	// Full control catalog
	s.AddResource(
		mcp.NewResource("grc://catalog", "Control Catalog",
			mcp.WithResourceDescription("All security controls grouped by category with implementation status"),
			mcp.WithMIMEType("application/json"),
		),
		func(_ context.Context, _ mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			data.mu.RLock()
			defer data.mu.RUnlock()
			type controlSummary struct {
				ID       string `json:"id"`
				Title    string `json:"title"`
				Status   string `json:"status"`
				Category string `json:"category"`
				Group    string `json:"group"`
				URL      string `json:"url,omitempty"`
			}
			baseURL := data.cfg.Project.URL
			var controls []controlSummary
			for _, g := range data.catalog.Groups {
				for i := range g.Controls {
					c := &g.Controls[i]
					controls = append(controls, controlSummary{
						ID:       c.ID,
						Title:    c.Title,
						Status:   catalog.EffectiveStatus(c),
						Category: c.Category,
						Group:    g.Title,
						URL:      controlURL(baseURL, data.catalog, c.ID),
					})
				}
			}
			return jsonResource("grc://catalog", controls)
		},
	)

	// All findings
	s.AddResource(
		mcp.NewResource("grc://audit/findings", "Audit Findings",
			mcp.WithResourceDescription("All audit findings with severity, status, and linked controls"),
			mcp.WithMIMEType("application/json"),
		),
		func(_ context.Context, _ mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			data.mu.RLock()
			defer data.mu.RUnlock()
			type findingSummary struct {
				ID       string   `json:"id"`
				Title    string   `json:"title"`
				Severity string   `json:"severity"`
				Status   string   `json:"status"`
				Controls []string `json:"controls"`
			}
			var findings []findingSummary
			for id, ref := range data.audits.FindingsByID {
				f := ref.Finding
				findings = append(findings, findingSummary{
					ID:       id,
					Title:    f.Title,
					Severity: f.Severity,
					Status:   f.Status,
					Controls: f.Controls,
				})
			}
			sort.Slice(findings, func(i, j int) bool {
				return findings[i].ID < findings[j].ID
			})
			return jsonResource("grc://audit/findings", findings)
		},
	)

	// Risk register
	s.AddResource(
		mcp.NewResource("grc://risk/register", "Risk Register",
			mcp.WithResourceDescription("Accepted and transferred risks with residual severity and compensating controls"),
			mcp.WithMIMEType("application/json"),
		),
		func(_ context.Context, _ mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			data.mu.RLock()
			defer data.mu.RUnlock()
			if data.risks == nil {
				return jsonResource("grc://risk/register", map[string]any{"risks": []any{}})
			}
			type riskSummary struct {
				ID                   string   `json:"id"`
				FindingID            string   `json:"finding_id"`
				Severity             string   `json:"severity"`
				ResidualSeverity     string   `json:"residual_severity"`
				Status               string   `json:"status"`
				CompensatingControls []string `json:"compensating_controls"`
			}
			var risks []riskSummary
			for id, ref := range data.risks.RisksByID {
				r := ref.Risk
				risks = append(risks, riskSummary{
					ID:                   id,
					FindingID:            r.Finding,
					Severity:             r.Severity,
					ResidualSeverity:     r.ResidualSeverity,
					Status:               r.Status,
					CompensatingControls: r.CompensatingControls,
				})
			}
			sort.Slice(risks, func(i, j int) bool {
				return risks[i].ID < risks[j].ID
			})
			return jsonResource("grc://risk/register", risks)
		},
	)
}

// --- Resource Templates ---

func registerResourceTemplates(s *mcpserver.MCPServer, data *complianceData) {
	// Individual control
	s.AddResourceTemplate(
		mcp.NewResourceTemplate("grc://catalog/control/{controlId}", "Control Detail",
			mcp.WithTemplateDescription("Detailed information for a specific security control"),
		),
		func(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			data.mu.RLock()
			defer data.mu.RUnlock()
			controlID := extractTemplateParam(req.Params.URI, "grc://catalog/control/")
			ctrl, ok := data.catalog.Controls[controlID]
			if !ok {
				return nil, fmt.Errorf("control %q not found", controlID)
			}
			return jsonResource(req.Params.URI, ctrl)
		},
	)

	// Individual finding
	s.AddResourceTemplate(
		mcp.NewResourceTemplate("grc://audit/finding/{findingId}", "Finding Detail",
			mcp.WithTemplateDescription("Detailed information for a specific audit finding"),
		),
		func(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			data.mu.RLock()
			defer data.mu.RUnlock()
			findingID := extractTemplateParam(req.Params.URI, "grc://audit/finding/")
			ref, ok := data.audits.FindingsByID[findingID]
			if !ok {
				return nil, fmt.Errorf("finding %q not found", findingID)
			}
			return jsonResource(req.Params.URI, ref.Finding)
		},
	)

	// Framework mapping
	s.AddResourceTemplate(
		mcp.NewResourceTemplate("grc://mapping/{frameworkId}", "Framework Mapping",
			mcp.WithTemplateDescription("Control-to-requirement mapping for a specific compliance framework"),
		),
		func(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			data.mu.RLock()
			defer data.mu.RUnlock()
			fwID := extractTemplateParam(req.Params.URI, "grc://mapping/")
			fm, ok := data.mappings[fwID]
			if !ok {
				return nil, fmt.Errorf("framework mapping %q not found", fwID)
			}
			return jsonResource(req.Params.URI, fm.Entries)
		},
	)

	// Architecture document
	s.AddResourceTemplate(
		mcp.NewResourceTemplate("grc://architecture/{document}", "Architecture Document",
			mcp.WithTemplateDescription("Architecture security documentation (threat models, network diagrams, etc.)"),
		),
		func(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			data.mu.RLock()
			defer data.mu.RUnlock()
			docName := extractTemplateParam(req.Params.URI, "grc://architecture/")
			archDir := filepath.Join(data.root, "architecture")
			// Sanitize to prevent path traversal
			docName = filepath.Base(docName)
			if !strings.HasSuffix(docName, ".md") {
				docName += ".md"
			}
			content, err := os.ReadFile(filepath.Join(archDir, docName))
			if err != nil {
				return nil, fmt.Errorf("architecture document %q not found", docName)
			}
			return []mcp.ResourceContents{
				mcp.TextResourceContents{
					URI:      req.Params.URI,
					MIMEType: "text/markdown",
					Text:     string(content),
				},
			}, nil
		},
	)
}

// --- Tools ---

func registerTools(s *mcpserver.MCPServer, data *complianceData) {
	// Search controls
	s.AddTool(
		mcp.NewTool("search_controls",
			mcp.WithDescription("Search security controls by keyword, status, or category"),
			mcp.WithString("query", mcp.Description("Search term to match against control ID, title, or description")),
			mcp.WithString("status", mcp.Description("Filter by status: implemented, partial, planned, not-started")),
			mcp.WithString("category", mcp.Description("Filter by category: technical, policy, process, physical")),
		),
		func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			data.mu.RLock()
			defer data.mu.RUnlock()
			query := strings.ToLower(req.GetString("query", ""))
			status := strings.ToLower(req.GetString("status", ""))
			category := strings.ToLower(req.GetString("category", ""))

			type result struct {
				ID       string `json:"id"`
				Title    string `json:"title"`
				Status   string `json:"status"`
				Category string `json:"category"`
				Group    string `json:"group"`
				URL      string `json:"url,omitempty"`
			}
			baseURL := data.cfg.Project.URL
			var results []result
			for _, g := range data.catalog.Groups {
				for i := range g.Controls {
					c := &g.Controls[i]
					effStatus := catalog.EffectiveStatus(c)
					if status != "" && strings.ToLower(effStatus) != status {
						continue
					}
					if category != "" && strings.ToLower(c.Category) != category {
						continue
					}
					if query != "" {
						haystack := strings.ToLower(c.ID + " " + c.Title + " " + c.Description)
						if !strings.Contains(haystack, query) {
							continue
						}
					}
					results = append(results, result{
						ID:       c.ID,
						Title:    c.Title,
						Status:   effStatus,
						Category: c.Category,
						Group:    g.Title,
						URL:      controlURL(baseURL, data.catalog, c.ID),
					})
				}
			}
			return toolResultJSON(results)
		},
	)

	// Search findings
	s.AddTool(
		mcp.NewTool("search_findings",
			mcp.WithDescription("Search audit findings by keyword, severity, or status"),
			mcp.WithString("query", mcp.Description("Search term to match against finding ID, title, or description")),
			mcp.WithString("severity", mcp.Description("Filter by severity: critical, high, medium, low, informational")),
			mcp.WithString("status", mcp.Description("Filter by status: open, in-progress, resolved, accepted")),
		),
		func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			data.mu.RLock()
			defer data.mu.RUnlock()
			query := strings.ToLower(req.GetString("query", ""))
			severity := strings.ToLower(req.GetString("severity", ""))
			status := strings.ToLower(req.GetString("status", ""))

			type result struct {
				ID       string   `json:"id"`
				Title    string   `json:"title"`
				Severity string   `json:"severity"`
				Status   string   `json:"status"`
				Controls []string `json:"controls"`
			}
			var results []result
			for id, ref := range data.audits.FindingsByID {
				f := ref.Finding
				if severity != "" && strings.ToLower(f.Severity) != severity {
					continue
				}
				if status != "" && strings.ToLower(f.Status) != status {
					continue
				}
				if query != "" {
					haystack := strings.ToLower(id + " " + f.Title + " " + f.Description)
					if !strings.Contains(haystack, query) {
						continue
					}
				}
				results = append(results, result{
					ID:       id,
					Title:    f.Title,
					Severity: f.Severity,
					Status:   f.Status,
					Controls: f.Controls,
				})
			}
			sort.Slice(results, func(i, j int) bool {
				return results[i].ID < results[j].ID
			})
			return toolResultJSON(results)
		},
	)

	// Compliance gap analysis
	s.AddTool(
		mcp.NewTool("compliance_gap_analysis",
			mcp.WithDescription("Analyze compliance gaps for a specific framework, showing unmapped or partially covered requirements"),
			mcp.WithString("framework", mcp.Description("Framework ID (e.g., eudi, iso27001, gdpr, owasp-asvs)"), mcp.Required()),
		),
		func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			data.mu.RLock()
			defer data.mu.RUnlock()
			fwID, err := req.RequireString("framework")
			if err != nil {
				return mcp.NewToolResultError("framework parameter is required"), nil
			}
			fwID = strings.ToLower(fwID)

			fm, ok := data.mappings[fwID]
			if !ok {
				return mcp.NewToolResultError(fmt.Sprintf("framework %q not found; available: %s", fwID, availableFrameworks(data.mappings))), nil
			}

			type gapEntry struct {
				Key        string   `json:"requirement_key"`
				Status     string   `json:"mapping_status"`
				WorkStatus string   `json:"work_status"`
				Controls   []string `json:"controls"`
				Notes      string   `json:"notes,omitempty"`
			}
			var gaps []gapEntry
			var covered, partial, missing int
			for _, e := range fm.Entries {
				switch strings.ToLower(e.Status) {
				case "covered", "full":
					covered++
				case "partial":
					partial++
					gaps = append(gaps, gapEntry{
						Key: e.Key, Status: e.Status, WorkStatus: e.WorkStatus,
						Controls: e.Controls, Notes: e.Notes,
					})
				default:
					missing++
					gaps = append(gaps, gapEntry{
						Key: e.Key, Status: e.Status, WorkStatus: e.WorkStatus,
						Controls: e.Controls, Notes: e.Notes,
					})
				}
			}

			return toolResultJSON(map[string]any{
				"framework": fwID,
				"summary": map[string]int{
					"total":   len(fm.Entries),
					"covered": covered,
					"partial": partial,
					"missing": missing,
				},
				"gaps": gaps,
			})
		},
	)

	// Finding statistics
	s.AddTool(
		mcp.NewTool("finding_statistics",
			mcp.WithDescription("Get summary statistics of audit findings by severity and status"),
		),
		func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			data.mu.RLock()
			defer data.mu.RUnlock()

			bySeverity := make(map[string]int)
			byStatus := make(map[string]int)
			var active, resolved, total int
			for _, ref := range data.audits.FindingsByID {
				f := ref.Finding
				total++
				bySeverity[f.Severity]++
				byStatus[f.Status]++
				if f.IsActive() {
					active++
				}
				if f.IsResolved() {
					resolved++
				}
			}
			return toolResultJSON(map[string]any{
				"total":       total,
				"active":      active,
				"resolved":    resolved,
				"by_severity": bySeverity,
				"by_status":   byStatus,
			})
		},
	)

	// Risk summary
	s.AddTool(
		mcp.NewTool("risk_summary",
			mcp.WithDescription("Get a summary of the risk register including overdue reviews"),
		),
		func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			data.mu.RLock()
			defer data.mu.RUnlock()
			if data.risks == nil {
				return toolResultJSON(map[string]any{"message": "no risk register configured"})
			}

			byStatus := make(map[string]int)
			bySeverity := make(map[string]int)
			var overdue int
			for _, ref := range data.risks.RisksByID {
				r := ref.Risk
				byStatus[r.Status]++
				bySeverity[r.ResidualSeverity]++
			}
			for _, lf := range data.risks.Files {
				if risk.IsOverdueRegister(lf.Data.Register) {
					overdue++
				}
			}
			return toolResultJSON(map[string]any{
				"total_risks":          len(data.risks.RisksByID),
				"by_status":            byStatus,
				"by_residual_severity": bySeverity,
				"overdue_registers":    overdue,
			})
		},
	)

	// List architecture documents
	s.AddTool(
		mcp.NewTool("list_architecture_docs",
			mcp.WithDescription("List available architecture and security documentation"),
		),
		func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			archDir := filepath.Join(data.root, "architecture")
			entries, err := os.ReadDir(archDir)
			if err != nil {
				return mcp.NewToolResultError("no architecture directory found"), nil
			}
			type doc struct {
				Name string `json:"name"`
				URI  string `json:"uri"`
			}
			var docs []doc
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
					name := strings.TrimSuffix(e.Name(), ".md")
					docs = append(docs, doc{
						Name: name,
						URI:  "grc://architecture/" + name,
					})
				}
			}
			return toolResultJSON(docs)
		},
	)

	// Control coverage across frameworks
	s.AddTool(
		mcp.NewTool("control_coverage",
			mcp.WithDescription("Show which frameworks reference a given control and the mapping status"),
			mcp.WithString("control_id", mcp.Description("The control ID to check coverage for"), mcp.Required()),
		),
		func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			data.mu.RLock()
			defer data.mu.RUnlock()
			controlID, err := req.RequireString("control_id")
			if err != nil {
				return mcp.NewToolResultError("control_id parameter is required"), nil
			}

			type fwCoverage struct {
				Framework string `json:"framework"`
				Key       string `json:"requirement_key"`
				Status    string `json:"status"`
			}
			var coverage []fwCoverage
			for fwID, fm := range data.mappings {
				for _, e := range fm.Entries {
					for _, c := range e.Controls {
						if strings.EqualFold(c, controlID) {
							coverage = append(coverage, fwCoverage{
								Framework: fwID,
								Key:       e.Key,
								Status:    e.Status,
							})
						}
					}
				}
			}
			sort.Slice(coverage, func(i, j int) bool {
				return coverage[i].Framework < coverage[j].Framework
			})
			return toolResultJSON(map[string]any{
				"control_id":    controlID,
				"referenced_in": len(coverage),
				"coverage":      coverage,
			})
		},
	)
}

// --- Bid Response Tools ---

func registerBidTools(s *mcpserver.MCPServer, data *complianceData) {
	// Map a single bid requirement to existing controls and evidence
	s.AddTool(
		mcp.NewTool("map_bid_requirement",
			mcp.WithDescription(`Map a single bid requirement to existing controls, evidence, and framework coverage.
Returns matching controls with implementation status, relevant evidence from audit findings,
cross-framework references, and a coverage assessment. Use this to build bid responses
requirement by requirement.`),
			mcp.WithString("requirement_id", mcp.Description("The bid requirement ID (e.g. '1.1', '2.5')"), mcp.Required()),
			mcp.WithString("requirement_text", mcp.Description("The full text of the bid requirement"), mcp.Required()),
			mcp.WithString("classification", mcp.Description("Requirement classification if provided (e.g. MANDATORY, SCORED, FUTURE-STATE)")),
			mcp.WithString("standards_refs", mcp.Description("Referenced technical standards or specifications (e.g. 'ETSI TS 119 472-1, ISO 18013-5')")),
			mcp.WithString("evidence_requirement", mcp.Description("Specific evidence the bid asks for, if stated")),
			mcp.WithString("component", mcp.Description("Component or domain the requirement applies to (e.g. 'Wallet', 'Issuer', 'Trust infrastructure')")),
		),
		func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			data.mu.RLock()
			defer data.mu.RUnlock()

			reqID, _ := req.RequireString("requirement_id")
			reqText, _ := req.RequireString("requirement_text")
			classification := req.GetString("classification", "")
			standardsRefs := req.GetString("standards_refs", "")
			evidenceReq := req.GetString("evidence_requirement", "")
			component := req.GetString("component", "")

			// Build search terms from the requirement text
			searchText := strings.ToLower(reqText + " " + standardsRefs + " " + component)

			// 1. Find matching controls by keyword similarity
			baseURL := data.cfg.Project.URL
			type controlMatch struct {
				ID          string   `json:"id"`
				Title       string   `json:"title"`
				Description string   `json:"description"`
				Status      string   `json:"status"`
				Category    string   `json:"category"`
				Owner       string   `json:"owner"`
				References  []string `json:"references,omitempty"`
				URL         string   `json:"url,omitempty"`
				MatchReason string   `json:"match_reason"`
			}
			var matchedControls []controlMatch
			for _, g := range data.catalog.Groups {
				for i := range g.Controls {
					c := &g.Controls[i]
					controlText := strings.ToLower(c.ID + " " + c.Title + " " + c.Description +
						" " + strings.Join(c.References, " ") + " " + strings.Join(c.Components, " "))

					// Score keyword overlap
					reasons := matchKeywords(searchText, controlText, c)
					if len(reasons) > 0 {
						matchedControls = append(matchedControls, controlMatch{
							ID:          c.ID,
							Title:       c.Title,
							Description: c.Description,
							Status:      catalog.EffectiveStatus(c),
							Category:    c.Category,
							Owner:       c.Owner,
							References:  c.References,
							URL:         controlURL(baseURL, data.catalog, c.ID),
							MatchReason: strings.Join(reasons, "; "),
						})
					}
				}
			}

			// 2. Find evidence from audit findings linked to matched controls
			type evidenceItem struct {
				FindingID   string           `json:"finding_id"`
				Title       string           `json:"title"`
				Status      string           `json:"status"`
				Severity    string           `json:"severity"`
				Controls    []string         `json:"controls"`
				Evidence    []audit.Evidence `json:"evidence,omitempty"`
				Description string           `json:"description,omitempty"`
			}
			seenFindings := make(map[string]bool)
			var evidenceItems []evidenceItem
			for _, mc := range matchedControls {
				refs := data.audits.FindingsByControl[mc.ID]
				for _, ref := range refs {
					f := ref.Finding
					if seenFindings[f.ID] {
						continue
					}
					seenFindings[f.ID] = true
					ei := evidenceItem{
						FindingID: f.ID,
						Title:     f.Title,
						Status:    f.Status,
						Severity:  f.Severity,
						Controls:  f.Controls,
					}
					if f.HasEvidence() {
						ei.Evidence = f.Evidence
					}
					if f.IsActive() {
						ei.Description = f.Description
					}
					evidenceItems = append(evidenceItems, ei)
				}
			}

			// 3. Find framework mapping references that mention matching controls
			type fwRef struct {
				Framework string `json:"framework"`
				ReqKey    string `json:"requirement_key"`
				Status    string `json:"mapping_status"`
				Notes     string `json:"notes,omitempty"`
			}
			var frameworkRefs []fwRef
			controlSet := make(map[string]bool)
			for _, mc := range matchedControls {
				controlSet[mc.ID] = true
			}
			for fwID, fm := range data.mappings {
				for _, e := range fm.Entries {
					for _, c := range e.Controls {
						if controlSet[c] {
							frameworkRefs = append(frameworkRefs, fwRef{
								Framework: fwID,
								ReqKey:    e.Key,
								Status:    e.Status,
								Notes:     e.Notes,
							})
							break
						}
					}
				}
			}
			sort.Slice(frameworkRefs, func(i, j int) bool {
				if frameworkRefs[i].Framework != frameworkRefs[j].Framework {
					return frameworkRefs[i].Framework < frameworkRefs[j].Framework
				}
				return frameworkRefs[i].ReqKey < frameworkRefs[j].ReqKey
			})

			// 4. Check for open issues that might affect compliance claims
			var openIssues []evidenceItem
			for _, ei := range evidenceItems {
				if ei.Status != "resolved" && ei.Severity != "" {
					openIssues = append(openIssues, ei)
				}
			}

			// 5. Determine coverage level
			coverage := "not_covered"
			if len(matchedControls) > 0 {
				allVerified := true
				anyVerified := false
				for _, mc := range matchedControls {
					if mc.Status == "verified" || mc.Status == "validated" {
						anyVerified = true
					} else {
						allVerified = false
					}
				}
				if allVerified && len(evidenceItems) > 0 {
					coverage = "fully_covered"
				} else if anyVerified {
					coverage = "partially_covered"
				} else {
					coverage = "controls_identified"
				}
			}

			return toolResultJSON(map[string]any{
				"requirement_id":     reqID,
				"requirement_text":   reqText,
				"classification":     classification,
				"standards_refs":     standardsRefs,
				"evidence_requested": evidenceReq,
				"component":          component,
				"coverage_level":     coverage,
				"matched_controls":   matchedControls,
				"evidence":           evidenceItems,
				"framework_refs":     frameworkRefs,
				"open_issues":        openIssues,
				"summary": map[string]any{
					"controls_matched":  len(matchedControls),
					"evidence_items":    len(evidenceItems),
					"framework_refs":    len(frameworkRefs),
					"open_issues_count": len(openIssues),
				},
			})
		},
	)

	// Batch assessment of multiple bid requirements
	s.AddTool(
		mcp.NewTool("assess_bid_requirements",
			mcp.WithDescription(`Assess a batch of bid requirements against existing controls.
Takes a JSON array of requirements (each with id, text, and optional classification/standards/component)
and returns a coverage matrix with summary statistics. Use this after extracting requirements
from a bid document to get a quick overview before deep-diving with map_bid_requirement.`),
			mcp.WithString("requirements_json", mcp.Description(`JSON array of requirements, each object having:
- "id": requirement ID (required)
- "text": requirement text (required)
- "classification": e.g. MANDATORY, SCORED (optional)
- "standards": referenced standards (optional)
- "component": component/domain (optional)`), mcp.Required()),
		),
		func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			data.mu.RLock()
			defer data.mu.RUnlock()

			reqJSON, err := req.RequireString("requirements_json")
			if err != nil {
				return mcp.NewToolResultError("requirements_json parameter is required"), nil
			}

			type bidReq struct {
				ID             string `json:"id"`
				Text           string `json:"text"`
				Classification string `json:"classification"`
				Standards      string `json:"standards"`
				Component      string `json:"component"`
			}
			var reqs []bidReq
			if err := json.Unmarshal([]byte(reqJSON), &reqs); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid JSON: %v", err)), nil
			}

			type reqResult struct {
				ID             string   `json:"id"`
				Text           string   `json:"text"`
				Classification string   `json:"classification,omitempty"`
				CoverageLevel  string   `json:"coverage_level"`
				ControlIDs     []string `json:"control_ids"`
				EvidenceCount  int      `json:"evidence_count"`
				OpenIssues     int      `json:"open_issues"`
			}

			var results []reqResult
			covCounts := map[string]int{
				"fully_covered":       0,
				"partially_covered":   0,
				"controls_identified": 0,
				"not_covered":         0,
			}

			for _, r := range reqs {
				searchText := strings.ToLower(r.Text + " " + r.Standards + " " + r.Component)

				var controlIDs []string
				anyVerified := false
				allVerified := true
				for _, g := range data.catalog.Groups {
					for i := range g.Controls {
						c := &g.Controls[i]
						controlText := strings.ToLower(c.ID + " " + c.Title + " " + c.Description +
							" " + strings.Join(c.References, " ") + " " + strings.Join(c.Components, " "))
						reasons := matchKeywords(searchText, controlText, c)
						if len(reasons) > 0 {
							controlIDs = append(controlIDs, c.ID)
							status := catalog.EffectiveStatus(c)
							if status == "verified" || status == "validated" {
								anyVerified = true
							} else {
								allVerified = false
							}
						}
					}
				}

				evidCount := 0
				openCount := 0
				seen := make(map[string]bool)
				for _, cid := range controlIDs {
					for _, ref := range data.audits.FindingsByControl[cid] {
						f := ref.Finding
						if seen[f.ID] {
							continue
						}
						seen[f.ID] = true
						if f.HasEvidence() {
							evidCount++
						}
						if f.IsActive() {
							openCount++
						}
					}
				}

				coverage := "not_covered"
				if len(controlIDs) > 0 {
					if allVerified && evidCount > 0 {
						coverage = "fully_covered"
					} else if anyVerified {
						coverage = "partially_covered"
					} else {
						coverage = "controls_identified"
					}
				}
				covCounts[coverage]++

				results = append(results, reqResult{
					ID:             r.ID,
					Text:           r.Text,
					Classification: r.Classification,
					CoverageLevel:  coverage,
					ControlIDs:     controlIDs,
					EvidenceCount:  evidCount,
					OpenIssues:     openCount,
				})
			}

			return toolResultJSON(map[string]any{
				"total_requirements": len(reqs),
				"coverage_summary":   covCounts,
				"requirements":       results,
			})
		},
	)

	// Generate bid response text for a requirement
	s.AddTool(
		mcp.NewTool("generate_evidence_summary",
			mcp.WithDescription(`Generate a structured evidence summary for a specific control suitable for inclusion
in a bid response. Returns all available evidence items, their types, collection dates,
and the control's implementation status with cross-references to framework compliance.`),
			mcp.WithString("control_id", mcp.Description("The control ID to generate evidence summary for"), mcp.Required()),
		),
		func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			data.mu.RLock()
			defer data.mu.RUnlock()

			controlID, err := req.RequireString("control_id")
			if err != nil {
				return mcp.NewToolResultError("control_id parameter is required"), nil
			}

			ctrl, ok := data.catalog.Controls[controlID]
			if !ok {
				return mcp.NewToolResultError(fmt.Sprintf("control %q not found", controlID)), nil
			}

			// Gather all evidence from findings linked to this control
			type evidDetail struct {
				FindingID    string           `json:"finding_id"`
				FindingTitle string           `json:"finding_title"`
				Status       string           `json:"status"`
				Evidence     []audit.Evidence `json:"evidence"`
			}
			var evidDetails []evidDetail
			refs := data.audits.FindingsByControl[controlID]
			for _, ref := range refs {
				f := ref.Finding
				if len(f.Evidence) > 0 {
					evidDetails = append(evidDetails, evidDetail{
						FindingID:    f.ID,
						FindingTitle: f.Title,
						Status:       f.Status,
						Evidence:     f.Evidence,
					})
				}
			}

			// Framework coverage
			type fwCov struct {
				Framework string `json:"framework"`
				ReqKey    string `json:"requirement_key"`
				Status    string `json:"status"`
			}
			var fwCoverage []fwCov
			for fwID, fm := range data.mappings {
				for _, e := range fm.Entries {
					for _, c := range e.Controls {
						if strings.EqualFold(c, controlID) {
							fwCoverage = append(fwCoverage, fwCov{
								Framework: fwID,
								ReqKey:    e.Key,
								Status:    e.Status,
							})
						}
					}
				}
			}

			return toolResultJSON(map[string]any{
				"control_id":           controlID,
				"title":                ctrl.Title,
				"description":          ctrl.Description,
				"status":               catalog.EffectiveStatus(ctrl),
				"category":             ctrl.Category,
				"owner":                ctrl.Owner,
				"references":           ctrl.References,
				"components":           ctrl.Components,
				"url":                  controlURL(data.cfg.Project.URL, data.catalog, controlID),
				"evidence":             evidDetails,
				"framework_coverage":   fwCoverage,
				"total_evidence_items": len(evidDetails),
			})
		},
	)
}

// matchKeywords returns reasons why a control matches a search text.
// It uses domain-relevant keyword groups to find semantic matches.
func matchKeywords(searchText, controlText string, c *catalog.Control) []string {
	var reasons []string

	// Direct ID or title word match
	words := strings.Fields(strings.ToLower(c.Title))
	for _, w := range words {
		if len(w) > 3 && strings.Contains(searchText, w) {
			reasons = append(reasons, "title keyword: "+w)
			break
		}
	}

	// Component match
	for _, comp := range c.Components {
		if strings.Contains(searchText, strings.ToLower(comp)) {
			reasons = append(reasons, "component: "+comp)
		}
	}

	// Reference match (standards like ETSI, ISO, etc.)
	for _, ref := range c.References {
		refLower := strings.ToLower(ref)
		if strings.Contains(searchText, refLower) {
			reasons = append(reasons, "reference: "+ref)
		}
		// Also try matching standard number fragments
		// e.g. "119 472" in "ETSI TS 119 472-1"
		parts := strings.Fields(refLower)
		for _, p := range parts {
			if len(p) > 4 && strings.Contains(searchText, p) {
				reasons = append(reasons, "reference fragment: "+p)
				break
			}
		}
	}

	// Domain keyword groups for semantic matching
	keywordGroups := map[string][]string{
		"issuance":        {"issu", "credential", "attestation", "pid", "eaa"},
		"presentation":    {"present", "verif", "disclos", "selective"},
		"wallet":          {"wallet", "mobile", "wsca", "wscd", "instance"},
		"crypto":          {"crypt", "sign", "seal", "key", "certificate", "tls", "x509", "x.509"},
		"trust":           {"trust", "trusted list", "root of trust"},
		"lifecycle":       {"lifecycle", "revoc", "suspend", "valid", "expir", "status list"},
		"identity":        {"identity", "identif", "onboard", "proofing", "eidas", "loa"},
		"auth":            {"authen", "authoriz", "access", "iam", "oauth", "oidc", "openid"},
		"deployment":      {"deploy", "cloud", "aws", "kubernetes", "docker", "container", "infra"},
		"logging":         {"log", "audit", "monitor", "track", "event"},
		"data_protection": {"gdpr", "privacy", "data protection", "personal data", "pii", "consent"},
		"integration":     {"integrat", "api", "interface", "endpoint", "rest", "protocol"},
		"security":        {"secur", "vulnerab", "threat", "attack", "penetrat"},
		"mdoc":            {"mdoc", "18013", "iso/iec"},
		"sdjwt":           {"sd-jwt", "sdjwt", "selective disclosure"},
		"multi_tenant":    {"tenant", "multi-tenant"},
	}

	matchedGroups := make(map[string]bool)
	for group, keywords := range keywordGroups {
		searchHit := false
		controlHit := false
		for _, kw := range keywords {
			if strings.Contains(searchText, kw) {
				searchHit = true
			}
			if strings.Contains(controlText, kw) {
				controlHit = true
			}
		}
		if searchHit && controlHit {
			matchedGroups[group] = true
		}
	}

	if len(matchedGroups) >= 2 {
		groups := make([]string, 0, len(matchedGroups))
		for g := range matchedGroups {
			groups = append(groups, g)
		}
		sort.Strings(groups)
		reasons = append(reasons, "domain match: "+strings.Join(groups, ", "))
	}

	return reasons
}

// --- Prompts ---

func registerPrompts(s *mcpserver.MCPServer, data *complianceData) {
	s.AddPrompt(
		mcp.NewPrompt("compliance_assessment",
			mcp.WithPromptDescription("System prompt for performing a compliance assessment against a specific framework"),
			mcp.WithArgument("framework",
				mcp.ArgumentDescription("The framework to assess against (e.g., eudi, iso27001, gdpr, owasp-asvs)"),
				mcp.RequiredArgument(),
			),
		),
		func(_ context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			fw := req.Params.Arguments["framework"]
			return mcp.NewGetPromptResult(
				fmt.Sprintf("Compliance assessment prompt for %s", fw),
				[]mcp.PromptMessage{
					mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(fmt.Sprintf(`Perform a compliance assessment against the %s framework.

Steps:
1. Use the compliance_gap_analysis tool with framework=%q to identify gaps
2. For each gap, use search_controls to find related controls and their implementation status
3. Use search_findings to check for any related open findings
4. Use control_coverage to see cross-framework coverage for key controls
5. Summarize:
   - Overall compliance posture (percentage covered)
   - Critical gaps that need immediate attention
   - Partially covered areas with improvement recommendations
   - Findings that block compliance claims
   - Risk-accepted items and their justification

Cite specific control IDs, finding IDs, and framework requirement keys throughout.`, fw, fw))),
				},
			), nil
		},
	)

	s.AddPrompt(
		mcp.NewPrompt("audit_preparation",
			mcp.WithPromptDescription("Prepare for an upcoming compliance audit by gathering evidence and identifying weak areas"),
			mcp.WithArgument("framework",
				mcp.ArgumentDescription("The framework being audited (e.g., eudi, iso27001, gdpr, owasp-asvs)"),
				mcp.RequiredArgument(),
			),
		),
		func(_ context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			fw := req.Params.Arguments["framework"]
			return mcp.NewGetPromptResult(
				fmt.Sprintf("Audit preparation prompt for %s", fw),
				[]mcp.PromptMessage{
					mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(fmt.Sprintf(`Prepare for an upcoming %s compliance audit.

Steps:
1. Run compliance_gap_analysis for %q to get current coverage
2. Use finding_statistics to understand the overall finding landscape
3. Search for any open critical/high findings using search_findings
4. Check the risk_summary for accepted risks and overdue reviews
5. List architecture documents using list_architecture_docs and review relevant ones

Produce an audit readiness report covering:
- Executive summary of compliance posture
- Evidence inventory: which controls have evidence, which need more
- Open findings that auditors will flag (grouped by severity)
- Risk acceptances that need updated justification
- Recommended actions before the audit (prioritized)
- Architecture documents available as evidence`, fw, fw))),
				},
			), nil
		},
	)

	s.AddPrompt(
		mcp.NewPrompt("risk_review",
			mcp.WithPromptDescription("Review the risk register for overdue items and assess overall risk posture"),
		),
		func(_ context.Context, _ mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return mcp.NewGetPromptResult(
				"Risk register review prompt",
				[]mcp.PromptMessage{
					mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(`Perform a comprehensive risk register review.

Steps:
1. Use risk_summary to get an overview of accepted/transferred risks
2. Read the full risk register resource at grc://risk/register
3. For each risk entry, check the linked finding status using search_findings
4. Use finding_statistics to understand the broader finding landscape
5. Check control_coverage for controls referenced as compensating measures

Produce a risk review report covering:
- Summary of risk posture (counts by status and residual severity)
- Overdue risk register reviews that need attention
- Risks where the underlying finding has been resolved (can be closed)
- Risks where compensating controls have degraded (need re-assessment)
- Recommendations for risk treatment changes
- Items requiring management decision or escalation`)),
				},
			), nil
		},
	)

	s.AddPrompt(
		mcp.NewPrompt("bid_response",
			mcp.WithPromptDescription("Process bid requirements and map them to existing controls, evidence, and compliance coverage"),
			mcp.WithArgument("bid_name",
				mcp.ArgumentDescription("Name or identifier for the bid (e.g. 'DVV Finland EUDI Wallet', 'Swedish eID procurement')"),
				mcp.RequiredArgument(),
			),
			mcp.WithArgument("component_scope",
				mcp.ArgumentDescription("Which components are in scope (e.g. 'Issuer, Wallet, Relying Party' or 'all')"),
			),
		),
		func(_ context.Context, req mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			bidName := req.Params.Arguments["bid_name"]
			scope := req.Params.Arguments["component_scope"]
			if scope == "" {
				scope = "all components"
			}
			return mcp.NewGetPromptResult(
				fmt.Sprintf("Bid response workflow for %s", bidName),
				[]mcp.PromptMessage{
					mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(fmt.Sprintf(`You are helping prepare a bid response for: %s
Scope: %s

The bid requirements will be provided (typically extracted from an Excel spreadsheet, PDF, or other document).
Each requirement typically has an ID, descriptive text, classification (mandatory/scored/optional), and
sometimes references to technical standards.

Follow this workflow:

PHASE 1 - INITIAL ASSESSMENT
1. First, use assess_bid_requirements with all extracted requirements as a JSON array to get a quick coverage overview
2. Review the coverage_summary to understand the overall posture

PHASE 2 - DETAILED MAPPING (for each requirement)
3. Use map_bid_requirement for each requirement that needs detailed analysis, especially:
   - All MANDATORY requirements (must be fully addressed)
   - SCORED requirements where coverage may earn points
   - Any requirement where initial assessment showed gaps
4. For matched controls, use generate_evidence_summary to gather available evidence

PHASE 3 - DISCOVER NATURAL GROUPINGS
As you map requirements, identify natural thematic clusters — groups of requirements that share
the same set of controls or address a common capability area. These groupings emerge from the bid
requirements themselves, NOT from our internal control catalog structure. Examples of natural groups
might be: "Credential Issuance Protocols", "Trust Infrastructure", "Deployment & Operations",
"Presentation & Verification", "Key Management & Cryptography", etc. The exact groups depend on
what the bid asks for.

For each group, note:
- Which requirement IDs belong to this group
- Which controls are shared across the group
- The overall coverage level for the group
- Any common evidence that supports the whole group

Also identify requirements that don't fit neatly into any group — these are typically requirements
that need special attention (e.g. language support, specific deployment constraints, or capabilities
that aren't covered by existing controls).

PHASE 4 - GENERATE BID RESPONSE DOCUMENT
Produce a Markdown document structured as follows:

# Bid Response: %s

## Executive Summary
- Overall coverage statistics
- Key strengths
- Areas requiring attention

## [Group Title] (one section per discovered group)
Brief summary of how our platform addresses this area, with links to the shared controls.

### Controls
For each relevant control, include:
- **[Control ID](url)** — Title (Status: verified/validated/in_progress)

### Requirements
For each requirement in this group:
| Req ID | Classification | Answer | Justification |
|--------|---------------|--------|---------------|
| X.Y | MANDATORY | Yes | Brief explanation referencing controls |

### Evidence
Summarize the evidence available for this group's controls.

## Attention Required
This section lists requirements that need special handling by the bid team. Each item should
have its own subsection with enough context for someone to write a manual response:

### [Requirement ID]: [Short title]
**Classification**: MANDATORY/SCORED
**Requirement**: [Full text]
**Status**: Why this needs attention (e.g. "no existing control", "capability exists but
not in required language", "requires commitment to specific deployment timeline")
**Suggested response**: Draft text if possible, or guidance on what to write
**Action needed**: What the bid team needs to decide or verify

Include here ANY requirement where:
- The answer should be YES but with caveats that need human review
- A commitment to future work is being made
- The requirement is not fully covered by existing controls
- There are open findings that could affect the claim
- The requirement references standards we don't currently track

## Compliance Matrix
Full matrix of all requirements with columns:
| Req ID | Requirement (brief) | Classification | Answer | Controls | Coverage Level |

IMPORTANT GUIDELINES:
- Be honest about gaps — undisclosed non-compliance is worse than acknowledged gaps with remediation plans
- Distinguish between platform capabilities (what we control) and operator responsibilities
- For SCORED requirements, maximize points by providing detailed evidence of maturity
- Use control URLs from tool outputs — link to https://compliance.siros.org/controls/... pages
- Reference specific control IDs, finding IDs, and evidence items throughout
- Flag any requirement that references standards we don't yet track
- When a requirement CAN be met with minor effort (e.g. adding a language, small config change),
  it's appropriate to answer YES but flag it in the Attention Required section so the team
  knows a commitment is being made`, bidName, scope, bidName))),
				},
			), nil
		},
	)
}

// --- Helpers ---

func jsonResource(uri string, v any) ([]mcp.ResourceContents, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling resource: %w", err)
	}
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      uri,
			MIMEType: "application/json",
			Text:     string(b),
		},
	}, nil
}

func toolResultJSON(v any) (*mcp.CallToolResult, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling result: %w", err)
	}
	return mcp.NewToolResultText(string(b)), nil
}

func extractTemplateParam(uri, prefix string) string {
	return strings.TrimPrefix(uri, prefix)
}

func availableFrameworks(m mapping.Mappings) string {
	var ids []string
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return strings.Join(ids, ", ")
}

// controlGroupDir returns the catalog subdirectory ("technical", "organizational")
// for a given control ID by searching through the catalog groups.
func controlGroupDir(cat *catalog.Catalog, controlID string) string {
	for _, g := range cat.Groups {
		for _, c := range g.Controls {
			if c.ID == controlID {
				if g.SourceDir != "" {
					return g.SourceDir
				}
				return "technical"
			}
		}
	}
	return "technical"
}

// controlURL returns the public URL for a control on the compliance site.
func controlURL(baseURL string, cat *catalog.Catalog, controlID string) string {
	if baseURL == "" {
		return ""
	}
	groupDir := controlGroupDir(cat, controlID)
	slug := strings.ToLower(strings.ReplaceAll(controlID, "-", "_"))
	return strings.TrimRight(baseURL, "/") + "/controls/" + groupDir + "/" + slug
}
