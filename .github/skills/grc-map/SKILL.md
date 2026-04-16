---
name: grc-map
description: 'Create or update framework-to-control mappings. Use when: mapping new framework requirements to controls, adding a new compliance framework, reviewing coverage gaps, updating assessment results.'
argument-hint: 'Framework to map (e.g. "map NIST 800-53 controls", "check ISO coverage gaps", "add SOC 2 framework")'
---

# GRC Framework Mapping

Create or update mappings between compliance framework requirements and
internal security controls. Mappings track how your controls satisfy
external requirements.

## When to Use

- Mapping a new compliance framework
- Reviewing coverage gaps in existing mappings
- Updating assessment results after remediation
- Adding a new framework to `.grc.yaml`

## Mapping Schema

Mappings live in `mappings/<framework>.yaml`. The exact structure depends
on the framework, but all follow this pattern:

```yaml
# For key-value frameworks (ISO, GDPR, OWASP ASVS):
mappings:
  - <key_field>: "Requirement ID"
    controls: [CTRL-01, CTRL-02]
    coverage: full|partial|none|not_assessed
    owner: platform|operator|shared
    notes: "Assessment notes"

# For EUDI-style frameworks with result tracking:
requirements:
  - id: "REQ-ID"
    controls: [CTRL-01]
    result: compliant|partially_compliant|non_compliant|not_applicable|not_assessed
    status: done|in_progress|to_do
    owner: platform|operator|shared
    observation: "Assessment notes"
```

## Coverage Values

| Coverage | Meaning |
|----------|---------|
| `full` | All mapped controls are verified/validated |
| `partial` | Some mapped controls are verified, others pending |
| `none` | No mapped controls are verified |
| `not_assessed` | Not applicable or deliberately excluded |

Note: `grc derive` automatically computes coverage from control statuses.
Manual coverage values are overwritten by derived values.

## Adding a New Framework

### 1. Create the framework catalog

Add a requirement catalog in `catalog/frameworks/<name>.yaml`:

```yaml
framework:
  id: my-framework
  title: "Framework Name"
  version: "1.0"
  source: https://example.com/framework

requirements:
  - id: REQ-1.1
    title: "Requirement Title"
    section: "1.1"
    description: |
      Full requirement text.
```

### 2. Create the mapping file

Add `mappings/<name>.yaml` with one entry per requirement,
mapping to your catalog controls.

### 3. Register in .grc.yaml

```yaml
frameworks:
  - id: my-framework
    name: "My Framework"
    catalog_file: my-framework.yaml
    mapping_file: my-framework.yaml
    sidebar_position: 5
```

### 4. Run the pipeline

```bash
grc derive     # Compute initial coverage
grc render     # Generate framework pages
```

## Gap Analysis

To identify coverage gaps:

1. List all framework requirements with `coverage: none`
2. For each, check if controls exist but aren't verified
3. If no controls map to the requirement, consider:
   - Adding a new control
   - Marking as `not_assessed` with justification
   - Documenting compensating controls
