# go-grc

Governance, Risk & Compliance (GRC) CLI for managing security compliance as
code.  Designed for the [SirosID](https://siros.org) wallet platform but
applicable to any project that tracks controls, audit findings, and framework
mappings in YAML.

`grc` automates the full compliance lifecycle:

1. **Sync** — create and update GitHub issues for each audit finding; discover
   linked PRs and collect evidence.
2. **Derive** — compute control and mapping statuses bottom-up from findings
   and evidence (validated → verified → planned → to-do).
3. **Render** — generate a complete [Docusaurus](https://docusaurus.io/)
   compliance site with per-control pages, per-requirement pages for each
   framework, dashboards, and finding summaries.
4. **Export** — produce an auditor-ready JSON evidence package.
5. **Status** — print a read-only overview of current compliance posture.

## Data model

`grc` works on a compliance repository with the following layout:

```
catalog/
  technical/        # Platform security controls (YAML)
  organizational/   # Operator controls (YAML)
  frameworks/       # Normative requirement text per framework
mappings/           # Framework → control cross-references (YAML)
  eudi-secreq.yaml  #   EUDI Wallet Security Requirements
  iso27001-annexa.yaml  #   ISO/IEC 27001:2022 Annex A
  gdpr.yaml         #   GDPR checklist
audits/             # Structured audit findings with evidence (YAML)
```

Each control has an ID (e.g. `SID-AUTH-01`), a status (`verified`,
`validated`, `planned`, `to_do`), and references to source code, PRs,
deployed endpoints, or external reports that serve as evidence.

### Framework mappings

Mapping files cross-reference external requirements to internal controls.
Supported frameworks:

| Framework | Standard | Requirements |
|-----------|----------|-------------|
| EUDI | [ENISA Wallet Security Requirements v0.5](https://www.enisa.europa.eu/publications/eudi-wallet-certification) | 85 |
| ISO 27001 | [ISO/IEC 27001:2022 Annex A](https://www.iso.org/standard/27001) | 93 |
| GDPR | [Regulation (EU) 2016/679](https://eur-lex.europa.eu/eli/reg/2016/679/oj) | 19 |

### Status derivation

The `derive` command computes statuses bottom-up:

- **Finding → Control**: a control with open findings is downgraded; a control
  whose findings are all resolved with evidence is upgraded to `validated`.
- **Control → Mapping**: a mapping requirement whose controls are all verified
  or validated is marked `compliant`; partial coverage yields `partial`.

## Installation

```bash
go install github.com/sirosfoundation/go-grc/cmd/grc@latest
```

Or build from source:

```bash
git clone https://github.com/sirosfoundation/go-grc.git
cd go-grc
make build          # → bin/grc
make install        # → $GOBIN/grc
```

Container image:

```bash
docker build -t grc .
docker run --rm -v "$PWD":/data grc --root /data status
```

## Usage

All commands take a `--root` flag pointing at the compliance repository:

```bash
# Show current compliance posture
grc --root /path/to/compliance status

# Sync findings ↔ GitHub issues
grc --root /path/to/compliance sync

# Derive statuses (preview first, then apply)
grc --root /path/to/compliance derive --dry-run
grc --root /path/to/compliance derive

# Generate the Docusaurus compliance site
grc --root /path/to/compliance render

# Export evidence package for auditors
grc --root /path/to/compliance export -o /tmp/audit-export
```

In a compliance repository with a Makefile wired to `grc`, the typical CI
pipeline is:

```bash
make pipeline       # sync → derive → render
make build          # render + docusaurus build
```

## Architecture

```
cmd/grc/
  main.go           CLI entrypoint (cobra)
  sync/             GitHub ↔ findings synchronisation
  derive/           Bottom-up status derivation
  render/           Docusaurus page generation
  export/           Evidence package export
  status/           Read-only status overview

pkg/
  catalog/          Control catalog + framework catalog loaders (YAML)
  audit/            Finding & evidence types, audit file loader/saver
  mapping/          EUDI, ISO 27001, GDPR mapping types and loader
  config/           Path resolution from --root flag
  github/           go-github wrapper for issue/PR operations
```

## Development

```bash
make setup          # Install golangci-lint, goimports, gosec, staticcheck
make test           # Run tests with -race
make lint           # Run golangci-lint
make coverage       # Generate coverage report
make fmt            # gofmt + goimports
make vet            # go vet
```

## License

BSD 2-Clause — see [LICENSE](LICENSE).
