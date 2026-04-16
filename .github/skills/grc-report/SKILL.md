---
name: grc-report
description: 'Generate, verify, and review the compliance site. Use when: building the site, checking dashboard accuracy, verifying OSCAL output, reviewing stale data, preparing for a compliance review. Runs grc render and Docusaurus build.'
argument-hint: 'What to check (e.g. "full build", "verify dashboard counts", "check stale findings")'
---

# GRC Report & Verification

Build the compliance site, verify dashboard accuracy, check for stale data,
and review generated output for correctness.

## When to Use

- After editing catalog, mappings, or audit YAML files
- Before committing changes — verify the build passes
- Reviewing dashboard accuracy
- Checking for stale data (old findings still marked open)
- Preparing for an external compliance review
- Validating OSCAL or JSON export output

## CLI Commands

### Render site pages
```bash
grc render [-r ROOT]
```

Generates Docusaurus Markdown pages from YAML sources. Output goes to the
directory specified in `.grc.yaml` under `site.dir` (default: `site/docs`).

### Export evidence package
```bash
grc export [-r ROOT]
```

Generates a JSON evidence package with all controls, findings, and evidence
suitable for auditor review.

### Show status
```bash
grc status [-r ROOT]
```

Read-only overview: control counts, finding counts, tracked vs untracked.

## Full Build Pipeline

```bash
grc sync              # Sync with GitHub
grc derive            # Propagate statuses
grc render            # Generate pages
cd site && npm run build  # Docusaurus production build
```

Or use the Makefile if available:
```bash
make pipeline         # sync → derive → render
make build            # Full build including Docusaurus
```

## Verification Checks

After building, verify:
1. **Control counts** — match source YAML
2. **Finding counts** — match audit YAML
3. **Framework coverage** — summary page totals match per-requirement pages
4. **No broken links** — Docusaurus build passes with `onBrokenLinks: 'throw'`
5. **Derived statuses** — controls reflect their findings correctly
