package render

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sirosfoundation/go-grc/pkg/audit"
	"github.com/sirosfoundation/go-grc/pkg/catalog"
	"github.com/sirosfoundation/go-grc/pkg/config"
	"github.com/sirosfoundation/go-grc/pkg/mapping"
)

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "render",
		Short: "Generate Docusaurus site pages from catalog, mappings, and findings",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			return run(root)
		},
	}
	return cmd
}

// --- URL maps (populated during run, used for cross-linking) ---
var (
	projectOrg     string
	controlURL     map[string]string
	eudiReqURL     map[string]string
	isoCtrlURL     map[string]string
	gdprItemURL    map[string]string
	asvsSectionURL map[string]string
)

// effectiveStatus returns DerivedStatus if set, otherwise Status.
func effectiveStatus(ctrl *catalog.Control) string {
	if ctrl.DerivedStatus != "" {
		return ctrl.DerivedStatus
	}
	return ctrl.Status
}

// deriveControlStatuses populates DerivedStatus on controls based on findings.
func deriveControlStatuses(cat *catalog.Catalog, audits *audit.AuditSet) {
	for id, ctrl := range cat.Controls {
		findings := audits.FindingsByControl[id]
		if len(findings) == 0 {
			continue
		}
		allResolved, allEvidence, anyInProgress := true, true, false
		for _, fref := range findings {
			f := fref.Finding
			if f.Status != "resolved" {
				allResolved = false
			}
			if !f.HasEvidence() {
				allEvidence = false
			}
			if f.Status == "in_progress" {
				anyInProgress = true
			}
		}
		var derived string
		switch {
		case allResolved && allEvidence:
			derived = "validated"
		case allResolved:
			derived = "verified"
		case anyInProgress:
			derived = "planned"
		default:
			derived = "to_do"
		}
		if derived != ctrl.Status {
			ctrl.DerivedStatus = derived
			cat.Controls[id] = ctrl
		}
	}
}

func run(root string) error {
	cfg := config.New(root)

	cat, err := catalog.Load(cfg.CatalogDir, cfg.CatalogSubdirs...)
	if err != nil {
		return fmt.Errorf("loading catalog: %w", err)
	}
	audits, err := audit.Load(cfg.AuditsDir)
	if err != nil {
		return fmt.Errorf("loading audits: %w", err)
	}
	maps, err := mapping.Load(cfg.MappingsDir)
	if err != nil {
		return fmt.Errorf("loading mappings: %w", err)
	}

	// Derive effective control statuses from findings before rendering.
	deriveControlStatuses(cat, audits)

	// Extract org from project repo for GitHub links.
	if parts := strings.SplitN(cfg.Project.Repo, "/", 2); len(parts) >= 1 {
		projectOrg = parts[0]
	}

	// Load framework catalogs (normative requirement text)
	eudiCat, _ := catalog.LoadFrameworkCatalog(cfg.CatalogDir, "eudi-secreq")
	isoCat, _ := catalog.LoadFrameworkCatalog(cfg.CatalogDir, "iso27001-annexa")
	gdprCat, _ := catalog.LoadFrameworkCatalog(cfg.CatalogDir, "gdpr-checklist")
	asvsCat, _ := catalog.LoadFrameworkCatalog(cfg.CatalogDir, "owasp-asvs")

	// Active findings (have tracking issue, not resolved)
	var activeFindings []*audit.Finding
	for _, ref := range audits.FindingsByID {
		f := ref.Finding
		if f.TrackingIssue != nil && f.Status != "resolved" {
			activeFindings = append(activeFindings, f)
		}
	}
	sort.Slice(activeFindings, func(i, j int) bool {
		return sevRank(activeFindings[i].Severity) > sevRank(activeFindings[j].Severity)
	})

	// Build URL maps
	controlURL = make(map[string]string)
	eudiReqURL = make(map[string]string)
	isoCtrlURL = make(map[string]string)
	gdprItemURL = make(map[string]string)
	asvsSectionURL = make(map[string]string)

	for _, group := range cat.Groups {
		kind := groupKind(group.ID)
		for _, ctrl := range group.Controls {
			slug := idSlug(ctrl.ID)
			controlURL[ctrl.ID] = "/controls/" + kind + "/" + slug
		}
	}
	if maps.EUDI != nil {
		for _, req := range maps.EUDI.Requirements {
			eudiReqURL[req.ID] = "/frameworks/eudi/" + eudiSlug(req.ID)
		}
	}
	if maps.ISO != nil {
		for _, m := range maps.ISO.Mappings {
			isoCtrlURL[m.AnnexA] = "/frameworks/iso27001/" + isoSlug(m.AnnexA)
		}
	}
	if maps.GDPR != nil {
		for _, m := range maps.GDPR.Mappings {
			slug := gdprSlug(m.MatchName)
			gdprItemURL[slug] = "/frameworks/gdpr/" + slug
		}
	}
	if maps.ASVS != nil {
		for _, m := range maps.ASVS.Mappings {
			slug := asvsSlug(m.Section)
			asvsSectionURL[m.Section] = "/frameworks/owasp-asvs/" + slug
		}
	}

	// Build framework→control reverse index
	frameworkRefs := buildFrameworkRefs(maps)

	// Clean generated dirs
	for _, subdir := range []string{"controls", "frameworks", "findings"} {
		os.RemoveAll(filepath.Join(cfg.SiteDir, subdir))
	}

	// Controls
	if err := generateControls(cfg, cat, audits, activeFindings, frameworkRefs); err != nil {
		return err
	}
	// Frameworks
	if err := generateFrameworks(cfg, cat, maps, audits, activeFindings, eudiCat, isoCat, gdprCat, asvsCat); err != nil {
		return err
	}
	// Findings
	if err := generateFindings(cfg, audits, activeFindings); err != nil {
		return err
	}
	// Landing page
	if err := generateLanding(cfg, cat, activeFindings); err != nil {
		return err
	}

	fmt.Println("Site generated.")
	return nil
}

// ---------------------------------------------------------------------------
// Control pages
// ---------------------------------------------------------------------------

func generateControls(cfg *config.Config, cat *catalog.Catalog, audits *audit.AuditSet, activeFindings []*audit.Finding, frameworkRefs map[string]fwRefs) error {
	dir := filepath.Join(cfg.SiteDir, "controls")
	writePage(filepath.Join(dir, "index.md"), renderControlIndex(cat))

	for _, group := range cat.Groups {
		kind := groupKind(group.ID)
		catDir := filepath.Join(dir, kind)
		writePage(filepath.Join(catDir, "_category_.json"), categoryJSON(kindLabel(kind)+" Controls", kindPosition(kind)))

		for _, ctrl := range group.Controls {
			slug := idSlug(ctrl.ID)
			page := renderControlPage(ctrl, group.Title, kind, audits, activeFindings, frameworkRefs)
			writePage(filepath.Join(catDir, slug+".md"), page)
		}
	}
	return nil
}

func renderControlIndex(cat *catalog.Catalog) string {
	total := len(cat.Controls)
	verified, toDo, platform, operator := 0, 0, 0, 0
	for _, ctrl := range cat.Controls {
		if effectiveStatus(ctrl) == "verified" || effectiveStatus(ctrl) == "validated" {
			verified++
		} else {
			toDo++
		}
		if ctrl.Owner == "platform" {
			platform++
		} else if ctrl.Owner == "operator" {
			operator++
		}
	}
	var b strings.Builder
	b.WriteString("---\nsidebar_label: Overview\nsidebar_position: 1\ntitle: Controls Overview\n---\n\n# Controls Overview\n\n")
	fmt.Fprintf(&b, "%d controls: %d verified, %d to-do | %d %s, %d %s\n\n",
		total, verified, toDo, platform, ownerBadge("platform"), operator, ownerBadge("operator"))

	for _, kind := range []string{"technical", "organizational"} {
		label := "Technical Controls (Platform-Provided)"
		if kind == "organizational" {
			label = "Organizational Controls (Operator-Required)"
		}
		fmt.Fprintf(&b, "## %s\n\n| ID | Title | Status | Owner | CSF Function |\n|----|-------|--------|-------|-------------|\n", label)
		for _, group := range cat.Groups {
			if groupKind(group.ID) != kind {
				continue
			}
			for _, ctrl := range group.Controls {
				slug := idSlug(ctrl.ID)
				fmt.Fprintf(&b, "| [%s](%s/%s) | %s | %s | %s | %s |\n",
					ctrl.ID, kind, slug, ctrl.Title,
					statusBadge(effectiveStatus(&ctrl)), ownerBadge(ctrl.Owner), csfBadge(ctrl.CSFFunction))
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func renderControlPage(ctrl catalog.Control, groupTitle, kind string, audits *audit.AuditSet, activeFindings []*audit.Finding, fwRefs map[string]fwRefs) string {
	cid := ctrl.ID
	effective := effectiveStatus(&ctrl)

	var b strings.Builder
	fmt.Fprintf(&b, "---\nsidebar_label: \"%s\"\ntitle: \"%s — %s\"\n---\n\n", cid, cid, ctrl.Title)
	fmt.Fprintf(&b, "# %s — %s\n\n", cid, ctrl.Title)
	b.WriteString("| Property | Value |\n|----------|-------|\n")
	fmt.Fprintf(&b, "| **Status** | %s |\n", statusBadge(effective))
	fmt.Fprintf(&b, "| **Owner** | %s |\n", ownerBadge(ctrl.Owner))
	fmt.Fprintf(&b, "| **Category** | %s |\n", ctrl.Category)
	fmt.Fprintf(&b, "| **CSF Function** | %s |\n", csfBadge(ctrl.CSFFunction))
	fmt.Fprintf(&b, "| **Group** | %s |\n\n", groupTitle)

	if ctrl.Description != "" {
		fmt.Fprintf(&b, "## Description\n\n%s\n", ctrl.Description)
	}
	if ctrl.OperatorResponsibility != "" {
		fmt.Fprintf(&b, "\n:::info Operator Responsibility\n%s\n:::\n", ctrl.OperatorResponsibility)
	}
	if len(ctrl.Components) > 0 {
		b.WriteString("\n## Components\n\n")
		for _, c := range ctrl.Components {
			fmt.Fprintf(&b, "- %s\n", c)
		}
	}
	if len(ctrl.References) > 0 {
		b.WriteString("\n## Source References\n\n")
		for _, r := range ctrl.References {
			parts := strings.SplitN(r, "/", 2)
			if len(parts) >= 2 {
				fmt.Fprintf(&b, "- [`%s`](https://github.com/%s/%s)\n", r, projectOrg, parts[0])
			} else {
				fmt.Fprintf(&b, "- `%s`\n", r)
			}
		}
	}

	// Linked findings
	linked := audits.FindingsByControl[cid]
	if len(linked) > 0 {
		b.WriteString("\n## Audit Findings\n\n| Finding | Severity | Status |\n|---------|----------|--------|\n")
		for _, ref := range linked {
			f := ref.Finding
			fmt.Fprintf(&b, "| %s — %s | %s | %s |\n", findingLink(f), f.Title, f.Severity, findingStatusBadge(f.Status))
		}
	}

	// Framework cross-references
	refs := fwRefs[cid]
	if len(refs.EUDI) > 0 || len(refs.ISO) > 0 || len(refs.GDPR) > 0 || len(refs.ASVS) > 0 {
		b.WriteString("\n## Framework Requirements\n\n")
		if len(refs.EUDI) > 0 {
			links := make([]string, len(refs.EUDI))
			for i, r := range refs.EUDI {
				links[i] = eudiReqLink(r)
			}
			fmt.Fprintf(&b, "**EUDI SecReq v0.5:** %s\n\n", strings.Join(links, ", "))
		}
		if len(refs.ISO) > 0 {
			links := make([]string, len(refs.ISO))
			for i, r := range refs.ISO {
				links[i] = isoCtrlLink(r)
			}
			fmt.Fprintf(&b, "**ISO 27001 Annex A:** %s\n\n", strings.Join(links, ", "))
		}
		if len(refs.GDPR) > 0 {
			links := make([]string, len(refs.GDPR))
			for i, r := range refs.GDPR {
				links[i] = gdprItemLink(r)
			}
			fmt.Fprintf(&b, "**GDPR Checklist:** %s\n\n", strings.Join(links, ", "))
		}
		if len(refs.ASVS) > 0 {
			links := make([]string, len(refs.ASVS))
			for i, r := range refs.ASVS {
				links[i] = asvsSectionLink(r)
			}
			fmt.Fprintf(&b, "**OWASP ASVS L3:** %s\n\n", strings.Join(links, ", "))
		}
	}

	return b.String()
}

// ---------------------------------------------------------------------------
// Framework pages (summary + per-requirement)
// ---------------------------------------------------------------------------

func generateFrameworks(cfg *config.Config, cat *catalog.Catalog, maps *mapping.Mappings, audits *audit.AuditSet, activeFindings []*audit.Finding, eudiCat, isoCat, gdprCat, asvsCat *catalog.FrameworkCatalog) error {
	fwDir := filepath.Join(cfg.SiteDir, "frameworks")
	writePage(filepath.Join(fwDir, "index.md"), renderFrameworkIndex())
	writePage(filepath.Join(fwDir, "_category_.json"), categoryJSON("Frameworks", 2))

	if maps.EUDI != nil {
		if err := generateEUDI(cfg, maps.EUDI, cat, audits, activeFindings, eudiCat); err != nil {
			return err
		}
	}
	if maps.ISO != nil {
		if err := generateISO(cfg, maps.ISO, cat, audits, activeFindings, isoCat); err != nil {
			return err
		}
	}
	if maps.GDPR != nil {
		if err := generateGDPR(cfg, maps.GDPR, cat, audits, activeFindings, gdprCat); err != nil {
			return err
		}
	}
	if maps.ASVS != nil {
		if err := generateASVS(cfg, maps.ASVS, cat, audits, activeFindings, asvsCat); err != nil {
			return err
		}
	}
	return nil
}

func renderFrameworkIndex() string {
	return `---
sidebar_label: Overview
sidebar_position: 1
title: Framework Coverage
---

# Framework Coverage

Controls are mapped to the following compliance frameworks.
Each framework page shows per-requirement coverage status and which
controls satisfy each requirement.

| Framework | Requirements | Coverage Status |
|-----------|-------------|-----------------|
| [EUDI Wallet Security Requirements v0.5](eudi/) | 85 | See details |
| [ISO/IEC 27001:2022 Annex A](iso27001/) | 93 | See details |
| [GDPR Checklist for Data Controllers](gdpr/) | 19 | See details |
| [OWASP ASVS 4.0.3 Level 3](owasp-asvs/) | 68 | See details |

## OSCAL Interoperability

The component definition is available as an OSCAL JSON artifact
at ` + "`" + `oscal/component-definition.json` + "`" + ` in the compliance repository.
Organizations can import this into their own GRC tools (trestle, CISO
Assistant, RegScale, etc.) to bootstrap their own assessments.
`
}

// --- EUDI ---

func generateEUDI(cfg *config.Config, eudi *mapping.EUDIMapping, cat *catalog.Catalog, audits *audit.AuditSet, activeFindings []*audit.Finding, fwCat *catalog.FrameworkCatalog) error {
	dir := filepath.Join(cfg.SiteDir, "frameworks", "eudi")
	writePage(filepath.Join(dir, "_category_.json"),
		`{"label":"EUDI SecReq v0.5","position":2,"link":{"type":"doc","id":"frameworks/eudi/index"}}`+"\n")
	writePage(filepath.Join(dir, "index.md"), renderEUDISummary(eudi, fwCat))

	for _, req := range eudi.Requirements {
		slug := eudiSlug(req.ID)
		var catEntry *catalog.FrameworkRequirement
		if fwCat != nil {
			catEntry = fwCat.ByID[req.ID]
		}
		page := renderRequirementPage(req.ID, catEntry, requirementAssessment{
			statusField: "result", statusValue: req.Result, owner: req.Owner,
			notes: req.Observation, controls: req.Controls,
		}, eudiReqConfig(), cat, activeFindings)
		writePage(filepath.Join(dir, slug+".md"), page)
	}
	fmt.Printf("  %d EUDI requirement pages\n", len(eudi.Requirements))
	return nil
}

func renderEUDISummary(eudi *mapping.EUDIMapping, fwCat *catalog.FrameworkCatalog) string {
	counts := map[string]int{}
	for _, req := range eudi.Requirements {
		counts[req.Result]++
	}
	var b strings.Builder
	b.WriteString("---\nsidebar_label: EUDI SecReq v0.5\ntitle: EUDI Wallet Security Requirements v0.5\n---\n\n# EUDI Wallet Security Requirements v0.5\n\n")
	fmt.Fprintf(&b, `<div class="dashboard-grid">
<div class="dashboard-card"><div class="number">%d</div><div class="label">Total Requirements</div></div>
<div class="dashboard-card"><div class="number">%d</div><div class="label">Compliant</div></div>
<div class="dashboard-card"><div class="number">%d</div><div class="label">Partially Compliant</div></div>
<div class="dashboard-card"><div class="number">%d</div><div class="label">Non-Compliant</div></div>
<div class="dashboard-card"><div class="number">%d</div><div class="label">Not Applicable</div></div>
<div class="dashboard-card"><div class="number">%d</div><div class="label">Not Assessed</div></div>
</div>`+"\n\n",
		len(eudi.Requirements), counts["compliant"], counts["partially_compliant"],
		counts["non_compliant"], counts["not_applicable"], counts["not_assessed"])

	b.WriteString("## Requirements\n\n| Ref | Status | Controls | Owner |\n|-----|--------|----------|-------|\n")
	statusMap := map[string]string{"compliant": "✅", "partially_compliant": "⚠️", "non_compliant": "❌", "not_applicable": "—", "not_assessed": "—"}
	for _, req := range eudi.Requirements {
		icon := statusMap[req.Result]
		if icon == "" {
			icon = "?"
		}
		title := ""
		if fwCat != nil {
			if ce := fwCat.ByID[req.ID]; ce != nil {
				title = ce.Title
			}
		}
		link := eudiReqLink(req.ID)
		label := link
		if title != "" {
			label = link + " " + title
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", label, icon, controlLinks(req.Controls), req.Owner)
	}
	return b.String()
}

// --- ISO 27001 ---

func generateISO(cfg *config.Config, iso *mapping.ISOFile, cat *catalog.Catalog, audits *audit.AuditSet, activeFindings []*audit.Finding, fwCat *catalog.FrameworkCatalog) error {
	dir := filepath.Join(cfg.SiteDir, "frameworks", "iso27001")
	writePage(filepath.Join(dir, "_category_.json"),
		`{"label":"ISO 27001:2022","position":3,"link":{"type":"doc","id":"frameworks/iso27001/index"}}`+"\n")
	writePage(filepath.Join(dir, "index.md"), renderISOSummary(iso, fwCat))

	for _, m := range iso.Mappings {
		slug := isoSlug(m.AnnexA)
		var catEntry *catalog.FrameworkRequirement
		if fwCat != nil {
			catEntry = fwCat.ByID[m.AnnexA]
		}
		page := renderRequirementPage(m.AnnexA, catEntry, requirementAssessment{
			statusField: "coverage", statusValue: m.Coverage, owner: m.Owner,
			notes: m.Notes, controls: m.Controls,
		}, isoReqConfig(), cat, activeFindings)
		writePage(filepath.Join(dir, slug+".md"), page)
	}
	fmt.Printf("  %d ISO 27001 control pages\n", len(iso.Mappings))
	return nil
}

func renderISOSummary(iso *mapping.ISOFile, fwCat *catalog.FrameworkCatalog) string {
	covered := 0
	notAssessed := 0
	for _, m := range iso.Mappings {
		if len(m.Controls) > 0 {
			covered++
		}
		if m.Coverage == "not_assessed" {
			notAssessed++
		}
	}
	var b strings.Builder
	b.WriteString("---\nsidebar_label: ISO 27001:2022\ntitle: \"ISO/IEC 27001:2022 Annex A Coverage\"\n---\n\n# ISO/IEC 27001:2022 Annex A Coverage\n\n")
	fmt.Fprintf(&b, `<div class="dashboard-grid">
<div class="dashboard-card"><div class="number">%d</div><div class="label">Annex A Controls</div></div>
<div class="dashboard-card"><div class="number">%d</div><div class="label">Covered</div></div>
<div class="dashboard-card"><div class="number">%d</div><div class="label">Not Assessed</div></div>
</div>`+"\n\n", len(iso.Mappings), covered, notAssessed)

	b.WriteString("## Annex A Control Mapping\n\n| ISO Control | Coverage | SID Controls | Notes |\n|-------------|----------|-------------|-------|\n")
	for _, m := range iso.Mappings {
		title := ""
		if fwCat != nil {
			if ce := fwCat.ByID[m.AnnexA]; ce != nil {
				title = ce.Title
			}
		}
		link := isoCtrlLink(m.AnnexA)
		notes := truncate(strings.ReplaceAll(m.Notes, "\n", " "), 100)
		fmt.Fprintf(&b, "| %s %s | %s | %s | %s |\n", link, title, coverageSpan(m.Coverage), controlLinks(m.Controls), notes)
	}
	return b.String()
}

// --- GDPR ---

func generateGDPR(cfg *config.Config, gdpr *mapping.GDPRFile, cat *catalog.Catalog, audits *audit.AuditSet, activeFindings []*audit.Finding, fwCat *catalog.FrameworkCatalog) error {
	dir := filepath.Join(cfg.SiteDir, "frameworks", "gdpr")
	writePage(filepath.Join(dir, "_category_.json"),
		`{"label":"GDPR Checklist","position":4,"link":{"type":"doc","id":"frameworks/gdpr/index"}}`+"\n")
	writePage(filepath.Join(dir, "index.md"), renderGDPRSummary(gdpr))

	for _, m := range gdpr.Mappings {
		slug := gdprSlug(m.MatchName)
		var catEntry *catalog.FrameworkRequirement
		if fwCat != nil {
			catEntry = fwCat.ByID[m.MatchName]
		}
		page := renderRequirementPage(m.MatchName, catEntry, requirementAssessment{
			statusField: "coverage", statusValue: m.Coverage, owner: m.Owner,
			notes: m.Notes, controls: m.Controls,
		}, gdprReqConfig(), cat, activeFindings)
		writePage(filepath.Join(dir, slug+".md"), page)
	}
	fmt.Printf("  %d GDPR checklist item pages\n", len(gdpr.Mappings))
	return nil
}

func renderGDPRSummary(gdpr *mapping.GDPRFile) string {
	full, partial, none, notAssessed := 0, 0, 0, 0
	for _, m := range gdpr.Mappings {
		switch m.Coverage {
		case "full":
			full++
		case "partial":
			partial++
		case "none":
			none++
		case "not_assessed":
			notAssessed++
		}
	}
	var b strings.Builder
	b.WriteString("---\nsidebar_label: GDPR Checklist\ntitle: GDPR Checklist for Data Controllers\n---\n\n# GDPR Checklist for Data Controllers\n\n")
	fmt.Fprintf(&b, `<div class="dashboard-grid">
<div class="dashboard-card"><div class="number">%d</div><div class="label">Checklist Items</div></div>
<div class="dashboard-card"><div class="number">%d</div><div class="label">Full Coverage</div></div>
<div class="dashboard-card"><div class="number">%d</div><div class="label">Partial</div></div>
<div class="dashboard-card"><div class="number">%d</div><div class="label">No Coverage</div></div>
<div class="dashboard-card"><div class="number">%d</div><div class="label">Not Assessed</div></div>
</div>`+"\n\n", len(gdpr.Mappings), full, partial, none, notAssessed)

	b.WriteString(":::info\nSee [Findings](/findings) for GDPR-related audit findings tracked as GitHub issues.\n:::\n\n")
	b.WriteString("## Checklist Coverage\n\n| Checklist Item | Coverage | Controls | Owner | Notes |\n|----------------|----------|----------|-------|-------|\n")
	for _, m := range gdpr.Mappings {
		notes := truncate(strings.ReplaceAll(m.Notes, "\n", " "), 120)
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
			gdprItemLink(m.MatchName), coverageSpan(m.Coverage), controlLinks(m.Controls),
			ownerBadge(m.Owner), notes)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Per-requirement detail page (unified for all three frameworks)
// ---------------------------------------------------------------------------

type requirementAssessment struct {
	statusField string
	statusValue string
	owner       string
	notes       string
	controls    []string
}

type reqConfig struct {
	source    string
	statusMap map[string]string
}

func eudiReqConfig() reqConfig {
	return reqConfig{
		source: "ENISA – Security Requirements for European Digital Identity Wallets v0.5",
		statusMap: map[string]string{
			"compliant": "✅ Compliant", "partially_compliant": "⚠️ Partially Compliant",
			"non_compliant": "❌ Non-Compliant", "not_applicable": "— Not Applicable", "not_assessed": "— Not Assessed",
		},
	}
}

func isoReqConfig() reqConfig {
	return reqConfig{
		source:    "ISO/IEC 27001:2022 Annex A",
		statusMap: coverageStatusMap(),
	}
}

func gdprReqConfig() reqConfig {
	return reqConfig{
		source:    "GDPR Checklist for Data Controllers",
		statusMap: coverageStatusMap(),
	}
}

func coverageStatusMap() map[string]string {
	return map[string]string{
		"full":         `<span class="coverage--full">full</span>`,
		"partial":      `<span class="coverage--partial">partial</span>`,
		"none":         `<span class="coverage--none">none</span>`,
		"not_assessed": `<span class="coverage--not-assessed">—</span>`,
	}
}

func renderRequirementPage(reqID string, catEntry *catalog.FrameworkRequirement, assess requirementAssessment, rc reqConfig, cat *catalog.Catalog, activeFindings []*audit.Finding) string {
	title := reqID
	section := ""
	description := ""
	if catEntry != nil {
		title = catEntry.Title
		section = catEntry.Section
		description = catEntry.Description
	}

	statusDisplay := assess.statusValue
	if s, ok := rc.statusMap[assess.statusValue]; ok {
		statusDisplay = s
	}

	var b strings.Builder
	fmt.Fprintf(&b, "---\nsidebar_label: \"%s\"\ntitle: \"%s — %s\"\n---\n\n", reqID, reqID, title)
	fmt.Fprintf(&b, "# %s — %s\n\n", reqID, title)

	if description != "" {
		fmt.Fprintf(&b, "> %s\n\n", resolveENISARefs(description))
	}

	b.WriteString("| Property | Value |\n|----------|-------|\n")
	if section != "" {
		fmt.Fprintf(&b, "| **Section** | %s |\n", section)
	}
	sfLabel := strings.ReplaceAll(assess.statusField, "_", " ")
	sfLabel = strings.ToUpper(sfLabel[:1]) + sfLabel[1:]
	fmt.Fprintf(&b, "| **%s** | %s |\n", sfLabel, statusDisplay)
	if assess.owner != "" {
		fmt.Fprintf(&b, "| **Owner** | %s |\n", ownerBadge(assess.owner))
	}
	b.WriteString("\n")

	if assess.notes != "" {
		fmt.Fprintf(&b, "## Assessment Notes\n\n%s\n\n", strings.TrimSpace(assess.notes))
	}

	if len(assess.controls) > 0 {
		b.WriteString("## Mapped Controls\n\n| Control | Title | Status |\n|---------|-------|--------|\n")
		for _, cid := range assess.controls {
			ctrl, ok := cat.Controls[cid]
			link := controlLink(cid)
			ctrlTitle := ""
			ctrlStatus := ""
			if ok {
				ctrlTitle = ctrl.Title
				ctrlStatus = statusBadge(effectiveStatus(ctrl))
			}
			fmt.Fprintf(&b, "| %s | %s | %s |\n", link, ctrlTitle, ctrlStatus)
		}
		b.WriteString("\n")
	}

	// Related findings
	var related []*audit.Finding
	for _, f := range activeFindings {
		if findingMatchesReq(f, reqID, assess.statusField) {
			related = append(related, f)
		}
	}
	if len(related) > 0 {
		b.WriteString("## Related Findings\n\n| Finding | Severity | Status |\n|---------|----------|--------|\n")
		for _, f := range related {
			fmt.Fprintf(&b, "| %s — %s | %s | %s |\n", findingLink(f), f.Title, f.Severity, findingStatusBadge(f.Status))
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "---\n\n*Source: %s*\n", rc.source)
	return b.String()
}

func findingMatchesReq(f *audit.Finding, reqID, statusField string) bool {
	if statusField == "result" {
		// EUDI — match on eudi_reqs
		for _, r := range f.EUDIReqs {
			if r == reqID {
				return true
			}
		}
	} else {
		// ISO — match on annex_a; GDPR — match on audit ID prefix
		for _, a := range f.AnnexA {
			if a == reqID {
				return true
			}
		}
		for _, s := range f.ASVSSections {
			if s == reqID {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Findings page
// ---------------------------------------------------------------------------

func generateFindings(cfg *config.Config, audits *audit.AuditSet, activeFindings []*audit.Finding) error {
	dir := filepath.Join(cfg.SiteDir, "findings")
	writePage(filepath.Join(dir, "_category_.json"), categoryJSON("Findings", 3))
	writePage(filepath.Join(dir, "index.md"), renderFindingsIndex(activeFindings))
	return nil
}

func renderFindingsIndex(findings []*audit.Finding) string {
	var b strings.Builder
	b.WriteString("---\nsidebar_label: Findings\nsidebar_position: 1\ntitle: Findings Overview\n---\n\n# Findings Overview\n\n")
	b.WriteString("Findings are tracked as GitHub issues. Each finding links to its tracking issue where analysis, comments, and implementation progress are managed. Resolved findings are removed from this page.\n\n")
	fmt.Fprintf(&b, `<div class="dashboard-grid">
<div class="dashboard-card"><div class="number">%d</div><div class="label">Open Findings</div></div>
</div>`+"\n\n", len(findings))
	b.WriteString("## Open Findings\n\n| Finding | Severity | Owner | Controls |\n|---------|----------|-------|----------|\n")
	for _, f := range findings {
		icon := sevIcon(f.Severity)
		fmt.Fprintf(&b, "| %s — %s | %s %s | %s | %s |\n",
			findingLink(f), f.Title, icon, f.Severity, ownerBadge(f.Owner), controlLinks(f.Controls))
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Landing page
// ---------------------------------------------------------------------------

func generateLanding(cfg *config.Config, cat *catalog.Catalog, activeFindings []*audit.Finding) error {
	total := len(cat.Controls)
	verified := 0
	for _, ctrl := range cat.Controls {
		if effectiveStatus(ctrl) == "verified" || effectiveStatus(ctrl) == "validated" {
			verified++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "---\nsidebar_position: 1\nslug: /\ntitle: %s\n---\n\n# %s\n\n", cfg.Project.Name, cfg.Project.Name)
	b.WriteString("Security controls, framework coverage, and compliance status.\n\n")
	fmt.Fprintf(&b, `<div class="dashboard-grid">
<a href="controls" class="dashboard-card"><div class="number">%d</div><div class="label">Total Controls</div></a>
<a href="controls" class="dashboard-card"><div class="number">%d</div><div class="label">Verified</div></a>
<a href="controls" class="dashboard-card"><div class="number">%d</div><div class="label">To Do</div></a>
<a href="findings" class="dashboard-card"><div class="number">%d</div><div class="label">Open Findings</div></a>
</div>`+"\n\n", total, verified, total-verified, len(activeFindings))

	b.WriteString(`## Quick Links

- **[Controls](controls)** — Full catalog of `)
	fmt.Fprintf(&b, "%d", total)
	b.WriteString(` security controls
- **[Frameworks](frameworks)** — Coverage against EUDI, ISO 27001, GDPR
- **[Findings](findings)** — Summary of audit findings (tracked as GitHub issues)
- **[Deployment Checklist](checklist)** — Operator requirements for each deployment

## For Deployment Operators

If you are deploying this platform, start with the
[Deployment Checklist](checklist) for all organizational requirements, and download the
the OSCAL component definition
to bootstrap your own compliance assessment.

See [How It Works](workflows) for architecture and workflow details.
`)
	return writePage(filepath.Join(cfg.SiteDir, "index.md"), b.String())
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writePage(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}

func categoryJSON(label string, position int) string {
	data, _ := json.Marshal(map[string]interface{}{
		"label":    label,
		"position": position,
	})
	return string(data) + "\n"
}

func idSlug(id string) string {
	return strings.ToLower(strings.ReplaceAll(id, "-", "_"))
}

func eudiSlug(id string) string {
	s := strings.ToLower(id)
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, ".", "_")
	return s
}

func isoSlug(ref string) string {
	return strings.ToLower(strings.ReplaceAll(ref, ".", "_"))
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func gdprSlug(name string) string {
	s := nonAlnum.ReplaceAllString(strings.ToLower(name), "_")
	return strings.Trim(s, "_")
}

func groupKind(groupID string) string {
	switch groupID {
	case "governance", "operations", "people", "physical":
		return "organizational"
	default:
		return "technical"
	}
}

func kindLabel(kind string) string {
	if kind == "organizational" {
		return "Organizational"
	}
	return "Technical"
}

func kindPosition(kind string) int {
	if kind == "organizational" {
		return 3
	}
	return 2
}

// --- Badge helpers (HTML matching the Docusaurus CSS classes) ---

func statusBadge(s string) string {
	m := map[string]string{
		"verified":  `<span class="badge--verified">verified</span>`,
		"to_do":     `<span class="badge--to-do">to_do</span>`,
		"planned":   `<span class="badge--to-do">planned</span>`,
		"validated": `<span class="badge--verified">validated</span>`,
	}
	if v, ok := m[s]; ok {
		return v
	}
	return s
}

func ownerBadge(s string) string {
	m := map[string]string{
		"platform": `<span class="badge--platform">platform</span>`,
		"operator": `<span class="badge--operator">operator</span>`,
	}
	if v, ok := m[s]; ok {
		return v
	}
	return s
}

func csfBadge(s string) string {
	m := map[string]string{
		"identify": `<span class="badge--csf">identify</span>`,
		"protect":  `<span class="badge--csf">protect</span>`,
		"detect":   `<span class="badge--csf">detect</span>`,
		"respond":  `<span class="badge--csf">respond</span>`,
		"recover":  `<span class="badge--csf">recover</span>`,
		"govern":   `<span class="badge--csf">govern</span>`,
	}
	if v, ok := m[s]; ok {
		return v
	}
	return s
}

func coverageSpan(s string) string {
	m := map[string]string{
		"full":         `<span class="coverage--full">full</span>`,
		"partial":      `<span class="coverage--partial">partial</span>`,
		"none":         `<span class="coverage--none">none</span>`,
		"not_assessed": `<span class="coverage--not-assessed">—</span>`,
	}
	if v, ok := m[s]; ok {
		return v
	}
	return s
}

func findingStatusBadge(s string) string {
	m := map[string]string{
		"open":        `<span class="badge--to-do">open</span>`,
		"in_progress": `<span class="badge--to-do">in progress</span>`,
		"resolved":    `<span class="badge--verified">resolved</span>`,
	}
	if v, ok := m[s]; ok {
		return v
	}
	return s
}

func sevIcon(s string) string {
	m := map[string]string{"critical": "🔴", "high": "🟠", "medium": "🟡", "low": "🟢"}
	if v, ok := m[s]; ok {
		return v
	}
	return ""
}

func sevRank(s string) int {
	switch s {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	}
	return 0
}

// --- Link helpers ---

func controlLinks(ids []string) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = controlLink(id)
	}
	return strings.Join(parts, ", ")
}

func controlLink(id string) string {
	if url, ok := controlURL[id]; ok {
		return "[" + id + "](" + url + ")"
	}
	return "`" + id + "`"
}

func eudiReqLink(id string) string {
	if url, ok := eudiReqURL[id]; ok {
		return "[" + id + "](" + url + ")"
	}
	return "`" + id + "`"
}

func isoCtrlLink(ref string) string {
	if url, ok := isoCtrlURL[ref]; ok {
		return "[" + ref + "](" + url + ")"
	}
	return "`" + ref + "`"
}

func gdprItemLink(name string) string {
	slug := gdprSlug(name)
	if url, ok := gdprItemURL[slug]; ok {
		return "[" + name + "](" + url + ")"
	}
	return name
}

func findingLink(f *audit.Finding) string {
	if f.TrackingIssue != nil {
		return fmt.Sprintf("[%s](https://github.com/%s/issues/%d)", f.ID, f.TrackingIssue.Repo, f.TrackingIssue.Number)
	}
	return "`" + f.ID + "`"
}

// --- Framework reverse index ---

type fwRefs struct {
	EUDI []string
	ISO  []string
	GDPR []string
	ASVS []string
}

func buildFrameworkRefs(maps *mapping.Mappings) map[string]fwRefs {
	refs := map[string]fwRefs{}
	if maps.EUDI != nil {
		for _, req := range maps.EUDI.Requirements {
			for _, cid := range req.Controls {
				r := refs[cid]
				r.EUDI = append(r.EUDI, req.ID)
				refs[cid] = r
			}
		}
	}
	if maps.ISO != nil {
		for _, m := range maps.ISO.Mappings {
			for _, cid := range m.Controls {
				r := refs[cid]
				r.ISO = append(r.ISO, m.AnnexA)
				refs[cid] = r
			}
		}
	}
	if maps.GDPR != nil {
		for _, m := range maps.GDPR.Mappings {
			for _, cid := range m.Controls {
				r := refs[cid]
				r.GDPR = append(r.GDPR, m.MatchName)
				refs[cid] = r
			}
		}
	}
	if maps.ASVS != nil {
		for _, m := range maps.ASVS.Mappings {
			for _, cid := range m.Controls {
				r := refs[cid]
				r.ASVS = append(r.ASVS, m.Section)
				refs[cid] = r
			}
		}
	}
	return refs
}

// --- ENISA reference resolution ---

func resolveENISARefs(text string) string {
	// Named document patterns
	replacements := []struct{ pattern, replacement string }{
		{`OWASP\s+Application\s+Security\s+Verification\s+Standard\s*\[i\.10\]`,
			"[OWASP ASVS](https://owasp.org/www-project-application-security-verification-standard/)"},
		{`ECCG\s+Agreed\s+Cryptograph(?:y|ic)\s+Mechanisms\s*\[2\]`,
			"[ECCG Agreed Cryptographic Mechanisms](https://www.enisa.europa.eu/topics/certification/eccg)"},
		{`CIR\s*\(EU\)\s*2024/2981\s*\[i\.3\]`,
			"[CIR (EU) 2024/2981](https://eur-lex.europa.eu/eli/reg_impl/2024/2981/oj)"},
		{`CIR\s*\(EU\)\s*2015/1502\s*\[i\.2\]`,
			"[CIR (EU) 2015/1502](https://eur-lex.europa.eu/eli/reg_impl/2015/1502/oj)"},
		{`EN\s+319\s+401\s*\[1\]`,
			"[EN 319 401](https://www.etsi.org/deliver/etsi_en/319400_319499/319401/)"},
	}
	for _, r := range replacements {
		re := regexp.MustCompile(r.pattern)
		text = re.ReplaceAllString(text, r.replacement)
	}
	// [ARF …] → link
	arfRe := regexp.MustCompile(`\[ARF ([^\]]+)\]`)
	text = arfRe.ReplaceAllString(text, "[ARF $1](https://eudi.dev/2.8.0/architecture-and-reference-framework-main/)")
	return text
}

// --- OWASP ASVS ---

func generateASVS(cfg *config.Config, asvs *mapping.ASVSFile, cat *catalog.Catalog, audits *audit.AuditSet, activeFindings []*audit.Finding, fwCat *catalog.FrameworkCatalog) error {
	dir := filepath.Join(cfg.SiteDir, "frameworks", "owasp-asvs")
	writePage(filepath.Join(dir, "_category_.json"),
		`{"label":"OWASP ASVS L3","position":5,"link":{"type":"doc","id":"frameworks/owasp-asvs/index"}}`+"\n")
	writePage(filepath.Join(dir, "index.md"), renderASVSSummary(asvs, fwCat))

	for _, m := range asvs.Mappings {
		slug := asvsSlug(m.Section)
		var catEntry *catalog.FrameworkRequirement
		if fwCat != nil {
			catEntry = fwCat.ByID[m.Section]
		}
		page := renderRequirementPage(m.Section, catEntry, requirementAssessment{
			statusField: "coverage", statusValue: m.Coverage, owner: m.Owner,
			notes: m.Notes, controls: m.Controls,
		}, asvsReqConfig(), cat, activeFindings)
		writePage(filepath.Join(dir, slug+".md"), page)
	}
	fmt.Printf("  %d OWASP ASVS section pages\n", len(asvs.Mappings))
	return nil
}

func renderASVSSummary(asvs *mapping.ASVSFile, fwCat *catalog.FrameworkCatalog) string {
	full, partial, none, notAssessed := 0, 0, 0, 0
	for _, m := range asvs.Mappings {
		switch m.Coverage {
		case "full":
			full++
		case "partial":
			partial++
		case "none":
			none++
		case "not_assessed":
			notAssessed++
		}
	}
	var b strings.Builder
	b.WriteString("---\nsidebar_label: OWASP ASVS L3\ntitle: \"OWASP ASVS 4.0.3 \xe2\x80\x93 Level 3\"\n---\n\n# OWASP ASVS 4.0.3 \xe2\x80\x93 Level 3\n\n")

	fmt.Fprintf(&b, `<div class="dashboard-grid">
<div class="dashboard-card"><div class="number">%d</div><div class="label">Sections Assessed</div></div>
<div class="dashboard-card"><div class="number">%d</div><div class="label">Full Coverage</div></div>
<div class="dashboard-card"><div class="number">%d</div><div class="label">Partial Coverage</div></div>
<div class="dashboard-card"><div class="number">%d</div><div class="label">No Coverage</div></div>
<div class="dashboard-card"><div class="number">%d</div><div class="label">Not Assessed / N\u002FA</div></div>
</div>`+"\n\n",
		len(asvs.Mappings), full, partial, none, notAssessed)

	b.WriteString(":::info\nAssessment covers 278 individual L3 requirements grouped into 68 sections. ")
	b.WriteString("Sections marked \u201cnot assessed\u201d are not applicable to the passwordless FIDO-only wallet architecture.\n:::\n\n")
	b.WriteString("## Section Coverage\n\n| Section | Title | Coverage | Controls | Owner |\n|---------|-------|----------|----------|-------|\n")
	for _, m := range asvs.Mappings {
		title := ""
		if fwCat != nil {
			if ce := fwCat.ByID[m.Section]; ce != nil {
				title = ce.Title
			}
		}
		link := asvsSectionLink(m.Section)
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", link, title, coverageSpan(m.Coverage), controlLinks(m.Controls), ownerBadge(m.Owner))
	}
	return b.String()
}

func asvsReqConfig() reqConfig {
	return reqConfig{
		source:    "OWASP Application Security Verification Standard 4.0.3",
		statusMap: coverageStatusMap(),
	}
}

func asvsSlug(section string) string {
	return strings.ToLower(strings.ReplaceAll(section, ".", "_"))
}

func asvsSectionLink(section string) string {
	if url, ok := asvsSectionURL[section]; ok {
		return "[" + section + "](" + url + ")"
	}
	return "`" + section + "`"
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
