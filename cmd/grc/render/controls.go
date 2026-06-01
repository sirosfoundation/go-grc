package render

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sirosfoundation/go-grc/pkg/audit"
	"github.com/sirosfoundation/go-grc/pkg/catalog"
	"github.com/sirosfoundation/go-grc/pkg/config"
)

func generateControls(cfg *config.Config, cat *catalog.Catalog, audits *audit.AuditSet, activeFindings []*audit.Finding, frameworkRefs map[string]map[string][]string, isPublic bool) error {
	dir := filepath.Join(cfg.SiteDir, "controls")
	writePage(filepath.Join(dir, "index.md"), renderControlIndex(cat, isPublic))

	for _, group := range cat.Groups {
		kind := groupKind(group)
		catDir := filepath.Join(dir, kind)
		writePage(filepath.Join(catDir, "_category_.json"), categoryJSON(kindLabel(kind)+" Controls", kindPosition(kind)))

		for _, ctrl := range group.Controls {
			slug := idSlug(ctrl.ID)
			page := renderControlPage(ctrl, group.Title, kind, audits, activeFindings, frameworkRefs, cfg, isPublic)
			writePage(filepath.Join(catDir, slug+".md"), page)
		}
	}
	return nil
}

func renderControlIndex(cat *catalog.Catalog, isPublic bool) string {
	total := len(cat.Controls)
	var b strings.Builder
	b.WriteString("---\nsidebar_label: Overview\nsidebar_position: 1\ntitle: Controls Overview\n---\n\n# Controls Overview\n\n")

	if isPublic {
		fmt.Fprintf(&b, "%d security controls across the platform.\n\n", total)
		for _, kind := range []string{"technical", "organizational"} {
			fmt.Fprintf(&b, "## %s Controls\n\n", titleCase(kind))

			controlsByOwner := make(map[string][]catalog.Control)
			for _, group := range cat.Groups {
				if groupKind(group) != kind {
					continue
				}
				for _, ctrl := range group.Controls {
					owner := ctrl.Owner
					if owner == "" {
						owner = "platform"
					}
					controlsByOwner[owner] = append(controlsByOwner[owner], ctrl)
				}
			}

			for owner := range controlsByOwner {
				sort.Slice(controlsByOwner[owner], func(i, j int) bool {
					return controlsByOwner[owner][i].ID < controlsByOwner[owner][j].ID
				})
			}

			ownerOrder := []string{"platform", "operator", "shared"}
			for _, owner := range ownerOrder {
				if ctrls, ok := controlsByOwner[owner]; ok && len(ctrls) > 0 {
					subLabel := map[string]string{
						"platform": "Platform-Provided",
						"operator": "Operator-Required",
						"shared":   "Shared Responsibilities",
					}[owner]
					fmt.Fprintf(&b, "### %s\n\n| ID | Title | Owner | CSF Function |\n|----|-------|-------|-------------|\n", subLabel)
					for _, ctrl := range ctrls {
						slug := idSlug(ctrl.ID)
						fmt.Fprintf(&b, "| [%s](%s/%s) | %s | %s | %s |\n",
							ctrl.ID, kind, slug, ctrl.Title,
							ownerBadge(ctrl.Owner), csfBadge(ctrl.CSFFunction))
					}
					b.WriteString("\n")
				}
			}
		}
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
		fmt.Fprintf(&b, "%d of %d controls assessed (%d verified). "+
			"Controls not yet referenced by any audit are omitted.\n\n",
			assessed, total, verified)
		for _, kind := range []string{"technical", "organizational"} {
			fmt.Fprintf(&b, "## %s Controls\n\n", titleCase(kind))

			controlsByOwner := make(map[string][]catalog.Control)
			for _, group := range cat.Groups {
				if groupKind(group) != kind {
					continue
				}
				for _, ctrl := range group.Controls {
					if catalog.EffectiveStatus(&ctrl) == "to_do" {
						continue
					}
					owner := ctrl.Owner
					if owner == "" {
						owner = "platform"
					}
					controlsByOwner[owner] = append(controlsByOwner[owner], ctrl)
				}
			}

			for owner := range controlsByOwner {
				sort.Slice(controlsByOwner[owner], func(i, j int) bool {
					return controlsByOwner[owner][i].ID < controlsByOwner[owner][j].ID
				})
			}

			ownerOrder := []string{"platform", "operator", "shared"}
			for _, owner := range ownerOrder {
				if ctrls, ok := controlsByOwner[owner]; ok && len(ctrls) > 0 {
					subLabel := map[string]string{
						"platform": "Platform-Provided",
						"operator": "Operator-Required",
						"shared":   "Shared Responsibilities",
					}[owner]
					fmt.Fprintf(&b, "### %s\n\n| ID | Title | Status | Owner | CSF Function |\n|----|-------|--------|-------|-------------|\n", subLabel)
					for _, ctrl := range ctrls {
						slug := idSlug(ctrl.ID)
						fmt.Fprintf(&b, "| [%s](%s/%s) | %s | %s | %s | %s |\n",
							ctrl.ID, kind, slug, ctrl.Title,
							statusBadge(catalog.EffectiveStatus(&ctrl)), ownerBadge(ctrl.Owner), csfBadge(ctrl.CSFFunction))
					}
					b.WriteString("\n")
				}
			}
		}
	}
	return b.String()
}

func renderControlPage(ctrl catalog.Control, groupTitle, kind string, audits *audit.AuditSet, activeFindings []*audit.Finding, frameworkRefs map[string]map[string][]string, cfg *config.Config, isPublic bool) string {
	cid := ctrl.ID
	effective := catalog.EffectiveStatus(&ctrl)
	var b strings.Builder
	fmt.Fprintf(&b, "---\nsidebar_label: \"%s\"\ntitle: \"%s — %s\"\n---\n\n", cid, cid, ctrl.Title)
	fmt.Fprintf(&b, "# %s — %s\n\n", cid, ctrl.Title)
	b.WriteString("| Property | Value |\n|----------|-------|\n")
	if !isPublic {
		fmt.Fprintf(&b, "| **Status** | %s |\n", statusBadge(effective))
	}
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
			b.WriteString("- ")
			b.WriteString(componentLink(c, cfg))
			b.WriteString("\n")
		}
	}
	if len(ctrl.References) > 0 {
		b.WriteString("\n## Source References\n\n")
		for _, r := range ctrl.References {
			if strings.HasPrefix(r, "https://") || strings.HasPrefix(r, "http://") {
				fmt.Fprintf(&b, "- [%s](%s)\n", r, r)
				continue
			}
			parts := strings.SplitN(r, "/", 2)
			if len(parts) >= 2 {
				repo := resolveRepo(parts[0], cfg)
				fullRepo := repo
				if fullRepo == "" {
					fullRepo = projectOrg + "/" + parts[0]
				}
				refPath := parts[1]
				if strings.HasPrefix(refPath, "issues/") || strings.HasPrefix(refPath, "pull/") {
					fmt.Fprintf(&b, "- [`%s`](https://github.com/%s/%s)\n", r, fullRepo, refPath)
				} else if refPath == "" || strings.HasSuffix(refPath, "/") || !strings.Contains(filepath.Base(refPath), ".") {
					fmt.Fprintf(&b, "- [`%s`](https://github.com/%s/tree/main/%s)\n", r, fullRepo, refPath)
				} else {
					fmt.Fprintf(&b, "- [`%s`](https://github.com/%s/blob/main/%s)\n", r, fullRepo, refPath)
				}
			} else {
				fmt.Fprintf(&b, "- `%s`\n", r)
			}
		}
	}

	if !isPublic {
		linked := audits.FindingsByControl[cid]
		if len(linked) > 0 {
			b.WriteString("\n## Audit Findings\n\n| Finding | Severity | Status |\n|---------|----------|--------|\n")
			for _, ref := range linked {
				f := ref.Finding
				fmt.Fprintf(&b, "| %s — %s | %s | %s |\n", findingLink(f), f.Title, f.Severity, findingStatusBadge(f.Status))
			}
		}

	}

	hasAnyRef := false
	for _, fw := range cfg.Frameworks {
		if refs, ok := frameworkRefs[fw.ID]; ok {
			if _, ok2 := refs[cid]; ok2 {
				hasAnyRef = true
				break
			}
		}
	}
	if hasAnyRef {
		b.WriteString("\n## Framework Requirements\n\n")
		for _, fw := range cfg.Frameworks {
			refs := frameworkRefs[fw.ID]
			if refs == nil {
				continue
			}
			reqIDs := refs[cid]
			if len(reqIDs) == 0 {
				continue
			}
			links := make([]string, len(reqIDs))
			for i, r := range reqIDs {
				links[i] = fwReqLink(r, fw.ID)
			}
			fmt.Fprintf(&b, "**%s:** %s\n\n", fw.Name, strings.Join(links, ", "))
		}
	}

	return b.String()
}
