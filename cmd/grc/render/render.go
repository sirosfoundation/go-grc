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
var profile string
cmd := &cobra.Command{
Use:   "render",
Short: "Generate Docusaurus site pages from catalog, mappings, and findings",
RunE: func(cmd *cobra.Command, args []string) error {
root, _ := cmd.Flags().GetString("root")
return run(root, profile)
},
}
cmd.Flags().StringVar(&profile, "profile", "public", `Render profile: "public" (no status/findings) or "private" (full detail)`)
return cmd
}

// --- URL maps (populated during run, used for cross-linking) ---
var (
projectOrg   string
controlURL   map[string]string
frameworkURLs map[string]map[string]string // framework ID → req key → URL
)



func run(root, profile string) error {
cfg, err := config.New(root)
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

isPublic := profile != "private"

// Derive effective control statuses from findings before rendering.
if !isPublic {
catalog.DeriveControlStatuses(cat, audits)
}

// Extract org from project repo for GitHub links.
if parts := strings.SplitN(cfg.Project.Repo, "/", 2); len(parts) >= 1 {
projectOrg = parts[0]
}

// Load framework catalogs (normative requirement text)
fwCats := make(map[string]*catalog.FrameworkCatalog)
for _, fw := range cfg.Frameworks {
name := strings.TrimSuffix(fw.CatalogFile, ".yaml")
name = strings.TrimSuffix(name, ".yml")
fwCat, _ := catalog.LoadFrameworkCatalog(cfg.CatalogDir, name)
if fwCat != nil {
fwCats[fw.ID] = fwCat
}
}

// Active findings (have tracking issue, not terminal) — private only
var activeFindings []*audit.Finding
if !isPublic {
for _, ref := range audits.FindingsByID {
f := ref.Finding
if f.TrackingIssue != nil && !f.IsTerminal() {
activeFindings = append(activeFindings, f)
}
}
sort.Slice(activeFindings, func(i, j int) bool {
return sevRank(activeFindings[i].Severity) > sevRank(activeFindings[j].Severity)
})
}

// Build URL maps
controlURL = make(map[string]string)
frameworkURLs = make(map[string]map[string]string)

for _, group := range cat.Groups {
kind := groupKind(group)
for _, ctrl := range group.Controls {
slug := idSlug(ctrl.ID)
controlURL[ctrl.ID] = "/controls/" + kind + "/" + slug
}
}
for _, fw := range cfg.Frameworks {
fm := maps[fw.ID]
if fm == nil {
continue
}
urls := make(map[string]string)
for _, e := range fm.Entries {
slug := entrySlug(e.Key)
urls[e.Key] = "/frameworks/" + fw.Slug + "/" + slug
}
frameworkURLs[fw.ID] = urls
}

// Build framework→control reverse index
frameworkRefs := buildFrameworkRefs(maps)

// Render into a staging directory, then atomically swap each subdir
// so the live site is never missing pages during regeneration.
stagingDir, err := os.MkdirTemp(filepath.Dir(cfg.SiteDir), ".grc-staging-*")
if err != nil {
return fmt.Errorf("creating staging dir: %w", err)
}
defer os.RemoveAll(stagingDir) // clean up on error

origSiteDir := cfg.SiteDir
cfg.SiteDir = stagingDir

// Controls
if err := generateControls(cfg, cat, audits, activeFindings, frameworkRefs, isPublic); err != nil {
cfg.SiteDir = origSiteDir
return err
}
// Frameworks
if err := generateFrameworks(cfg, cat, maps, audits, activeFindings, fwCats, isPublic); err != nil {
cfg.SiteDir = origSiteDir
return err
}
// CSF overview
if err := generateCSFPage(cfg, cat, isPublic); err != nil {
cfg.SiteDir = origSiteDir
return err
}
// Findings
if !isPublic {
if err := generateFindings(cfg, audits, activeFindings); err != nil {
cfg.SiteDir = origSiteDir
return err
}
}
// Landing page
if err := generateLanding(cfg, cat, activeFindings, isPublic); err != nil {
cfg.SiteDir = origSiteDir
return err
}

cfg.SiteDir = origSiteDir

// Atomically swap each subdirectory
subdirs := []string{"controls", "frameworks"}
if !isPublic {
subdirs = append(subdirs, "findings")
}
for _, subdir := range subdirs {
dst := filepath.Join(cfg.SiteDir, subdir)
src := filepath.Join(stagingDir, subdir)
if _, err := os.Stat(src); err != nil {
continue
}
old := dst + ".old"
os.RemoveAll(old)
os.Rename(dst, old)     // move current out of the way
if err := os.Rename(src, dst); err != nil {
os.Rename(old, dst) // rollback on failure
return fmt.Errorf("swapping %s: %w", subdir, err)
}
os.RemoveAll(old) // clean up previous version
}
// Swap top-level files
for _, fname := range []string{"index.md", "csf.md"} {
srcFile := filepath.Join(stagingDir, fname)
dstFile := filepath.Join(cfg.SiteDir, fname)
if _, err := os.Stat(srcFile); err == nil {
os.Rename(dstFile, dstFile+".old")
if err := os.Rename(srcFile, dstFile); err != nil {
os.Rename(dstFile+".old", dstFile)
return fmt.Errorf("swapping %s: %w", fname, err)
}
os.RemoveAll(dstFile + ".old")
}
}

// Generate Docusaurus sidebars.ts and update config based on profile
if err := generateDocusaurusConfig(cfg, isPublic); err != nil {
return err
}

fmt.Println("Site generated.")
return nil
}

// ---------------------------------------------------------------------------
// Control pages
// ---------------------------------------------------------------------------

func generateDocusaurusConfig(cfg *config.Config, isPublic bool) error {
// Generate sidebars.ts
var sb strings.Builder
sb.WriteString(`import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  controlsSidebar: [
    'controls/index',
    {type: 'autogenerated', dirName: 'controls/technical'},
    {type: 'autogenerated', dirName: 'controls/organizational'},
  ],
  architectureSidebar: [
    {type: 'autogenerated', dirName: 'architecture'},
  ],
  frameworksSidebar: [
    {type: 'autogenerated', dirName: 'frameworks'},
  ],
`)
if !isPublic {
sb.WriteString(`  findingsSidebar: [
    {type: 'autogenerated', dirName: 'findings'},
  ],
`)
}
sb.WriteString(`};

export default sidebars;
`)
siteRoot := filepath.Dir(cfg.SiteDir)
sidebarsPath := filepath.Join(siteRoot, "sidebars.ts")
if err := os.WriteFile(sidebarsPath, []byte(sb.String()), 0644); err != nil {
return fmt.Errorf("writing sidebars.ts: %w", err)
}

// Update docusaurus.config.ts: replace navbar items block
configPath := filepath.Join(siteRoot, "docusaurus.config.ts")
data, err := os.ReadFile(configPath)
if err != nil {
return fmt.Errorf("reading docusaurus.config.ts: %w", err)
}
configStr := string(data)

// Replace the findings navbar item: remove it in public mode
findingsItem := `        {
          type: 'docSidebar',
          sidebarId: 'findingsSidebar',
          position: 'left',
          label: 'Findings',
        },`

if isPublic && strings.Contains(configStr, findingsItem) {
configStr = strings.Replace(configStr, findingsItem+"\n", "", 1)
if err := os.WriteFile(configPath, []byte(configStr), 0644); err != nil {
return fmt.Errorf("writing docusaurus.config.ts: %w", err)
}
} else if !isPublic && !strings.Contains(configStr, findingsItem) {
// Re-add findings item before the GitHub link
ghItem := `        {
          href: 'https://github.com/sirosfoundation',`
configStr = strings.Replace(configStr, ghItem, findingsItem+"\n"+ghItem, 1)
if err := os.WriteFile(configPath, []byte(configStr), 0644); err != nil {
return fmt.Errorf("writing docusaurus.config.ts: %w", err)
}
}

// In public mode, remove stale findings directory if it exists
if isPublic {
findingsDir := filepath.Join(cfg.SiteDir, "docs", "findings")
os.RemoveAll(findingsDir)
}

return nil
}

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
label := "Technical Controls (Platform-Provided)"
if kind == "organizational" {
label = "Organizational Controls (Operator-Required)"
}
fmt.Fprintf(&b, "## %s\n\n| ID | Title | Owner | CSF Function |\n|----|-------|-------|-------------|\n", label)
for _, group := range cat.Groups {
if groupKind(group) != kind {
continue
}
for _, ctrl := range group.Controls {
slug := idSlug(ctrl.ID)
fmt.Fprintf(&b, "| [%s](%s/%s) | %s | %s | %s |\n",
ctrl.ID, kind, slug, ctrl.Title,
ownerBadge(ctrl.Owner), csfBadge(ctrl.CSFFunction))
}
}
b.WriteString("\n")
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
label := "Technical Controls (Platform-Provided)"
if kind == "organizational" {
label = "Organizational Controls (Operator-Required)"
}
fmt.Fprintf(&b, "## %s\n\n| ID | Title | Status | Owner | CSF Function |\n|----|-------|--------|-------|-------------|\n", label)
for _, group := range cat.Groups {
if groupKind(group) != kind {
continue
}
for _, ctrl := range group.Controls {
if catalog.EffectiveStatus(&ctrl) == "to_do" {
continue
}
slug := idSlug(ctrl.ID)
fmt.Fprintf(&b, "| [%s](%s/%s) | %s | %s | %s | %s |\n",
ctrl.ID, kind, slug, ctrl.Title,
statusBadge(catalog.EffectiveStatus(&ctrl)), ownerBadge(ctrl.Owner), csfBadge(ctrl.CSFFunction))
}
}
b.WriteString("\n")
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
if repo != "" {
fmt.Fprintf(&b, "- [`%s`](https://github.com/%s/tree/main/%s)\n", r, repo, parts[1])
} else {
fmt.Fprintf(&b, "- [`%s`](https://github.com/%s/%s/tree/main/%s)\n", r, projectOrg, parts[0], parts[1])
}
} else {
fmt.Fprintf(&b, "- `%s`\n", r)
}
}
}

// Linked findings (private only)
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

// Framework cross-references (generic)
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

// ---------------------------------------------------------------------------
// Framework pages (summary + per-requirement) — fully generic
// ---------------------------------------------------------------------------

func generateFrameworks(cfg *config.Config, cat *catalog.Catalog, maps mapping.Mappings, audits *audit.AuditSet, activeFindings []*audit.Finding, fwCats map[string]*catalog.FrameworkCatalog, isPublic bool) error {
fwDir := filepath.Join(cfg.SiteDir, "frameworks")
writePage(filepath.Join(fwDir, "index.md"), renderFrameworkIndex(cfg.Frameworks, maps))
writePage(filepath.Join(fwDir, "_category_.json"), categoryJSON("Frameworks", 2))

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
pos := fw.SidebarPosition + 1 // +1 since overview takes position 1
writePage(filepath.Join(dir, "_category_.json"),
fmt.Sprintf(`{"label":%q,"position":%d,"link":{"type":"doc","id":"frameworks/%s/index"}}`, catLabel, pos, fw.Slug)+"\n")
writePage(filepath.Join(dir, "index.md"), renderFrameworkSummary(fw, fm, fwCat, isPublic))

for _, e := range fm.Entries {
slug := entrySlug(e.Key)
var catEntry *catalog.FrameworkRequirement
if fwCat != nil {
catEntry = fwCat.ByID[e.Key]
}
notes := e.Notes
if fw.ID == "eudi" {
// EUDI uses "observation" field; treat as notes.
}
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
writePage(filepath.Join(dir, slug+".md"), page)
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
sourceLink = fmt.Sprintf("[%s](%s)", fw.Name, fw.SourceURL)
}
fmt.Fprintf(&b, "---\nsidebar_label: %s\ntitle: %s\n---\n\n# %s\n\n", fw.Name, fw.Name, sourceLink)

if isPublic {
// Public: no dashboard, no status columns
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
// Private: full dashboard + status
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

// isUnassessedStatus returns true if the status means "not yet audited".
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
source: fw.Source,
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

// ---------------------------------------------------------------------------
// Per-requirement detail page (unified for all frameworks)
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

// Related findings (private only)
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
	fmt.Fprintf(&b, "---\n\n*Source: [%s](%s)*\n", rc.source, rc.sourceURL)
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
fmt.Fprintf(&b, "%d open findings are tracked as GitHub issues.\n\n", len(findings))
b.WriteString("| Finding | Severity | Owner | Controls |\n|---------|----------|-------|----------|\n")
for _, f := range findings {
icon := sevIcon(f.Severity)
fmt.Fprintf(&b, "| %s | %s %s | %s | %s |\n",
findingLink(f), icon, f.Severity, ownerBadge(f.Owner), controlLinks(f.Controls))
}
return b.String()
}

// ---------------------------------------------------------------------------
// Landing page

// ---------------------------------------------------------------------------
// CSF (Cybersecurity Framework) overview page
// ---------------------------------------------------------------------------

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
// Group controls by CSF function; track which directory each control lives in
byFunc := map[string][]catalog.Control{}
ctrlKind := map[string]string{} // control ID -> "technical" or "organizational"
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

// Mermaid diagram
b.WriteString("```mermaid\nflowchart LR\n")
for _, fn := range csfFunctions {
upper := strings.ToUpper(fn.ID)
b.WriteString(fmt.Sprintf("  %s[\"%s\"]\n", upper, fn.Name))
b.WriteString(fmt.Sprintf("  style %s fill:%s,color:#fff,stroke:none\n", upper, fn.Color))
}
b.WriteString("  GOVERN --> IDENTIFY --> PROTECT --> DETECT --> RESPOND --> RECOVER\n")
b.WriteString("```\n\n")

// Sections per function
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
fmt.Fprintf(&b, "| [%s](controls/%s/%s) | %s | %s |\n",
ctrl.ID, kind, slug, ctrl.Title, ownerBadge(ctrl.Owner))
} else {
fmt.Fprintf(&b, "| [%s](controls/%s/%s) | %s | %s | %s |\n",
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

	fence := string([]byte{96, 96, 96})
	var b strings.Builder
	b.WriteString("\n## How It Fits Together\n\n")
	b.WriteString("Compliance frameworks define **requirements** that are mapped to\n")
	b.WriteString("platform **controls**.  Each control is categorised under a\n")
	b.WriteString("[NIST CSF 2.0](/csf) function so that coverage can be reviewed\n")
	b.WriteString("at every level of abstraction.\n\n")
	fmt.Fprintf(&b, "%smermaid\n", fence)
	b.WriteString("flowchart LR\n")

	b.WriteString("  subgraph Frameworks\n")
	b.WriteString("    direction TB\n")
	for _, fw := range cfg.Frameworks {
		fwID := strings.ReplaceAll(fw.ID, "-", "_")
		fmt.Fprintf(&b, "    FW_%s[\"%s\"]\n", fwID, fw.Name)
	}
	b.WriteString("  end\n\n")

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
	b.WriteString("  subgraph Controls\n")
	b.WriteString("    direction TB\n")
	fmt.Fprintf(&b, "    TECH[\"%d Technical\"]\n", techCount)
	fmt.Fprintf(&b, "    ORG[\"%d Organizational\"]\n", orgCount)
	fmt.Fprintf(&b, "    ALL{{\"%d Controls\"}}\n", total)
	b.WriteString("    TECH --- ALL\n")
	b.WriteString("    ORG --- ALL\n")
	b.WriteString("  end\n\n")

	b.WriteString("  subgraph CSF[\"CSF 2.0 Functions\"]\n")
	b.WriteString("    direction TB\n")
	for _, fn := range csfFunctions {
		upper := strings.ToUpper(fn.ID)
		count := csfCounts[fn.ID]
		fmt.Fprintf(&b, "    %s[\"%s \u00b7 %d\"]\n", upper, fn.Name, count)
	}
	b.WriteString("  end\n\n")

	b.WriteString("  Frameworks -- requirements --> Controls\n")
	b.WriteString("  Controls -- categorised --> CSF\n\n")

	b.WriteString("  style Frameworks fill:#f0f4ff,stroke:#6366f1\n")
	b.WriteString("  style Controls fill:#f0fdf4,stroke:#22c55e\n")
	b.WriteString("  style CSF fill:#fffbeb,stroke:#eab308\n")
	for _, fn := range csfFunctions {
		upper := strings.ToUpper(fn.ID)
		fmt.Fprintf(&b, "  style %s fill:%s,color:#fff,stroke:none\n", upper, fn.Color)
	}
	fmt.Fprintf(&b, "%s\n\n", fence)
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
// ---------------------------------------------------------------------------

func generateLanding(cfg *config.Config, cat *catalog.Catalog, activeFindings []*audit.Finding, isPublic bool) error {
total := len(cat.Controls)
// Build framework name list for quick links
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

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func entrySlug(key string) string {
s := strings.ToLower(key)
s = strings.ReplaceAll(s, "-", "_")
s = strings.ReplaceAll(s, ".", "_")
s = nonAlnum.ReplaceAllString(s, "_")
return strings.Trim(s, "_")
}

func groupKind(group catalog.Group) string {
if group.SourceDir != "" {
return group.SourceDir
}
return "technical"
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
"in_progress": `<span class="badge--to-do">in_progress</span>`,
"validated": `<span class="badge--verified">validated</span>`,
}
if v, ok := m[s]; ok {
return v
}
return s
}

func ownerBadge(s string) string {
badges := map[string]string{
"platform": `<span class="badge--platform">platform</span>`,
"operator": `<span class="badge--operator">operator</span>`,
"shared":   `<span class="badge--platform">platform</span> <span class="badge--operator">operator</span>`,
}
parts := strings.Split(s, ", ")
var out []string
for _, p := range parts {
if v, ok := badges[p]; ok {
out = append(out, v)
} else {
out = append(out, p)
}
}
return strings.Join(out, " ")
}

func csfBadge(s string) string {
anchors := map[string]string{
"govern":   "govern-gv",
"identify": "identify-id",
"protect":  "protect-pr",
"detect":   "detect-de",
"respond":  "respond-rs",
"recover":  "recover-rc",
}
if a, ok := anchors[s]; ok {
return fmt.Sprintf(`[<span class="badge--csf">%s</span>](/csf#%s)`, s, a)
}
return s
}

func coverageSpan(s string) string {
m := map[string]string{
"full":         `<span class="coverage--full">full</span>`,
"partial":      `<span class="coverage--partial">partial</span>`,
"none":         `<span class="coverage--none">none</span>`,
"not_assessed": `<span class="coverage--not-assessed">\u2014</span>`,
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
"accepted":   `<span class="badge--verified">accepted</span>`,
}
if v, ok := m[s]; ok {
return v
}
return s
}

func sevIcon(s string) string {
m := map[string]string{"critical": "\U0001f534", "high": "\U0001f7e0", "medium": "\U0001f7e1", "low": "\U0001f7e2"}
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

func fwReqLink(key, fwID string) string {
if urls, ok := frameworkURLs[fwID]; ok {
if url, ok2 := urls[key]; ok2 {
return "[" + key + "](" + url + ")"
}
}
return "`" + key + "`"
}

func findingLink(f *audit.Finding) string {
if f.TrackingIssue != nil {
return fmt.Sprintf("[%s](https://github.com/%s/issues/%d)", f.ID, f.TrackingIssue.Repo, f.TrackingIssue.Number)
}
return "`" + f.ID + "`"
}

func truncate(s string, n int) string {
s = strings.TrimSpace(s)
if len(s) <= n {
return s
}
return s[:n-3] + "..."
}

// --- Framework reverse index (generic) ---

func buildFrameworkRefs(maps mapping.Mappings) map[string]map[string][]string {
// fwID → controlID → []reqKey
refs := map[string]map[string][]string{}
for fwID, fm := range maps {
fwRefs := map[string][]string{}
for _, e := range fm.Entries {
for _, cid := range e.Controls {
fwRefs[cid] = append(fwRefs[cid], e.Key)
}
}
refs[fwID] = fwRefs
}
return refs
}

// --- ENISA reference resolution (applied to EUDI descriptions) ---

// Pre-compiled ENISA reference patterns.
var enisaRefPatterns []struct {
re          *regexp.Regexp
replacement string
}

var arfRe *regexp.Regexp

func init() {
enisaRefPatterns = []struct {
re          *regexp.Regexp
replacement string
}{
{regexp.MustCompile(`OWASP\s+Application\s+Security\s+Verification\s+Standard\s*\[i\.10\]`),
"[OWASP ASVS](https://owasp.org/www-project-application-security-verification-standard/)"},
{regexp.MustCompile(`ECCG\s+Agreed\s+Cryptograph(?:y|ic)\s+Mechanisms\s*\[2\]`),
"[ECCG Agreed Cryptographic Mechanisms](https://www.enisa.europa.eu/topics/certification/eccg)"},
{regexp.MustCompile(`CIR\s*\(EU\)\s*2024/2981\s*\[i\.3\]`),
"[CIR (EU) 2024/2981](https://eur-lex.europa.eu/eli/reg_impl/2024/2981/oj)"},
{regexp.MustCompile(`CIR\s*\(EU\)\s*2015/1502\s*\[i\.2\]`),
"[CIR (EU) 2015/1502](https://eur-lex.europa.eu/eli/reg_impl/2015/1502/oj)"},
{regexp.MustCompile(`EN\s+319\s+401\s*\[1\]`),
"[EN 319 401](https://www.etsi.org/deliver/etsi_en/319400_319499/319401/)"},
}
arfRe = regexp.MustCompile(`\[ARF ([^\]]+)\]`)
}

func resolveENISARefs(text string) string {
for _, r := range enisaRefPatterns {
text = r.re.ReplaceAllString(text, r.replacement)
}
text = arfRe.ReplaceAllString(text, "[ARF $1](https://eudi.dev/2.8.0/architecture-and-reference-framework-main/)")
return text
}
