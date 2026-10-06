---
name: grc-assess
description: 'Perform risk assessment on compliance findings. Use when: reclassifying severity, updating finding status, adding mitigations, triaging audit results, prioritizing remediation. Reads and updates audits/*.yaml and catalog controls.'
argument-hint: 'What to assess (e.g. "triage findings", "reclassify F-3 severity")'
---

# GRC Risk Assessment

Evaluate and update risk classifications for audit findings. Triage new findings,
reclassify severity after investigation, add mitigation plans, and update control statuses.

## When to Use

- Triaging findings after an audit
- Reclassifying severity after deeper investigation
- Adding or updating mitigation plans for open findings
- Updating finding status (open → in_progress → resolved → closed)
- Reviewing overall risk posture across audits

## Status Lifecycle

```
open → in_progress → resolved → closed
                  ↘ closed (won't fix / accepted risk)
```

| Status | Meaning |
|--------|---------|
| `open` | Finding confirmed, no remediation started |
| `in_progress` | Remediation underway (issue created, PR in progress) |
| `resolved` | Fix deployed, awaiting verification or next audit cycle |
| `closed` | Verified fixed, or risk formally accepted |

## Procedure

### 1. Review current findings

Read the audit YAML in `audits/*.yaml`. For each finding, check:
- Is the severity still accurate given current evidence?
- Has remediation progressed since last assessment?
- Are the linked issues/PRs still accurate?

### 2. Reclassify severity

When evidence changes the risk picture:
1. Update `severity` in the finding record
2. Add a note to `description` or `resolution` explaining the reclassification
3. Document the reasoning

### 2a. Risk register entries

If the project configures a risk methodology (`risk_register.methodology` in
`.grc.yaml`, served over MCP as `grc://risk/methodology`), that document is
**authoritative** for how risks are assessed and recorded. Before creating,
reviewing or editing anything under the risk register:

1. Read the methodology and follow it: required fields, how `severity` is
   derived (do not choose it freely), what each `status` means, who must
   approve, and the review cadence.
2. Never change the register's schema, vocabulary or assessment rules
   without making the matching change to the methodology document in the
   same change (update its version and history). Equally, a methodology
   change that alters what the register records must update the register.
3. If an existing entry does not conform, report the divergence and fix the
   entry, or propose the methodology change; do not leave them inconsistent.
4. Every risk needs an `owner`. Only the role the methodology names may
   accept a risk; do not record an acceptance on someone's behalf.
5. Every non-draft risk needs a `treatment_action` (`action`, `responsible`,
   `status`: open | in_progress | done, optional `due_date`). `completed_date`
   (YYYY-MM-DD) is required when `status` is `done` and must not be set
   otherwise. Do not mark an action done on someone's behalf.

### 3. Update statuses

When remediation progresses:
1. Update `status` field
2. Add evidence references if available
3. Link issues and PRs

### 4. Run derive

After updating findings:
```bash
grc derive     # Propagate changes to control and mapping statuses
grc render     # Regenerate compliance pages
```

The `derive` command automatically:
- Computes control statuses from findings (to_do → in_progress → verified, plus an evidenced flag)
- Propagates coverage to framework mappings (none → partial → full)
