---
name: grc-sync
description: 'Synchronize findings with GitHub issues and PRs. Use when: creating tracking issues, linking PRs to findings, syncing issue state, refreshing audit data from repos. Uses grc sync CLI command.'
argument-hint: 'What to sync (e.g. "sync all open findings", "link issue #42 to F-3")'
---

# GRC GitHub Sync

Synchronize finding status in `audits/*.yaml` with the current state of linked
GitHub issues and pull requests.

## When to Use

- After issues are closed or PRs are merged
- Creating tracking issues for new findings
- Linking existing issues/PRs to findings
- Periodic refresh to catch status changes

## CLI Commands

### Full sync
```bash
grc sync [--repo OWNER/REPO] [--dry-run]
```

Phases:
1. Create tracking issues for findings without one
2. Discover linked issues/PRs from tracking issue comments
3. Sync issue state (closed issues → resolved findings)
4. Collect evidence from closed issues and merged PRs

### Link a finding to an issue or PR
```bash
grc sync link FINDING_ID OWNER/REPO#NUMBER [--pr]
```

Links an existing GitHub issue or PR to a finding. Use `--pr` when linking
a pull request instead of an issue.

## Environment

Requires `GITHUB_TOKEN` environment variable with repo access.

## Procedure

### 1. Check current sync status

```bash
grc status
```

Review which findings have tracking issues and which don't.

### 2. Run sync

```bash
grc sync --dry-run    # Preview changes first
grc sync              # Apply changes
```

### 3. Verify results

```bash
grc status            # Check updated counts
grc derive            # Propagate status changes
grc render            # Regenerate site
```
