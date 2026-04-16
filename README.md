# go-grc

Governance, Risk & Compliance CLI for SirosID.

Replaces the Python compliance scripts (`sync_issues.py`, `generate_docs.py`) with a single Go binary that automates the full compliance lifecycle:

1. **Sync** GitHub issues with audit findings (create tracking issues, discover links, collect evidence)
2. **Derive** control and framework mapping statuses bottom-up from findings and evidence
3. **Render** Docusaurus compliance site pages
4. **Export** auditor-ready JSON evidence packages
5. **Status** read-only compliance overview

## Architecture

```
pkg/
  catalog/    Control catalog loader (YAML)
  audit/      Findings, evidence types, audit file loader/saver
  mapping/    EUDI, ISO 27001, GDPR mapping types and loader
  config/     Path resolution from --root flag
  github/     go-github wrapper for issue/PR operations

cmd/grc/
  main.go     CLI entrypoint (cobra)
  sync/       GitHub ↔ findings synchronisation
  derive/     Bottom-up status derivation
  render/     Docusaurus page generation
  export/     Auditor evidence package export
  status/     Read-only status overview
```

## Usage

```bash
# Build
make build

# Sync findings with GitHub issues
grc --root /path/to/compliance sync

# Derive statuses (dry-run first)
grc --root /path/to/compliance derive --dry-run
grc --root /path/to/compliance derive

# Render compliance site
grc --root /path/to/compliance render

# Export evidence package
grc --root /path/to/compliance export -o /tmp/audit-export

# Status overview
grc --root /path/to/compliance status
```

## Development

```bash
make setup    # Install tools
make test     # Run tests
make lint     # Run golangci-lint
make coverage # Coverage report
make build    # Build binary
```

## License

See repository root.
