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
   framework, dashboards, finding summaries, risk register, and year-cycle
   calendar.
4. **Serve** — run an HTTP server for the rendered site with optional GitHub
   webhook listener for automatic re-rendering on push.
5. **Export** — produce an auditor-ready JSON evidence package.
6. **Status** — print a read-only overview of current compliance posture.

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
risk-register/      # Accepted risks with compensating controls (YAML)
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
| FitCEM | Nordic EUDIW Certification System – Wallet Instance FitCEM PP | — |
| ISO 27001 | [ISO/IEC 27001:2022 Annex A](https://www.iso.org/standard/27001) | 93 |
| GDPR | [Regulation (EU) 2016/679](https://eur-lex.europa.eu/eli/reg/2016/679/oj) | 19 |
| OWASP ASVS | [OWASP ASVS 4.0.3 Level 3](https://owasp.org/www-project-application-security-verification-standard/) | — |
| STRIDE | Threat model mapping | — |

### Risk register

Accepted risks are tracked in YAML files under `risk-register/`. Each risk
links to a finding, documents compensating controls, residual severity, and
review intervals. The rendered site shows an overview with severity badges
and per-risk detail pages.

### Year-cycle calendar

Recurring GRC activities (reviews, audits, penetration tests) can be loaded
from an iCal (.ics) feed and projected onto a single canonical year. The
rendered page shows a circular SVG year diagram and a month-by-month schedule.

Controls can reference calendar activities by a stable lookup key derived
from the event title (e.g. `DPIA Review` → `dpia-review`) rather than
calendar UIDs that may change over time.

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

### Container image

```bash
docker pull ghcr.io/sirosfoundation/go-grc:latest

# Or build locally
make docker
```

## Usage

All commands take a `--root` (`-r`) flag pointing at the compliance
repository:

```bash
# Show current compliance posture
grc -r /path/to/compliance status

# Sync findings ↔ GitHub issues
grc -r /path/to/compliance sync

# Derive statuses (preview first, then apply)
grc -r /path/to/compliance derive --dry-run
grc -r /path/to/compliance derive

# Generate the Docusaurus compliance site (public or private)
grc -r /path/to/compliance render --profile public
grc -r /path/to/compliance render --profile private

# Serve the rendered site over HTTP
grc -r /path/to/compliance serve --profile private --addr :8080

# Serve with GitHub webhook for automatic re-rendering
grc -r /path/to/compliance serve --webhook --webhook-secret "$GRC_WEBHOOK_SECRET"

# Export evidence package for auditors
grc -r /path/to/compliance export -o /tmp/audit-export
```

### Docker usage

The Docker image includes Node.js and pnpm so that `grc serve` can run
Docusaurus builds inside the container.  On startup the server renders
markdown, runs `pnpm install` + `docusaurus build`, and then serves the
styled HTML site.

```bash
# Serve the compliance site (renders + builds on startup)
docker run -v /path/to/compliance:/data \
  -p 8080:8080 \
  ghcr.io/sirosfoundation/go-grc \
  serve -r /data --profile private

# With GitHub webhook and daily scheduled rebuilds
docker run -v /path/to/compliance:/data \
  -e GRC_WEBHOOK_SECRET=your-secret \
  -p 8080:8080 \
  ghcr.io/sirosfoundation/go-grc \
  serve -r /data --webhook --rebuild-interval 24h

# One-shot render (markdown only, no Docusaurus build)
docker run --rm -v /path/to/compliance:/data \
  ghcr.io/sirosfoundation/go-grc \
  render -r /data --profile public
```

### Docker Compose

Serve both public and private dashboards side by side.  A named volume for
`node_modules` caches npm dependencies across container restarts so only the
Docusaurus build (~20 s) runs instead of a full `pnpm install` (~2 min):

```yaml
# docker-compose.yml
services:
  public:
    image: ghcr.io/sirosfoundation/go-grc:latest
    volumes:
      - .:/data
      - public-site-cache:/data/site/node_modules
    command: ["serve", "-r", "/data", "--profile", "public", "--addr", ":8080"]
    ports:
      - "8080:8080"
    restart: unless-stopped

  private:
    image: ghcr.io/sirosfoundation/go-grc:latest
    volumes:
      - .:/data
      - private-site-cache:/data/site/node_modules
    command: ["serve", "-r", "/data", "--profile", "private", "--addr", ":8080"]
    ports:
      - "8081:8080"
    restart: unless-stopped

volumes:
  public-site-cache:
  private-site-cache:
```

```bash
docker compose up -d
# Public dashboard:  http://localhost:8080
# Private dashboard: http://localhost:8081
```

To enable automatic re-rendering on GitHub push, add a webhook service:

```yaml
  webhook:
    image: ghcr.io/sirosfoundation/go-grc:latest
    volumes:
      - .:/data
      - webhook-site-cache:/data/site/node_modules
    command: ["serve", "-r", "/data", "--profile", "private", "--addr", ":8080", "--webhook", "--rebuild-interval", "24h"]
    ports:
      - "8082:8080"
    environment:
      - GRC_WEBHOOK_SECRET=${GRC_WEBHOOK_SECRET}
    restart: unless-stopped
```

### CI pipeline

In a compliance repository with a Makefile wired to `grc`, the typical CI
pipeline is:

```bash
make pipeline       # sync → derive → render
make build          # render + docusaurus build
```

## Configuration

Project settings are stored in `.grc.yaml` at the repository root:

```yaml
project:
  name: "My Compliance Dashboard"
  repo: "myorg/compliance"
  url: "https://compliance.example.com"

catalog:
  dir: catalog
  subdirs: [technical, organizational]

mappings:
  dir: mappings

audits:
  dir: audits

site:
  dir: site/docs

frameworks:
  - id: iso27001
    name: "ISO 27001 Annex A"
    catalog_file: iso27001-annexa.yaml
    mapping_file: iso27001-annexa.yaml
    key_field: annex_a

risk_register:
  dir: risk-register
  files: [platform.yaml]

year_cycle:
  title: "Compliance Year Cycle"
  source: "https://calendar.google.com/calendar/ical/.../basic.ics"
  public: false
```

## Architecture

```
cmd/grc/
  main.go           CLI entrypoint (cobra)
  sync/             GitHub ↔ findings synchronisation
  derive/           Bottom-up status derivation
  render/           Docusaurus page generation (controls, frameworks,
                    risk register, year-cycle, CSF overview, findings)
  serve/            HTTP server with GitHub webhook support
  export/           Evidence package export
  status/           Read-only status overview
  validate/         Config and data validation
  initialize/       Scaffold a new compliance repository
  oscal/            OSCAL SSP export
  risk/             Risk register CLI operations

pkg/
  catalog/          Control catalog + framework catalog loaders (YAML)
  audit/            Finding & evidence types, audit file loader/saver
  mapping/          Framework mapping types and loader
  config/           Project configuration (.grc.yaml) and path resolution
  github/           go-github wrapper for issue/PR operations
  risk/             Risk register types and loader
  yearcycle/        iCal parser + year-cycle projection
```

## Development

```bash
make setup          # Install golangci-lint, goimports, gosec, staticcheck
make test           # Run tests with -race
make lint           # Run golangci-lint
make coverage       # Generate coverage report
make fmt            # gofmt + goimports
make vet            # go vet
make docker         # Build Docker image
```

[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/sirosfoundation/go-grc/badge)](https://scorecard.dev/viewer/?uri=github.com/sirosfoundation/go-grc)
## License

BSD 2-Clause — see [LICENSE](LICENSE).
