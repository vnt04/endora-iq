# ApiLens

**AI-Powered API Quality Platform for Developers.**

ApiLens is a CLI-first AI quality layer that sits *on top of* the API tooling you
already use — Postman, Newman, OpenAPI/Swagger, Bruno, Hoppscotch — and helps you
**generate tests, audit coverage, review collections, explain failures, build
docs, and scaffold monitoring**. It is a single Go binary with no runtime services.

---

## What ApiLens is (and isn't)

**ApiLens is** the AI quality layer above your existing API tools. Its job is to
raise API test coverage and quality:

- automatically generate test scripts,
- find gaps and weak spots in a test suite,
- analyze the root cause of test failures,
- generate branded API documentation,
- scaffold free monitoring.

**ApiLens is _not_** a Postman alternative. Per the product principles (SRS §2):

1. **Do not reimplement mature tools.** Execution stays with Newman; docs rendering
   stays with Redoc; conversion stays with `postman-to-openapi`; scheduling stays
   with GitHub Actions.
2. **AI features create the main value.** The deterministic engines score; the AI
   narrates and recommends.
3. **CLI-first.** No GUI, no API client, no request editor, no collection editor,
   no gateway, no custom scheduler.
4. **Works with your existing workflow** and ships as a **single binary**.

---

## How it works

ApiLens deliberately separates *deterministic measurement* from *AI narrative*:

1. **Parse** — a Postman v2.1 collection, an OpenAPI 3.x / Swagger 2.0 spec, or a
   Newman JSON report is normalized into one internal domain model.
2. **Score (deterministic, no AI)** — the `coverage` and `scoring` engines compute
   coverage % (Authentication, Error, Validation, Security, Performance, Edge Case)
   and a weighted 0–100 quality score with an A–F grade. **Identical input always
   yields identical scores** — no model variance.
3. **Redact (security, SRS §20)** — *before any data is sent to an LLM*, every
   prompt is built from a redacted deep-copy. `Authorization` / `Bearer` / `Basic`
   credentials, API keys, JWTs, cookies, and secret-bearing body/query values are
   masked with `***REDACTED***`. Secrets never leave the process.
4. **Narrate (AI)** — the LLM turns the redacted, already-scored picture into prose:
   gaps, risks, prioritized recommendations, root-cause analysis.

So: **scores are math you can trust and reproduce; the AI explains them.**

---

## Requirements

- **Go 1.25 or newer is required.** ApiLens uses
  `github.com/charmbracelet/bubbles` (Bubble Tea spinners for long AI calls), whose
  current `@latest` releases require Go **1.25+**. The module therefore declares
  `go 1.25`. (The SRS nominally says "Go >= 1.24", but the latest bubbles/x deps are
  not satisfiable on 1.24 — see `docs/CONTRACT.md`.)
- For full functionality of the non-AI features, optionally install:
  - [`newman`](https://github.com/postmanlabs/newman) — used by `apilens test`.
  - `npx` + [`postman-to-openapi`](https://www.npmjs.com/package/postman-to-openapi)
    and [`@redocly/cli`](https://www.npmjs.com/package/@redocly/cli) — used by
    `apilens docs` (with built-in fallbacks when absent).

---

## Install

Install the latest release straight from source:

```bash
go install github.com/apilens/apilens@latest
```

This installs an `apilens` binary into `$(go env GOBIN)` (or `$(go env GOPATH)/bin`).
Make sure that directory is on your `PATH`.

Or build a local binary from a clone:

```bash
git clone https://github.com/apilens/apilens.git
cd apilens
go build -o apilens .       # produces ./apilens
./apilens version

# Or install every main package from the working tree:
go install ./...
```

A `Makefile` wraps the common tasks:

```bash
make build      # go build -o apilens .
make test       # full test suite with coverage (scripts/test.sh)
make vet        # build + go vet (scripts/check.sh)
make cover      # coverage.out + total %
make run ARGS="audit examples/collection.json"
make help       # list all targets
```

---

## Configuration

ApiLens reads an optional `apilens.yaml` from the current directory (override with
`--config`). Generate one with `apilens config init`:

```yaml
provider: anthropic       # anthropic | openai | gemini | openrouter
model: claude-sonnet-4
report_dir: reports
docs_dir: docs
monitor_dir: monitor
temperature: 0.1
max_tokens: 4000
# timeout: 120s           # optional
# base_url: ""            # optional provider base-URL override
```

`APILENS_`-prefixed environment variables override file values (e.g.
`APILENS_PROVIDER=openai`), and the `--provider` / `--model` flags override both.

### API keys (never stored in the file)

The provider API key is **read only from the environment** — never from
`apilens.yaml` and never sent to an LLM as data. Set the variable that matches your
provider:

| Provider     | Environment variable   |
|--------------|------------------------|
| `anthropic`  | `ANTHROPIC_API_KEY`    |
| `openai`     | `OPENAI_API_KEY`       |
| `gemini`     | `GEMINI_API_KEY`       |
| `openrouter` | `OPENROUTER_API_KEY`   |

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
apilens config show     # prints the resolved config; the key is masked
```

`generate`, `audit`, `review`, and `explain` need a key. `test`, `docs`, `monitor`,
and `config` are fully deterministic and need **no** key.

---

## Commands

Global flags (all commands): `-c/--config`, `-p/--provider`, `-m/--model`,
`-v/--verbose`. Sample inputs for every example below live in
[`examples/`](examples/).

### `apilens generate` — AI test generator (SRS §9)

Generate Postman/Newman-compatible test scripts from a sample response, expected
status, and endpoint metadata.

```bash
apilens generate \
  --response examples/response.json \
  --endpoint "GET /users" \
  --status 200 \
  --output reports/generated-tests.js
```

Covers status, response-time, required-field, schema, type, format, null, nested,
and array validation plus edge-case suggestions. Output runs in the Postman Tests
tab and under Newman.

### `apilens audit` — AI collection auditor (SRS §10)

Audit a Postman collection or OpenAPI spec: deterministic coverage + quality scores
plus an AI narrative of gaps and risk. Writes `audit-report.md` / `audit-report.json`.

```bash
apilens audit examples/collection.json --out-dir reports --format both
apilens audit examples/openapi.yaml --format md
```

`--format` is `md`, `json`, or `both`. Coverage checks include `401/403/404/422/429/500`,
invalid payload, missing fields, empty body, boundary values, large payload,
authorization failure, expired token, duplicate data, invalid format, missing headers.

### `apilens review` — AI collection review (SRS §11)

Produce prioritized, actionable recommendations: weak/untested endpoints, missing
scenarios, and an estimate of production risk. Writes `review.md`.

```bash
apilens review examples/collection.json --out reports/review.md
```

### `apilens explain` — AI failure analyzer (SRS §12)

Analyze a Newman JSON report: group failures, predict root causes with a confidence
score, and recommend fixes. Optionally correlate with the source collection. Writes
`failure-analysis.md`.

```bash
apilens explain examples/newman-report.json \
  --collection examples/collection.json \
  --out reports/failure-analysis.md
```

### `apilens test` — data-driven testing (SRS §14)

Run a collection against a CSV / JSON / JSONL dataset, with optional parallelism,
bail-on-failure, and inter-request delay. No AI. (Requires `newman` on `PATH` to
execute requests.)

```bash
apilens test \
  --collection examples/collection.json \
  --data examples/users.csv \
  --out-dir reports \
  --parallel 2 --delay 100         # add --bail to stop on first failure
```

### `apilens docs` — documentation generator (SRS §13)

Convert a collection/spec into branded HTML via the `Collection → OpenAPI → Redoc`
pipeline. Writes `docs/openapi.yaml` and `docs/index.html`. No AI.

```bash
apilens docs examples/collection.json \
  --out-dir docs --title "Demo User API" --theme "#1976d2"
apilens docs examples/openapi.yaml --out-dir docs   # --deploy adds a Pages workflow
```

When `npx`/`postman-to-openapi`/`@redocly/cli` are unavailable, ApiLens falls back
to a built-in OpenAPI emitter and a self-contained, CDN-backed Redoc page — always
producing valid output.

### `apilens monitor init` — monitoring generator (SRS §15)

Scaffold a free GitHub Actions monitoring workflow plus `reports/` and `alerts/`
folders. No AI.

```bash
apilens monitor init --out-dir . --mode hourly --notify slack,email
```

`--mode` is `15min | 30min | hourly | daily`; `--notify` accepts any of
`email,slack,discord,telegram`. The generated `alerts/config.yaml` documents the
repository secrets each channel needs.

### `apilens config` — configuration

```bash
apilens config init     # write a default apilens.yaml (errors if one exists)
apilens config show     # print the resolved config (API key masked)
```

`apilens version` prints the build version.

---

## Project structure

```
api-lens/
├── main.go               # entry point; registers AI providers via blank imports
├── cmd/                  # cobra command tree (one file per command)
├── internal/
│   ├── core/             # frozen domain model, errors, config, ai client, redact
│   │   ├── ai/           #   high-level Generate/Analyze/Review/Explain client
│   │   ├── config/       #   Config load/defaults/env resolution
│   │   └── redact/       #   secret masking (SRS §20) — runs before every prompt
│   ├── providers/        # provider seam + anthropic/openai/gemini/openrouter
│   ├── parser/           # postman / openapi / newman / dataset parsers
│   ├── coverage/         # deterministic coverage engine (no AI)
│   ├── scoring/          # deterministic quality-score engine (no AI)
│   ├── generate/         # Feature A — test generator
│   ├── audit/            # Feature B — collection auditor
│   ├── review/           # Feature C — collection review
│   ├── explain/          # Feature D — failure analyzer
│   ├── docs/             # Feature E — documentation generator
│   ├── testing/          # Feature F — data-driven testing (package `datatest`)
│   └── monitor/          # Feature G — monitoring generator
├── pkg/
│   ├── logging/          # zerolog setup
│   └── tui/              # Bubble Tea spinner for long AI calls
├── configs/              # sample apilens.yaml
├── examples/             # offline sample inputs (see examples/README.md)
├── templates/            # reference copies of embedded templates
├── scripts/              # dev harness (go-env.sh, check.sh, test.sh)
├── docs/                 # CONTRACT.md (frozen type contract) + generated docs
└── SRS.md                # full software requirements specification
```

The frozen type contract every package builds against is
[`docs/CONTRACT.md`](docs/CONTRACT.md).

---

## Development

Go lives in WSL in this repo's environment. The `scripts/` directory is the dev
harness:

| Script | Purpose |
|--------|---------|
| `scripts/go-env.sh` | Sets the canonical Go environment (WSL-native caches, pins `GOTOOLCHAIN=local`). `source` it before running raw `go` commands. |
| `scripts/check.sh`  | `go build ./...` + `go vet ./...`; non-zero on failure. |
| `scripts/test.sh`   | Full test suite with a coverage summary. |

```bash
# from a POSIX shell (WSL/Linux/macOS)
source scripts/go-env.sh        # once per shell
go build ./...

# or via the wrappers / Makefile
./scripts/check.sh
./scripts/test.sh
make test
```

CI for the project itself runs build + vet + race-enabled tests with coverage on Go
1.25.x — see [`.github/workflows/ci.yml`](.github/workflows/ci.yml).

Contributions are welcome. Please keep the deterministic engines AI-free, always
redact before prompting, add table-driven tests next to new code, and never send
secrets to an LLM.

---

## License

[MIT](LICENSE) © 2026 ApiLens contributors.
