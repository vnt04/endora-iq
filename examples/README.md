# ApiLens examples

Realistic sample inputs so you can try every command offline, against files that
live in this folder. Run the commands from the repository root after building the
binary (`go build -o apilens .` — see the top-level [README](../README.md)).

| File | Purpose |
|------|---------|
| [`collection.json`](collection.json) | Postman v2.1 collection: tested + untested endpoints, a bearer-auth collection, and headers carrying secrets (to exercise redaction) |
| [`openapi.yaml`](openapi.yaml) | The same service as an OpenAPI 3.0 spec |
| [`response.json`](response.json) | A sample `GET /users` response body, for `generate` |
| [`newman-report.json`](newman-report.json) | A Newman JSON run report with passing + failing assertions, for `explain` |
| [`users.csv`](users.csv) | A dataset (with one deliberately invalid row), for `test` |

## Which commands need an API key?

`generate`, `audit`, `review`, and `explain` call an LLM, so they need a provider
API key in the environment (e.g. `export ANTHROPIC_API_KEY=...`). `test`, `docs`,
`monitor`, and `config` are fully deterministic and run with **no key**.

## One-liner per command

```bash
# config — write a default apilens.yaml, then print the resolved config (key masked)
./apilens config init
./apilens config show

# generate — AI test scripts from a sample response (needs API key)
./apilens generate \
  --response examples/response.json \
  --endpoint "GET /users" \
  --status 200 \
  --output reports/generated-tests.js

# audit — deterministic coverage/quality scores + AI narrative (needs API key)
./apilens audit examples/collection.json --out-dir reports --format both
./apilens audit examples/openapi.yaml --format md          # OpenAPI input also works

# review — prioritized, actionable recommendations (needs API key)
./apilens review examples/collection.json --out reports/review.md

# explain — group Newman failures, predict root causes (needs API key)
./apilens explain examples/newman-report.json \
  --collection examples/collection.json \
  --out reports/failure-analysis.md

# test — run a collection against a dataset (no API key; needs Newman installed to execute)
./apilens test \
  --collection examples/collection.json \
  --data examples/users.csv \
  --out-dir reports --parallel 2 --delay 100

# docs — branded HTML docs via the OpenAPI -> Redoc pipeline (no API key)
./apilens docs examples/collection.json --out-dir docs --title "Demo User API"
./apilens docs examples/openapi.yaml --out-dir docs        # OpenAPI input also works

# monitor — scaffold a GitHub Actions monitoring workflow (no API key)
./apilens monitor init --out-dir . --mode hourly --notify slack,email
```

## Notes

- **Secret redaction:** `collection.json` ships with a real-looking `Authorization:
  Bearer ...` token, an `X-Api-Key`, and a `password` in the request body. ApiLens
  masks all of these (SRS §20) *before* any prompt is built — confirm by inspecting
  the generated `reports/*.json`/`*.md`, which never contain the raw secret.
- **External tools:** `test` shells out to [`newman`](https://github.com/postmanlabs/newman)
  and `docs` uses `npx postman-to-openapi` / `@redocly/cli` when available. When those
  are missing, `docs` still emits a valid `openapi.yaml` and a self-contained,
  CDN-backed `index.html` via the built-in fallback.
