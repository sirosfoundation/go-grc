# go-grc Generalization Plan

Make `go-grc` a general-purpose GRC toolchain that any organization can adopt,
with all project-specific content pushed into configuration and data files.

## 1. Current Hardcoding Inventory

### Category A — Project Identity (easy to fix)

| Location | Hardcoded Content |
|---|---|
| `pkg/config/config.go:5` | `DefaultRepo = "sirosfoundation/compliance"` |
| `pkg/config/config.go:6` | `ComplianceURL = "https://compliance.siros.org"` |
| `cmd/grc/main.go:24` | `"GRC toolchain for SirosID"` |
| `cmd/grc/render/render.go` | `"SirosID Compliance Dashboard"` in landing page |
| `pkg/catalog/catalog.go:1` | `"SirosID control catalog"` in doc comment |

### Category B — Framework Registry (medium effort)

The four frameworks (EUDI, ISO 27001, GDPR, OWASP ASVS) are compiled into the
tool as Go types and hard-wired function calls:

| Location | Content |
|---|---|
| `pkg/mapping/mapping.go` | Four separate types (`EUDIMapping`, `ISOFile`, `GDPRFile`, `ASVSFile`) |
| `pkg/mapping/mapping.go:86-113` | Four hardcoded filenames in `Load()` |
| `cmd/grc/derive/derive.go:49-52` | Four framework-specific derive functions |
| `cmd/grc/render/render.go:50-63` | Four `generateXxx()` calls |
| `cmd/grc/render/render.go` | Four `generateXxx()` functions with hardcoded titles/sidebar positions |

### Category C — Catalog Shape (low effort)

| Location | Content |
|---|---|
| `pkg/catalog/catalog.go:75` | `[]string{"technical", "organizational"}` subdirectory names |
| `pkg/catalog/framework.go:33` | `"frameworks"` subdirectory name |

### Category D — Rendering (medium effort)

The renderer is Docusaurus-specific (Markdown frontmatter with `sidebar_position`,
`slug`, `title`, `sidebar_label`). This is acceptable as a default, but the
dashboard title, sidebar ordering, and page structure should be configurable.

## 2. Proposed Configuration: `.grc.yaml`

Introduce a project-level configuration file loaded from `--root`:

```yaml
# .grc.yaml — project configuration for go-grc
project:
  name: "SirosID"                          # Display name for dashboard
  repo: "sirosfoundation/compliance"       # Default GitHub repo for issue sync
  url: "https://compliance.siros.org"      # Published site URL (for links)

catalog:
  dir: catalog                             # relative to root
  subdirs: [technical, organizational]     # control group subdirectories
  frameworks_subdir: frameworks            # framework catalog subdir

mappings:
  dir: mappings

audits:
  dir: audits

site:
  dir: site/docs                           # Docusaurus output directory

oscal:
  dir: oscal

# Framework registry — each entry defines a loadable framework mapping.
# The tool discovers frameworks from this list instead of hardcoding them.
frameworks:
  - id: eudi
    name: "EUDI Security Requirements"
    catalog_file: eudi-secreq.yaml         # in catalog/frameworks/
    mapping_file: eudi-secreq.yaml         # in mappings/
    type: keyed                            # mapping has a primary key field
    key_field: id                          # YAML field name for requirement ID
    result_field: result                   # field tracking assessment outcome
    status_field: status                   # field tracking work status
    sidebar_position: 1

  - id: iso27001
    name: "ISO 27001 Annex A"
    catalog_file: iso27001-annexa.yaml
    mapping_file: iso27001-annexa.yaml
    type: keyed
    key_field: annex_a
    coverage_field: coverage
    sidebar_position: 2

  - id: gdpr
    name: "GDPR Checklist"
    catalog_file: gdpr-checklist.yaml
    mapping_file: gdpr.yaml
    type: keyed
    key_field: match_name
    coverage_field: coverage
    sidebar_position: 3

  - id: owasp-asvs
    name: "OWASP ASVS 4.0.3 Level 3"
    catalog_file: owasp-asvs.yaml
    mapping_file: owasp-asvs.yaml
    type: keyed
    key_field: section
    coverage_field: coverage
    sidebar_position: 4
```

### Impact on Code

| Component | Change |
|---|---|
| `pkg/config` | Load `.grc.yaml`, expose `Project`, `Frameworks[]` |
| `pkg/mapping` | Replace 4 typed structs with one `GenericMapping` that uses `key_field` etc. |
| `cmd/grc/derive` | Single `deriveFrameworkCoverage()` iterating `cfg.Frameworks` |
| `cmd/grc/render` | Single `generateFramework()` driven by framework config |
| `cmd/grc/sync` | Use `cfg.Project.Repo` as default |
| `cmd/grc/main.go` | Use `cfg.Project.Name` in description |

### Backward Compatibility

If no `.grc.yaml` exists, fall back to current defaults (SIROS-specific).
This means existing compliance repos continue to work unchanged.

## 3. Implementation Phases

### Phase 1 — Configuration Loading (non-breaking)

1. Add `.grc.yaml` schema to `pkg/config`
2. Load config if present, fall back to hardcoded defaults
3. Replace `DefaultRepo` / `ComplianceURL` constants with config fields
4. Dashboard title from `project.name`

### Phase 2 — Generic Framework Registry

1. Define `FrameworkConfig` struct in config
2. Replace `EUDIMapping`/`ISOFile`/`GDPRFile`/`ASVSFile` with a generic
   `FrameworkMapping` that loads any YAML mapping structure
3. Single `Load(dir string, frameworks []FrameworkConfig)` function
4. Single `deriveFrameworkCoverage(fw FrameworkConfig, ...)` function
5. Single `generateFramework(fw FrameworkConfig, ...)` renderer

### Phase 3 — Extensible Rendering

1. Configurable output format (Docusaurus Markdown is the default)
2. Dashboard page uses `project.name` and `project.url`
3. Sidebar positions from framework config
4. Potential future: Hugo, MkDocs, plain HTML renderers

## 4. Copilot Skills

### Current Skills (in compliance repo)

Four skills exist in `compliance/.github/skills/`:

| Skill | Location | Description |
|---|---|---|
| `compliance-audit` | compliance repo | Conduct audits, create findings YAML |
| `compliance-assess` | compliance repo | Triage/reclassify findings, update risk |
| `compliance-sync` | compliance repo | Sync findings ↔ GitHub issues/PRs |
| `compliance-report` | compliance repo | Build site, verify dashboard |

### Where Should Skills Live?

Skills fall into two categories:

**Tool-generic skills** — should move to `go-grc` repo (`.github/skills/`):
These describe the YAML schema, CLI commands, and the GRC workflow itself.
Any project using go-grc benefits from these.

**Project-specific skills** — stay in the compliance repo:
These reference specific controls, frameworks, or project architecture.

### Proposed Skill Set for go-grc

#### 1. `grc-audit` (move from compliance, generalize)

```yaml
---
name: grc-audit
description: >
  Conduct a security/compliance audit against a framework.
  Use when: running a framework audit, scanning codebase for compliance gaps,
  creating structured findings in audits/*.yaml linked to catalog controls.
argument-hint: 'Framework or scope to audit (e.g. "GDPR data flows", "access controls")'
---
```

Teaches the agent:
- The audit YAML schema (`audit:` + `findings:[]`)
- How to create well-formed finding records
- How to link findings to catalog controls
- Severity classification guidance
- Evidence collection patterns

#### 2. `grc-assess` (move from compliance, generalize)

```yaml
---
name: grc-assess
description: >
  Perform risk assessment on compliance findings.
  Use when: reclassifying severity, updating status, adding mitigations,
  triaging audit results, prioritizing remediation.
argument-hint: 'What to assess (e.g. "triage findings", "reclassify F-3 severity")'
---
```

Teaches the agent:
- Status lifecycle: `open → in_progress → resolved → closed`
- The `grc derive` command and what it does
- How control statuses relate to finding statuses
- Risk classification criteria

#### 3. `grc-sync` (move from compliance, generalize)

```yaml
---
name: grc-sync
description: >
  Synchronize findings with GitHub issues and PRs.
  Use when: creating tracking issues, linking PRs to findings,
  checking if issues are closed, refreshing audit data from repos.
argument-hint: 'What to sync (e.g. "sync all open findings", "link issue #42 to P-3")'
---
```

Teaches the agent:
- The `grc sync` and `grc sync link` CLI commands
- GitHub issue/PR state mapping to finding status
- Tracking issue creation patterns
- Evidence collection from PRs

#### 4. `grc-report` (move from compliance, generalize)

```yaml
---
name: grc-report
description: >
  Generate, verify, and review the compliance site.
  Use when: building the site, checking dashboard accuracy,
  verifying OSCAL output, reviewing stale data, preparing for review.
argument-hint: 'What to check (e.g. "full build", "verify counts", "check stale findings")'
---
```

Teaches the agent:
- The `grc render` and `grc export` commands
- Docusaurus build process
- OSCAL output format
- Dashboard verification procedures

#### 5. `grc-init` (NEW)

```yaml
---
name: grc-init
description: >
  Initialize a new go-grc compliance project.
  Use when: setting up GRC for a new project, creating initial .grc.yaml,
  scaffolding catalog/mappings/audits directory structure, choosing frameworks.
argument-hint: 'Project to initialize (e.g. "new GDPR project", "ISO 27001 for acme-corp")'
---
```

Teaches the agent:
- How to create a `.grc.yaml` configuration file
- The directory structure (`catalog/`, `mappings/`, `audits/`)
- Available framework templates
- How to scaffold initial control groups
- Docusaurus site scaffolding

#### 6. `grc-catalog` (NEW)

```yaml
---
name: grc-catalog
description: >
  Manage the control catalog: add, update, or review controls.
  Use when: adding new security controls, reviewing control coverage,
  updating control status, mapping controls to components.
argument-hint: 'What to do (e.g. "add access control", "review crypto controls")'
---
```

Teaches the agent:
- Control YAML schema (`group:` + `controls:[]`)
- Control categories (technical, policy, process, physical)
- CSF function mapping (identify, protect, detect, respond, recover, govern)
- Status values and their meaning
- Owner classification (platform, operator, shared)
- How to cross-reference controls with framework mappings

#### 7. `grc-map` (NEW)

```yaml
---
name: grc-map
description: >
  Create or update framework-to-control mappings.
  Use when: mapping new framework requirements to controls, adding a new
  framework, reviewing coverage gaps, updating assessment results.
argument-hint: 'Framework to map (e.g. "map NIST 800-53 controls", "check ISO coverage gaps")'
---
```

Teaches the agent:
- Mapping YAML schema (requirements → controls)
- Coverage assessment values (full, partial, none, not_assessed)
- Result/status values per framework type
- How to register a new framework in `.grc.yaml`
- Gap analysis methodology

### Skill Distribution

| Skill | Repository | Rationale |
|---|---|---|
| `grc-audit` | **go-grc** | Schema + CLI knowledge is tool-generic |
| `grc-assess` | **go-grc** | Risk framework is tool-generic |
| `grc-sync` | **go-grc** | GitHub sync is tool-generic |
| `grc-report` | **go-grc** | Build/render is tool-generic |
| `grc-init` | **go-grc** | Scaffolding is tool-generic |
| `grc-catalog` | **go-grc** | Catalog schema is tool-generic |
| `grc-map` | **go-grc** | Mapping schema is tool-generic |

The compliance repo would keep only project-specific instructions (e.g. a
`.github/copilot-instructions.md` that says "this project uses go-grc with
EUDI, ISO, GDPR, and OWASP ASVS frameworks — see go-grc skills for workflow").

### New CLI Command: `grc init`

To support `grc-init`, add a new subcommand:

```
grc init [--name NAME] [--repo OWNER/REPO] [--frameworks eudi,iso27001,gdpr,owasp-asvs]
```

This scaffolds:
- `.grc.yaml` with selected frameworks
- `catalog/` with empty group files
- `mappings/` with skeleton framework mappings
- `audits/` directory
- `site/` with Docusaurus template (package.json, docusaurus.config.ts, sidebars.ts)

## 5. Summary of Changes

| Area | Current | Proposed |
|---|---|---|
| Project identity | Constants in Go source | `.grc.yaml` → `project:` |
| Framework registry | 4 hardcoded types + functions | Data-driven from `.grc.yaml` → `frameworks:[]` |
| Mapping types | 4 separate Go structs | 1 generic `FrameworkMapping` |
| Derive logic | 4 framework-specific functions | 1 generic function |
| Render logic | 4 `generateXxx()` functions | 1 data-driven `generateFramework()` |
| Catalog subdirs | Hardcoded `[technical, organizational]` | `.grc.yaml` → `catalog.subdirs` |
| GitHub repo | Constant `DefaultRepo` | `.grc.yaml` → `project.repo` |
| Copilot skills | 4 SIROS-specific in compliance | 7 generic in go-grc |
| Scaffolding | Manual | `grc init` command |
