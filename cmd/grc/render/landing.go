package render

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sirosfoundation/go-grc/pkg/audit"
	"github.com/sirosfoundation/go-grc/pkg/catalog"
	"github.com/sirosfoundation/go-grc/pkg/config"
)

var csfFunctions = []struct {
	ID    string
	Name  string
	Desc  string
	Color string
}{
	{"govern", "Govern (GV)", "Establish and monitor the organization\u2019s cybersecurity risk management strategy, expectations, and policy. Govern provides context for all other functions.", "#6366f1"},
	{"identify", "Identify (ID)", "Understand the organization\u2019s assets, suppliers, and related cybersecurity risks. Prioritize efforts consistent with risk management strategy and business needs.", "#0ea5e9"},
	{"protect", "Protect (PR)", "Implement safeguards to ensure delivery of critical services and reduce the likelihood and impact of cybersecurity events.", "#22c55e"},
	{"detect", "Detect (DE)", "Develop and implement activities to identify the occurrence of a cybersecurity event in a timely manner.", "#eab308"},
	{"respond", "Respond (RS)", "Take action regarding a detected cybersecurity incident to contain its impact.", "#f97316"},
	{"recover", "Recover (RC)", "Maintain plans for resilience and restore capabilities or services impaired by a cybersecurity incident.", "#ef4444"},
}

func generateCSFPage(cfg *config.Config, cat *catalog.Catalog, isPublic bool) error {
	byFunc := map[string][]catalog.Control{}
	ctrlKind := map[string]string{}
	for _, group := range cat.Groups {
		kind := groupKind(group)
		for _, ctrl := range group.Controls {
			if ctrl.CSFFunction != "" {
				if !isPublic && catalog.EffectiveStatus(&ctrl) == "to_do" {
					continue
				}
				byFunc[ctrl.CSFFunction] = append(byFunc[ctrl.CSFFunction], ctrl)
				ctrlKind[ctrl.ID] = kind
			}
		}
	}

	var b strings.Builder
	b.WriteString(`---
sidebar_label: CSF Functions
sidebar_position: 4
title: NIST Cybersecurity Framework Functions
---

# NIST Cybersecurity Framework Functions

Controls are mapped to the six functions of the
[NIST Cybersecurity Framework (CSF) 2.0](https://www.nist.gov/cyberframework).
The functions organize cybersecurity outcomes at the highest level.

`)

	b.WriteString("```mermaid\nflowchart LR\n")
	for _, fn := range csfFunctions {
		upper := strings.ToUpper(fn.ID)
		b.WriteString(fmt.Sprintf("  %s[\"%s\"]\n", upper, fn.Name))
		b.WriteString(fmt.Sprintf("  style %s fill:%s,color:#fff,stroke:none\n", upper, fn.Color))
	}
	b.WriteString("  GOVERN --> IDENTIFY --> PROTECT --> DETECT --> RESPOND --> RECOVER\n")
	b.WriteString("```\n\n")

	for _, fn := range csfFunctions {
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", fn.Name, fn.Desc)
		ctrls := byFunc[fn.ID]
		if len(ctrls) == 0 {
			b.WriteString("_No controls mapped to this function._\n\n")
			continue
		}
		if isPublic {
			b.WriteString("| Control | Title | Owner |\n|---------|-------|-------|\n")
		} else {
			b.WriteString("| Control | Title | Status | Owner |\n|---------|-------|--------|-------|\n")
		}
		for _, ctrl := range ctrls {
			slug := idSlug(ctrl.ID)
			kind := ctrlKind[ctrl.ID]
			if kind == "" {
				kind = "technical"
			}
			if isPublic {
				fmt.Fprintf(&b, "| [%s](/controls/%s/%s) | %s | %s |\n",
					ctrl.ID, kind, slug, ctrl.Title, ownerBadge(ctrl.Owner))
			} else {
				fmt.Fprintf(&b, "| [%s](/controls/%s/%s) | %s | %s | %s |\n",
					ctrl.ID, kind, slug, ctrl.Title,
					statusBadge(catalog.EffectiveStatus(&ctrl)), ownerBadge(ctrl.Owner))
			}
		}
		b.WriteString("\n")
	}

	return writePage(filepath.Join(cfg.SiteDir, "csf.md"), b.String())
}

func renderComplianceOverview(cfg *config.Config, cat *catalog.Catalog) string {
	csfCounts := map[string]int{}
	for _, group := range cat.Groups {
		for _, ctrl := range group.Controls {
			if ctrl.CSFFunction != "" {
				csfCounts[ctrl.CSFFunction]++
			}
		}
	}

	total := len(cat.Controls)
	techCount, orgCount := 0, 0
	for _, group := range cat.Groups {
		kind := groupKind(group)
		if kind == "technical" {
			techCount += len(group.Controls)
		} else {
			orgCount += len(group.Controls)
		}
	}

	var b strings.Builder
	b.WriteString("\n## How It Fits Together\n\n")
	b.WriteString("Compliance frameworks define **requirements** that are mapped to\n")
	b.WriteString("platform **controls**.  Each control is categorised under a\n")
	b.WriteString("[NIST CSF 2.0](/csf) function so that coverage can be reviewed\n")
	b.WriteString("at every level of abstraction.\n\n")

	const (
		svgW    = 1248
		colFW   = 132
		colCtrl = 504
		colCSF  = 876
		boxW    = 240
		boxH    = 32
		gap     = 6
		headerH = 28
		radius  = 6
	)

	fwCount := len(cfg.Frameworks)
	csfCount := len(csfFunctions)
	ctrlRows := 3

	maxRows := fwCount
	if csfCount > maxRows {
		maxRows = csfCount
	}
	if ctrlRows > maxRows {
		maxRows = ctrlRows
	}
	svgH := 60 + headerH + maxRows*(boxH+gap) + 20

	fmt.Fprintf(&b, "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 %d %d\" style=\"width:100%%;max-width:%dpx;font-family:system-ui,sans-serif\">\n", svgW, svgH, svgW)
	b.WriteString("<defs>\n")
	b.WriteString("  <marker id=\"ah\" markerWidth=\"10\" markerHeight=\"7\" refX=\"10\" refY=\"3.5\" orient=\"auto\"><polygon points=\"0 0, 10 3.5, 0 7\" fill=\"#94a3b8\"/></marker>\n")
	b.WriteString("</defs>\n")

	panelTop := 10
	panelH := svgH - 20
	fmt.Fprintf(&b, "<rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"10\" fill=\"#f0f4ff\" stroke=\"#6366f1\" stroke-width=\"1.5\"/>\n", colFW-10, panelTop, boxW+20, panelH)
	fmt.Fprintf(&b, "<rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"10\" fill=\"#f0fdf4\" stroke=\"#22c55e\" stroke-width=\"1.5\"/>\n", colCtrl-10, panelTop, boxW+20, panelH)
	fmt.Fprintf(&b, "<rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"10\" fill=\"#fffbeb\" stroke=\"#eab308\" stroke-width=\"1.5\"/>\n", colCSF-10, panelTop, boxW+20, panelH)

	headerY := panelTop + 22
	fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" text-anchor=\"middle\" font-weight=\"700\" font-size=\"14\" fill=\"#6366f1\">Frameworks</text>\n", colFW+boxW/2, headerY)
	fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" text-anchor=\"middle\" font-weight=\"700\" font-size=\"14\" fill=\"#22c55e\">Controls</text>\n", colCtrl+boxW/2, headerY)
	fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" text-anchor=\"middle\" font-weight=\"700\" font-size=\"14\" fill=\"#eab308\">CSF 2.0 Functions</text>\n", colCSF+boxW/2, headerY)

	startY := panelTop + headerH + 14

	fwMidYs := make([]int, fwCount)
	for i, fw := range cfg.Frameworks {
		y := startY + i*(boxH+gap)
		fwMidYs[i] = y + boxH/2
		fmt.Fprintf(&b, "<rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"%d\" fill=\"#fff\" stroke=\"#c7d2fe\" stroke-width=\"1\"/>\n", colFW, y, boxW, boxH, radius)
		fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" text-anchor=\"middle\" font-size=\"12\" fill=\"#312e81\">%s</text>\n", colFW+boxW/2, y+boxH/2+4, fw.Name)
	}

	ctrlLabels := []struct {
		label  string
		fill   string
		stroke string
	}{
		{fmt.Sprintf("%d Technical", techCount), "#dcfce7", "#22c55e"},
		{fmt.Sprintf("%d Organizational", orgCount), "#dcfce7", "#22c55e"},
		{fmt.Sprintf("%d Controls", total), "#bbf7d0", "#16a34a"},
	}
	ctrlMidY := 0
	for i, cl := range ctrlLabels {
		y := startY + i*(boxH+gap)
		ry := radius
		if i == 2 {
			ry = boxH / 2
		}
		fmt.Fprintf(&b, "<rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"%d\" fill=\"%s\" stroke=\"%s\" stroke-width=\"1\"/>\n", colCtrl, y, boxW, boxH, ry, cl.fill, cl.stroke)
		fw := "600"
		if i == 2 {
			fw = "700"
		}
		fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" text-anchor=\"middle\" font-size=\"13\" font-weight=\"%s\" fill=\"#14532d\">%s</text>\n", colCtrl+boxW/2, y+boxH/2+4, fw, cl.label)
		if i == 2 {
			ctrlMidY = y + boxH/2
		}
	}

	csfMidYs := make([]int, csfCount)
	for i, fn := range csfFunctions {
		y := startY + i*(boxH+gap)
		csfMidYs[i] = y + boxH/2
		count := csfCounts[fn.ID]
		fmt.Fprintf(&b, "<rect x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"%d\" fill=\"%s\"/>\n", colCSF, y, boxW, boxH, radius, fn.Color)
		fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" text-anchor=\"middle\" font-size=\"12\" font-weight=\"600\" fill=\"#fff\">%s \u00b7 %d</text>\n", colCSF+boxW/2, y+boxH/2+4, fn.Name, count)
	}

	arrowXStart := colFW + boxW + 4
	arrowXEnd := colCtrl - 4
	for _, my := range fwMidYs {
		fmt.Fprintf(&b, "<line x1=\"%d\" y1=\"%d\" x2=\"%d\" y2=\"%d\" stroke=\"#94a3b8\" stroke-width=\"1.5\" marker-end=\"url(#ah)\"/>\n", arrowXStart, my, arrowXEnd, ctrlMidY)
	}

	labelX := (arrowXStart + arrowXEnd) / 2
	labelY := startY - 2
	fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" text-anchor=\"middle\" font-size=\"10\" fill=\"#64748b\" font-style=\"italic\">requirements</text>\n", labelX, labelY)

	arrowXStart2 := colCtrl + boxW + 4
	arrowXEnd2 := colCSF - 4
	for _, my := range csfMidYs {
		fmt.Fprintf(&b, "<line x1=\"%d\" y1=\"%d\" x2=\"%d\" y2=\"%d\" stroke=\"#94a3b8\" stroke-width=\"1.5\" marker-end=\"url(#ah)\"/>\n", arrowXStart2, ctrlMidY, arrowXEnd2, my)
	}

	labelX2 := (arrowXStart2 + arrowXEnd2) / 2
	fmt.Fprintf(&b, "<text x=\"%d\" y=\"%d\" text-anchor=\"middle\" font-size=\"10\" fill=\"#64748b\" font-style=\"italic\">categorised</text>\n", labelX2, labelY)

	b.WriteString("</svg>\n\n")

	b.WriteString("## Platform vs Operator\n\n")
	b.WriteString("Each control is labelled **platform** or **operator**:\n\n")
	b.WriteString("- **Platform** controls apply to the open-source SIROS\u00a0ID codebase itself \u2014\n")
	b.WriteString("  they are satisfied by the software and verified through code, tests, and audits.\n")
	b.WriteString("- **Operator** controls apply to the organisation running the platform \u2014\n")
	b.WriteString("  policies, processes, and infrastructure that each deployment must provide independently.\n\n")
	b.WriteString("This separation reflects the fact that SIROS\u00a0ID is designed to be operated\n")
	b.WriteString("not only by the SIROS Foundation but by any organisation independently.\n\n")

	return b.String()
}

func generateLanding(cfg *config.Config, cat *catalog.Catalog, activeFindings []*audit.Finding, isPublic bool) error {
	total := len(cat.Controls)
	var fwNames []string
	for _, fw := range cfg.Frameworks {
		fwNames = append(fwNames, fw.Name)
	}
	fwList := strings.Join(fwNames, ", ")

	var b strings.Builder
	fmt.Fprintf(&b, "---\nsidebar_position: 1\nslug: /\ntitle: %s\n---\n\n# %s\n\n", cfg.Project.Name, cfg.Project.Name)

	if isPublic {
		b.WriteString("Security controls and compliance framework mappings.\n\n")
		fmt.Fprintf(&b, `## Quick Links

- **[Controls](controls)** %s %d security controls
- **[Frameworks](frameworks)** %s Mappings against %s
- **[CSF Functions](csf)** %s NIST Cybersecurity Framework function overview
`, "\u2014", total, "\u2014", fwList, "\u2014")

		b.WriteString(renderComplianceOverview(cfg, cat))
	} else {
		assessed, verified := 0, 0
		for _, ctrl := range cat.Controls {
			eff := catalog.EffectiveStatus(ctrl)
			if eff != "to_do" {
				assessed++
			}
			if eff == "verified" || eff == "validated" {
				verified++
			}
		}
		b.WriteString("Security controls, framework coverage, and compliance status.\n\n")
		fmt.Fprintf(&b, `<div class="dashboard-grid">
<a href="controls" class="dashboard-card"><div class="number">%d</div><div class="label">Assessed Controls</div></a>
<a href="controls" class="dashboard-card"><div class="number">%d</div><div class="label">Verified</div></a>
<a href="controls" class="dashboard-card"><div class="number">%d</div><div class="label">In Progress</div></a>
<a href="findings" class="dashboard-card"><div class="number">%d</div><div class="label">Open Findings</div></a>
</div>`+"\n\n", assessed, verified, assessed-verified, len(activeFindings))
		fmt.Fprintf(&b, `## Quick Links

- **[Controls](controls)** %s %d of %d controls assessed
- **[Frameworks](frameworks)** %s Coverage against %s
- **[CSF Functions](csf)** %s NIST Cybersecurity Framework function overview
- **[Findings](findings)** %s %d open audit findings
`, "\u2014", assessed, total, "\u2014", fwList, "\u2014", "\u2014", len(activeFindings))

		b.WriteString(renderComplianceOverview(cfg, cat))
	}
	return writePage(filepath.Join(cfg.SiteDir, "index.md"), b.String())
}
