---
name: grc-audit
description: 'Conduct a security/compliance audit against a framework. Use when: running a framework audit, scanning codebase for compliance gaps, creating structured findings in audits/*.yaml linked to catalog controls. Produces structured findings that feed into the compliance dashboard.'
argument-hint: 'Framework or scope to audit (e.g. "GDPR data flows", "ISO 27001 access controls")'
---

# GRC Audit

Conduct a security or compliance audit against a specific framework or scope.
Produces structured findings in `audits/*.yaml` that feed into the compliance dashboard.

## When to Use

- Scanning the codebase against a compliance framework
- Performing PII data-flow analysis
- Reviewing a specific control domain (e.g. data protection, access control)
- Creating a new structured audit with findings
- Extending an existing audit with additional findings

## Prerequisites

The project must have a go-grc compliance structure:
- `catalog/` with control YAML files
- `mappings/` with framework mapping files
- `audits/` for findings
- Optionally a `.grc.yaml` configuration file

## Data Model

### Audit YAML structure (`audits/<id>.yaml`)

```yaml
audit:
  id: FRAMEWORK-YYYY-MM       # Unique audit identifier
  title: Descriptive Title
  date: "YYYY-MM-DD"
  scope: What was audited
  method: How the audit was conducted
  assurance: ai-assisted|manual|automated
  narrative: ./references/narrative.md   # Optional reference document

findings:
  - id: F-1                   # Unique within audit
    title: Short description
    severity: low|medium|high|critical
    status: open|in_progress|resolved|closed
    owner: platform|operator
    controls: [CTRL-ID-01]    # Links to catalog control IDs
    issues:
      - repo: org/repo
        number: 42
    pull_requests:
      - repo: org/repo
        number: 43
    description: |
      Detailed description of the finding.
    evidence:
      - file: relative/path/to/file.go
        line: 25
    resolution: |
      Description of how the finding was resolved.
```

## Procedure

### 1. Choose scope and framework

Determine which framework to audit against (EUDI, ISO 27001, GDPR, OWASP ASVS,
or a custom framework registered in `.grc.yaml`).

### 2. Review existing controls

Read `catalog/technical/*.yaml` and `catalog/organizational/*.yaml` to understand
what controls are already defined for the project.

### 3. Scan the codebase

For each relevant framework requirement, review the codebase against the mapped
controls. Check for:
- Implementation gaps (control claims vs. reality)
- Configuration weaknesses
- Missing validation or error handling
- Data protection gaps

### 4. Create findings

For each gap found, create a structured finding in the appropriate audit YAML file.
Each finding must link to at least one catalog control via the `controls` field.

### 5. Run the pipeline

After creating findings:
```bash
grc derive     # Update control and mapping statuses
grc render     # Regenerate compliance pages
```

## Severity Classification

| Severity | Criteria |
|----------|---------|
| `critical` | Exploitable vulnerability with immediate impact |
| `high` | Significant gap requiring urgent remediation |
| `medium` | Notable weakness with mitigating factors |
| `low` | Minor improvement or hardening opportunity |
