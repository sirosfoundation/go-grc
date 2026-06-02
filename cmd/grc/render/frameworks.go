package render

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sirosfoundation/go-grc/pkg/audit"
	"github.com/sirosfoundation/go-grc/pkg/catalog"
	"github.com/sirosfoundation/go-grc/pkg/config"
	"github.com/sirosfoundation/go-grc/pkg/mapping"
)

func generateFrameworks(cfg *config.Config, cat *catalog.Catalog, maps mapping.Mappings, audits *audit.AuditSet, activeFindings []*audit.Finding, fwCats map[string]*catalog.FrameworkCatalog, isPublic bool) error {
	fwDir := filepath.Join(cfg.SiteDir, "frameworks")
	if err := writePage(filepath.Join(fwDir, "index.md"), renderFrameworkIndex(cfg.Frameworks, maps)); err != nil {
		return err
	}
	if err := writePage(filepath.Join(fwDir, "_category_.json"), categoryJSON("Frameworks", 2)); err != nil {
		return err
	}

	for _, fw := range cfg.Frameworks {
		fm := maps[fw.ID]
		if fm == nil {
			continue
		}
		if err := generateFramework(cfg, fw, fm, cat, audits, activeFindings, fwCats[fw.ID], isPublic); err != nil {
			return err
		}
	}
	return nil
}

func renderFrameworkIndex(frameworks []config.FrameworkConfig, maps mapping.Mappings) string {
	var b strings.Builder
	b.WriteString(`---
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
`)
	for _, fw := range frameworks {
		fm := maps[fw.ID]
		count := 0
		if fm != nil {
			count = len(fm.Entries)
		}
		fmt.Fprintf(&b, "| [%s](%s/) | %d | See details |\n", fw.Name, fw.Slug, count)
	}
	b.WriteString(`
## OSCAL Interoperability

The component definition is available as an OSCAL JSON artifact
at ` + "`" + `oscal/component-definition.json` + "`" + ` in the compliance repository.
Organizations can import this into their own GRC tools (trestle, CISO
Assistant, RegScale, etc.) to bootstrap their own assessments.
`)
	return b.String()
}

func generateFramework(cfg *config.Config, fw config.FrameworkConfig, fm *mapping.FrameworkMapping, cat *catalog.Catalog, audits *audit.AuditSet, activeFindings []*audit.Finding, fwCat *catalog.FrameworkCatalog, isPublic bool) error {
	dir := filepath.Join(cfg.SiteDir, "frameworks", fw.Slug)
	catLabel := fw.Name
	pos := fw.SidebarPosition + 1
	if err := writePage(filepath.Join(dir, "_category_.json"),
		fmt.Sprintf(`{"label":%q,"position":%d,"link":{"type":"doc","id":"frameworks/%s/index"}}`, catLabel, pos, fw.Slug)+"\n"); err != nil {
		return err
	}
	if err := writePage(filepath.Join(dir, "index.md"), renderFrameworkSummary(fw, fm, fwCat, isPublic)); err != nil {
		return err
	}

	for _, e := range fm.Entries {
		slug := entrySlug(e.Key)
		var catEntry *catalog.FrameworkRequirement
		if fwCat != nil {
			catEntry = fwCat.ByID[e.Key]
		}
		notes := e.Notes
		rc := fwReqConfig(fw)
		description := ""
		if catEntry != nil {
			description = catEntry.Description
		}
		if fw.ID == "eudi" && description != "" {
			description = resolveENISARefs(description)
		}
		page := renderRequirementPage(e.Key, catEntry, description, requirementAssessment{
			statusField: fw.StatusField,
			statusValue: e.Status,
			owner:       e.Owner,
			notes:       notes,
			controls:    e.Controls,
		}, rc, fw.ID, cat, activeFindings, isPublic)
		if err := writePage(filepath.Join(dir, slug+".md"), page); err != nil {
			return err
		}
	}
	fmt.Printf("  %d %s pages\n", len(fm.Entries), fw.Name)
	return nil
}

func renderFrameworkSummary(fw config.FrameworkConfig, fm *mapping.FrameworkMapping, fwCat *catalog.FrameworkCatalog, isPublic bool) string {
	counts := map[string]int{}
	for _, e := range fm.Entries {
		counts[e.Status]++
	}

	var b strings.Builder
	sourceLink := fw.Name
	if fw.SourceURL != "" {
		srcURL := fw.SourceURL
		if strings.HasPrefix(srcURL, "/") && !strings.HasSuffix(srcURL, "/") {
			srcURL = "pathname://" + srcURL
		}
		sourceLink = fmt.Sprintf("[%s](%s)", fw.Name, srcURL)
	}
	fmt.Fprintf(&b, "---\nsidebar_label: %s\ntitle: %s\n---\n\n# %s\n\n", fw.Name, fw.Name, sourceLink)

	if isPublic {
		fmt.Fprintf(&b, "%d requirements mapped to controls.\n\n", len(fm.Entries))
		b.WriteString("## Requirements\n\n| Requirement | Title | Controls | Owner |\n|-------------|-------|----------|-------|\n")
		for _, e := range fm.Entries {
			title := ""
			if fwCat != nil {
				if ce := fwCat.ByID[e.Key]; ce != nil {
					title = ce.Title
				}
			}
			link := fwReqLink(e.Key, fw.ID)
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", link, title, controlLinks(e.Controls), ownerBadge(e.Owner))
		}
	} else {
		assessed := 0
		for _, e := range fm.Entries {
			if !isUnassessedStatus(e.Status, fw.DeriveMode) {
				assessed++
			}
		}
		b.WriteString(`<div class="dashboard-grid">` + "\n")
		fmt.Fprintf(&b, `<div class="dashboard-card"><div class="number">%d</div><div class="label">Assessed</div></div>`+"\n", assessed)
		for _, sv := range orderedStatuses(fw.DeriveMode) {
			if sv == "not_assessed" {
				continue
			}
			label := formatStatusLabel(sv)
			fmt.Fprintf(&b, `<div class="dashboard-card"><div class="number">%d</div><div class="label">%s</div></div>`+"\n", counts[sv], label)
		}
		b.WriteString("</div>\n\n")
		fmt.Fprintf(&b, "%d of %d requirements assessed. "+
			"Requirements not yet referenced by any audit are omitted.\n\n", assessed, len(fm.Entries))
		if fw.DeriveMode == "result" {
			statusMap := resultStatusMap()
			b.WriteString("## Requirements\n\n| Ref | Status | Controls | Owner |\n|-----|--------|----------|-------|\n")
			for _, e := range fm.Entries {
				if isUnassessedStatus(e.Status, fw.DeriveMode) {
					continue
				}
				icon := statusMap[e.Status]
				if icon == "" {
					icon = "?"
				}
				title := ""
				if fwCat != nil {
					if ce := fwCat.ByID[e.Key]; ce != nil {
						title = ce.Title
					}
				}
				link := fwReqLink(e.Key, fw.ID)
				label := link
				if title != "" {
					label = link + " " + title
				}
				fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", label, icon, controlLinks(e.Controls), e.Owner)
			}
		} else {
			b.WriteString("## Coverage\n\n| Requirement | Title | Coverage | Controls | Owner |\n|-------------|-------|----------|----------|-------|\n")
			for _, e := range fm.Entries {
				if isUnassessedStatus(e.Status, fw.DeriveMode) {
					continue
				}
				title := ""
				if fwCat != nil {
					if ce := fwCat.ByID[e.Key]; ce != nil {
						title = ce.Title
					}
				}
				link := fwReqLink(e.Key, fw.ID)
				fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", link, title, coverageSpan(e.Status), controlLinks(e.Controls), ownerBadge(e.Owner))
			}
		}
	}
	return b.String()
}

func orderedStatuses(mode string) []string {
	if mode == "result" {
		return []string{"compliant", "partially_compliant", "non_compliant", "not_applicable", "not_assessed"}
	}
	return []string{"full", "partial", "none", "not_assessed"}
}

func isUnassessedStatus(status, deriveMode string) bool {
	return status == "not_assessed"
}

func formatStatusLabel(s string) string {
	words := strings.Split(strings.ReplaceAll(s, "_", " "), " ")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func resultStatusMap() map[string]string {
	return map[string]string{
		"compliant":           "\u2705",
		"partially_compliant": "\u26a0\ufe0f",
		"non_compliant":       "\u274c",
		"not_applicable":      "\u2014",
		"not_assessed":        "\u2014",
	}
}

func fwReqConfig(fw config.FrameworkConfig) reqConfig {
	if fw.DeriveMode == "result" {
		return reqConfig{
			source:    fw.Source,
			sourceURL: fw.SourceURL,
			statusMap: map[string]string{
				"compliant":           "\u2705 Compliant",
				"partially_compliant": "\u26a0\ufe0f Partially Compliant",
				"non_compliant":       "\u274c Non-Compliant",
				"not_applicable":      "\u2014 Not Applicable",
				"not_assessed":        "\u2014 Not Assessed",
			},
		}
	}
	return reqConfig{
		source:    fw.Source,
		sourceURL: fw.SourceURL,
		statusMap: coverageStatusMap(),
	}
}

type requirementAssessment struct {
	statusField string
	statusValue string
	owner       string
	notes       string
	controls    []string
}

type reqConfig struct {
	source    string
	sourceURL string
	statusMap map[string]string
}

func coverageStatusMap() map[string]string {
	return map[string]string{
		"full":         `<span class="coverage--full">full</span>`,
		"partial":      `<span class="coverage--partial">partial</span>`,
		"none":         `<span class="coverage--none">none</span>`,
		"not_assessed": `<span class="coverage--not-assessed">\u2014</span>`,
	}
}

func renderRequirementPage(reqID string, catEntry *catalog.FrameworkRequirement, description string, assess requirementAssessment, rc reqConfig, fwID string, cat *catalog.Catalog, activeFindings []*audit.Finding, isPublic bool) string {
	title := reqID
	section := ""
	if catEntry != nil {
		title = catEntry.Title
		section = catEntry.Section
	}

	statusDisplay := assess.statusValue
	if s, ok := rc.statusMap[assess.statusValue]; ok {
		statusDisplay = s
	}

	var b strings.Builder
	fmt.Fprintf(&b, "---\nsidebar_label: \"%s\"\ntitle: \"%s \u2014 %s\"\n---\n\n", reqID, reqID, title)
	fmt.Fprintf(&b, "# %s \u2014 %s\n\n", reqID, title)

	if description != "" && strings.TrimRight(description, ".") != strings.TrimRight(title, ".") {
		fmt.Fprintf(&b, "> %s\n\n", description)
	}

	b.WriteString("| Property | Value |\n|----------|-------|\n")
	if section != "" {
		fmt.Fprintf(&b, "| **Section** | %s |\n", section)
	}
	if !isPublic {
		sfLabel := strings.ReplaceAll(assess.statusField, "_", " ")
		sfLabel = strings.ToUpper(sfLabel[:1]) + sfLabel[1:]
		fmt.Fprintf(&b, "| **%s** | %s |\n", sfLabel, statusDisplay)
	}
	if assess.owner != "" {
		fmt.Fprintf(&b, "| **Owner** | %s |\n", ownerBadge(assess.owner))
	}
	b.WriteString("\n")

	if !isPublic && assess.notes != "" {
		fmt.Fprintf(&b, "## Assessment Notes\n\n%s\n\n", strings.TrimSpace(assess.notes))
	}

	if len(assess.controls) > 0 {
		if isPublic {
			b.WriteString("## Mapped Controls\n\n| Control | Title |\n|---------|-------|\n")
			for _, cid := range assess.controls {
				ctrl, ok := cat.Controls[cid]
				link := controlLink(cid)
				ctrlTitle := ""
				if ok {
					ctrlTitle = ctrl.Title
				}
				fmt.Fprintf(&b, "| %s | %s |\n", link, ctrlTitle)
			}
		} else {
			b.WriteString("## Mapped Controls\n\n| Control | Title | Status |\n|---------|-------|--------|\n")
			for _, cid := range assess.controls {
				ctrl, ok := cat.Controls[cid]
				link := controlLink(cid)
				ctrlTitle := ""
				ctrlStatus := ""
				if ok {
					ctrlTitle = ctrl.Title
					ctrlStatus = statusBadge(catalog.EffectiveStatus(ctrl))
				}
				fmt.Fprintf(&b, "| %s | %s | %s |\n", link, ctrlTitle, ctrlStatus)
			}
		}
		b.WriteString("\n")
	}

	if !isPublic {
		var related []*audit.Finding
		for _, f := range activeFindings {
			if findingMatchesReq(f, reqID, fwID) {
				related = append(related, f)
			}
		}
		if len(related) > 0 {
			b.WriteString("## Related Findings\n\n| Finding | Severity | Status |\n|---------|----------|--------|\n")
			for _, f := range related {
				fmt.Fprintf(&b, "| %s \u2014 %s | %s | %s |\n", findingLink(f), f.Title, f.Severity, findingStatusBadge(f.Status))
			}
			b.WriteString("\n")
		}

	}

	if rc.sourceURL != "" {
		srcURL := rc.sourceURL
		if strings.HasPrefix(srcURL, "/") && !strings.HasSuffix(srcURL, "/") {
			srcURL = "pathname://" + srcURL
		}
		fmt.Fprintf(&b, "---\n\n*Source: [%s](%s)*\n", rc.source, srcURL)
	} else {
		fmt.Fprintf(&b, "---\n\n*Source: %s*\n", rc.source)
	}
	return b.String()
}

func componentLink(name string, cfg *config.Config) string {
	for _, comp := range cfg.Components {
		if comp.Name == name {
			var parts []string
			if comp.Repo != "" {
				parts = append(parts, fmt.Sprintf("[%s](https://github.com/%s)", name, comp.Repo))
			} else {
				parts = append(parts, name)
			}
			if comp.DocsURL != "" {
				parts = append(parts, fmt.Sprintf(" ([docs](%s))", comp.DocsURL))
			}
			return strings.Join(parts, "")
		}
	}
	return name
}

func resolveRepo(repoShort string, cfg *config.Config) string {
	for _, comp := range cfg.Components {
		if comp.Repo != "" {
			parts := strings.SplitN(comp.Repo, "/", 2)
			if len(parts) == 2 && parts[1] == repoShort {
				return comp.Repo
			}
		}
	}
	return ""
}

func findingMatchesReq(f *audit.Finding, reqID, fwID string) bool {
	return f.MatchesReq(fwID, reqID)
}
