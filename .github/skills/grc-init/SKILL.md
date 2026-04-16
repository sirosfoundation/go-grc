---
name: grc-init
description: 'Initialize a new go-grc compliance project. Use when: setting up GRC for a new project, creating .grc.yaml, scaffolding catalog/mappings/audits directory structure, choosing frameworks to track.'
argument-hint: 'Project details (e.g. "new GDPR project for acme-corp", "ISO 27001 compliance")'
---

# GRC Project Initialization

Set up a new go-grc compliance project from scratch. Creates the directory
structure, configuration file, and optional Docusaurus site scaffolding.

## When to Use

- Starting a new compliance management effort
- Adding GRC tracking to an existing project
- Setting up a new framework assessment

## Directory Structure

A go-grc project has this layout:

```
project-root/
├── .grc.yaml              # Project configuration
├── catalog/               # Control definitions
│   ├── technical/         # Technical controls (YAML)
│   └── organizational/    # Organizational controls (YAML)
├── catalog/frameworks/    # Framework requirement catalogs
├── mappings/              # Framework-to-control mappings
├── audits/                # Structured audit findings
├── oscal/                 # OSCAL exports (generated)
└── site/                  # Docusaurus site (optional)
    ├── package.json
    ├── docusaurus.config.ts
    ├── sidebars.ts
    └── docs/              # Generated pages (gitignored)
```

## Configuration (.grc.yaml)

```yaml
project:
  name: "My Project Compliance"
  repo: "myorg/compliance"
  url: "https://compliance.example.com"

catalog:
  dir: catalog
  subdirs: [technical, organizational]
  frameworks_subdir: frameworks

mappings:
  dir: mappings

audits:
  dir: audits

site:
  dir: site/docs

oscal:
  dir: oscal

frameworks:
  - id: iso27001
    name: "ISO 27001 Annex A"
    catalog_file: iso27001-annexa.yaml
    mapping_file: iso27001-annexa.yaml
    sidebar_position: 1
```

## Control YAML Schema

```yaml
group:
  id: auth
  title: Authentication & Identity

controls:
  - id: MY-AUTH-01
    title: Multi-Factor Authentication
    description: |
      Description of what this control does.
    category: technical    # technical | policy | process | physical
    csf_function: protect  # identify | protect | detect | respond | recover | govern
    status: to_do          # to_do | planned | verified | validated
    owner: platform        # platform | operator | shared
    components: []         # Asset/component names (optional)
    references: []         # Source code references (optional)
```

## Procedure

### 1. Create the directory structure

```bash
mkdir -p catalog/{technical,organizational,frameworks} mappings audits
```

### 2. Create .grc.yaml

Configure project identity, directories, and frameworks.

### 3. Create initial control groups

Start with at least one control group YAML file per subdirectory.

### 4. Create framework mappings

For each framework, create a mapping YAML that links framework requirements
to your catalog controls.

### 5. Install go-grc

```bash
go install github.com/sirosfoundation/go-grc/cmd/grc@latest
```

### 6. Set up the Docusaurus site (optional)

```bash
npx create-docusaurus site classic
grc render
cd site && npm run build
```
