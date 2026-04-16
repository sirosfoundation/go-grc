---
name: grc-catalog
description: 'Manage the control catalog: add, update, or review security controls. Use when: adding new controls, reviewing control coverage, updating control status, mapping controls to components or CSF functions.'
argument-hint: 'What to do (e.g. "add access control", "review crypto controls", "list all to-do controls")'
---

# GRC Catalog Management

Manage the security control catalog. Controls are the building blocks
that framework requirements map to.

## When to Use

- Adding new security controls
- Reviewing control coverage against a framework
- Updating control status or metadata
- Mapping controls to software components
- Organizing controls into groups

## Control Schema

Controls live in `catalog/<subdir>/*.yaml`:

```yaml
group:
  id: group-id
  title: Group Title

controls:
  - id: PREFIX-GROUP-NN      # e.g. SID-AUTH-01
    title: Human-readable title
    description: |
      What this control does and how it's implemented.
    category: technical       # technical | policy | process | physical
    csf_function: protect     # NIST CSF function
    status: to_do             # to_do | planned | verified | validated
    owner: platform           # platform | operator | shared
    components:               # Optional - affected components
      - Component Name
    references:               # Optional - source code references
      - repo/path/to/file.go
```

## Status Values

| Status | Meaning |
|--------|---------|
| `to_do` | Not yet implemented or verified |
| `planned` | Implementation scheduled or in progress |
| `verified` | Implementation confirmed via review |
| `validated` | Implementation verified with evidence (all findings resolved) |

Note: `derive` automatically computes effective status from linked findings.
A control with open findings will have its derived status downgraded.

## CSF Functions (NIST Cybersecurity Framework)

| Function | Purpose |
|----------|---------|
| `identify` | Asset management, risk assessment |
| `protect` | Access control, data security, training |
| `detect` | Monitoring, anomaly detection |
| `respond` | Incident response, mitigation |
| `recover` | Recovery planning, improvements |
| `govern` | Governance, policy, oversight |

## Owner Classification

| Owner | Meaning |
|-------|---------|
| `platform` | Implemented by the platform/product team |
| `operator` | Required of the deploying organization |
| `shared` | Split responsibility between platform and operator |

## Procedure

### 1. Choose the right group

Controls are organized into groups in `catalog/technical/` and
`catalog/organizational/`. Pick the appropriate group file or create a new one.

### 2. Define the control

Follow the naming convention: `PREFIX-GROUP-NN` where PREFIX is the project
prefix (e.g. `SID`), GROUP is the control domain (e.g. `AUTH`, `CRYPTO`),
and NN is the sequence number.

### 3. Map to frameworks

After adding a control, update relevant framework mappings in `mappings/`
to reference the new control ID.

### 4. Run the pipeline

```bash
grc derive     # Compute statuses
grc render     # Regenerate site
```
