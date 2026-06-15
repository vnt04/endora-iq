export const meta = {
  name: 'apilens-features',
  description: 'Implement all 8 ApiLens features + scoring/coverage engines against the frozen contract, then integrate, test, fill coverage, polish, and review',
  phases: [
    { title: 'Features', detail: '9 parallel: coverage, scoring, generate, audit, review, explain, docs, datatest, monitor' },
    { title: 'Integrate', detail: 'go mod tidy + build + vet until green' },
    { title: 'Test', detail: 'go test ./... green; fix failures' },
    { title: 'Coverage', detail: 'fill unit-test gaps toward 80%' },
    { title: 'Polish', detail: 'README, examples, templates, CI, Makefile, LICENSE' },
    { title: 'Review', detail: 'security + Go review (parallel), then fix criticals' },
  ],
}

const BUILD = "export MSYS_NO_PATHCONV=1; wsl.exe -d Ubuntu -- bash /home/ubuntu/workspace/2026_projects/api-lens/scripts/check.sh"
const TEST = "export MSYS_NO_PATHCONV=1; wsl.exe -d Ubuntu -- bash /home/ubuntu/workspace/2026_projects/api-lens/scripts/test.sh"
const GORUN = "export MSYS_NO_PATHCONV=1; wsl.exe -d Ubuntu -- bash -c 'source /home/ubuntu/workspace/2026_projects/api-lens/scripts/go-env.sh && cd /home/ubuntu/workspace/2026_projects/api-lens && go GO_ARGS'"

const BRIEF = `
# ApiLens — Feature Implementation Brief

ApiLens is a CLI-first "AI quality layer" for API testing (full spec in SRS.md at repo root). The FOUNDATION + SKELETON are already built and the whole module compiles (go build ./... is green). Your job is to fill in real behavior.

## THE FROZEN CONTRACT
docs/CONTRACT.md is authoritative. READ IT before writing. Use the EXACT types/signatures there. Do NOT redefine core types or change any exported signature. If you believe a signature must change, prefer adapting your code; only as a last resort note it loudly in your return — a later integrate phase reconciles.

## Environment
- File tools (Read/Write/Edit/Glob/Grep): use paths relative to the repo root (current working dir, a \\\\wsl.localhost\\Ubuntu\\... share).
- Repo root (Linux, for builds): /home/ubuntu/workspace/2026_projects/api-lens
- Go 1.26.4 lives in WSL only.

## Key APIs you will use (verify exact names in CONTRACT.md / source before calling)
- parser.Parse(path) (*core.Collection, error)        // auto-detect postman|openapi
- parser.ParseNewman(path) (*core.NewmanReport, error)
- parser.ParsePostman / parser.ParseOpenAPI / parser.LoadDataset(path) ([]map[string]string, error) / parser.DetectFormat
- redact.Collection(c) / redact.Endpoint(e) / redact.String(s)  // SECURITY: redact BEFORE any AI prompt
- ai.Client.Generate/Analyze/Review/Explain(ctx, system, user string) (string, error)
- coverage.Analyze(c *core.Collection) coverage.Result
- scoring.Compute(s scoring.Subscores) scoring.Score
- pkg/tui.RunWithSpinner(ctx, label, func(ctx) error) error  // optional, wrap long AI calls
- core domain model: core.Collection{Name,Description,Source,BaseURL,Endpoints[]}, core.Endpoint{Method,Path,Name,Headers,QueryParams,PathParams,HasAuth,AuthType,RequestBody,TestScripts[],Responses[],Tags[],Description}, core.NewmanReport / Execution / Assertion.

## SECURITY (SRS §20) — non-negotiable
Never send secrets to an LLM. Build every prompt from redact.Collection(...)/redact.Endpoint(...)/redact.String(...). Never hardcode API keys. For any os/exec call, never pass untrusted strings to a shell — exec the binary directly with arg slices (no "sh -c"), to avoid command injection.

## STYLE
Idiomatic Go: small focused files (<450 lines), explicit error wrapping (fmt.Errorf("ctx: %w", err)), context-aware, no panics in library code. Write table-driven unit tests for your package in the same dir (_test.go). For AI-dependent code, inject ai.NewWithProvider(fake.New("..."), cfg) using internal/providers/fake — tests MUST NOT hit the network. Use t.TempDir() for file outputs.

## HARD RULE for the Features phase
Do NOT run any 'go' command (build/test/mod) — many agents run in parallel and would race on go.mod/go.sum and the build cache. ONLY write/edit .go files. The Integrate phase compiles; the Test phase runs tests. (A PostToolUse hook may warn "N files modified this session" — that is expected in this multi-agent build; ignore it.)

## Output-file conventions
Commands write artifacts to the user's chosen dir (from Options). In code, create parent dirs with os.MkdirAll. Never write outside the requested dir.
`

phase('Features')

const featureResults = await parallel([
  // 1. COVERAGE ENGINE (deterministic, AI-free) — heart of audit/review
  () => agent(BRIEF + `
# TASK: Implement the COVERAGE ENGINE — internal/coverage/coverage.go (SRS §10). DETERMINISTIC, NO AI.

Replace the stub func Analyze(c *core.Collection) Result with a real, deterministic, well-documented heuristic analyzer + unit tests.

Approach (make it stable & testable):
- Define the six SRS §10 categories: "Authentication", "Error", "Validation", "Security", "Performance", "Edge Case".
- For each category define a fixed checklist of signals drawn from SRS §10 coverage checks: status codes {401,403,404,422,429,500} and concepts {invalid payload, missing fields, empty body, boundary values, large payload, authorization failure, expired token, duplicate data, invalid format, missing headers}, plus validation signals (schema validation, type validation, format validation, null checks, required fields, response-time assertion).
- Build a lowercased corpus per collection from every endpoint's TestScripts (joined), plus declared/example Responses[].Status, plus presence of auth, plus request bodies. Detect each check via keyword/code matching (e.g. status 401 found if any TestScript mentions "401"/"unauthorized" or any Response.Status==401; response-time found if scripts mention "responsetime"/"response time"/"less than" + "ms"; schema via "jsonschema"/"schema"/"to.have.property"; etc.). Document each detector.
- CategoryScore.Score = round(100 * found/total) for that category; Found/Missing list the human-readable check names.
- Result.Overall = rounded average of category scores (document if weighted). Result.MissingChecks = union of missing checks across categories (deduped, human-readable, e.g. "401 Unauthorized","429 Rate Limit","Empty Payload"). Result.RiskLevel = "High" if Overall<50, "Medium" if <75, else "Low".
- Handle nil/empty collection (Overall 0, RiskLevel "High", all checks missing).
- Make detection a small table of {checkName, category, matchFunc} so it is easy to test and extend. Keep pure (no I/O, no globals).

Write internal/coverage/coverage_test.go: table-driven tests covering — empty collection; a collection with rich test scripts (high score); a collection with only happy-path GETs (low auth/error coverage); verifying specific MissingChecks appear and RiskLevel thresholds. Aim >85% coverage of this file.

Return: the category checklist you used, the scoring formula, and risk thresholds.`,
    { label: 'coverage-engine', phase: 'Features' }),

  // 2. SCORING ENGINE (deterministic)
  () => agent(BRIEF + `
# TASK: Implement the QUALITY SCORE ENGINE — internal/scoring/scoring.go (SRS §16). DETERMINISTIC, NO AI.

Replace stub func Compute(s Subscores) Score with a weighted aggregate + grade + tests.
- Subscores fields (0-100 each): Coverage, Validation, Security, ErrorHandling, Documentation.
- Define documented weights summing to 1.0 (suggest: Coverage 0.30, Validation 0.20, Security 0.20, ErrorHandling 0.20, Documentation 0.10). Score.Value = round(weighted sum), clamped 0-100.
- Clamp each incoming subscore to [0,100] defensively.
- Grade bands: A>=90, B>=80, C>=70, D>=60, else F. Expose the weights as documented exported consts or a documented var so tests/readers see them.
- Score.Subscores echoes the (clamped) inputs.

Write internal/scoring/scoring_test.go: table-driven — all-100 => 100/A; all-0 => 0/F; mixed values verifying exact weighted rounding; out-of-range inputs clamped; each grade boundary (89/90, 79/80, etc.). Aim >90% coverage.

Return: the exact weights and grade bands used.`,
    { label: 'scoring-engine', phase: 'Features' }),

  // 3. GENERATE (Feature A)
  () => agent(BRIEF + `
# TASK: Implement FEATURE A — AI Test Generator: internal/generate/generate.go (SRS §9).

func Run(ctx, cfg, client *ai.Client, opts Options) error  // opts: ResponsePath, Endpoint ("GET /users"), Status (e.g. 200), OutputPath (default generated-tests.js)
Behavior:
1. Read the response JSON file at opts.ResponsePath (validate exists; it is an EXAMPLE API response body). Pretty/normalize it. REDACT it with redact.String before putting it in a prompt (it may contain tokens/cookies).
2. Parse opts.Endpoint into method+path (e.g. "GET /users"); default status 200 if 0.
3. Build a strong system prompt: "You are an expert API test engineer. Output ONLY JavaScript for the Postman Tests tab (also Newman-compatible). Use pm.test/pm.expect/pm.response. No markdown, no prose." And a user prompt embedding endpoint, expected status, and the (redacted) example response, requesting: status validation, response-time validation, required-fields, schema validation, type validation, format validation (email/uuid/date), null checks, nested-object validation, array validation, and a block of // edge-case suggestions as comments.
4. Call client.Generate(ctx, system, user) (wrap in tui.RunWithSpinner). Strip any markdown code fences (\`\`\`js ... \`\`\`) from the result. Ensure it begins with a header comment. Write to opts.OutputPath via os.WriteFile (MkdirAll parent). Log where it was written.
5. If client is nil or API key missing, return a clear error (the cmd layer guarantees a client for generate, but be defensive).

Tests (generate_test.go): use ai.NewWithProvider(fake.New("pm.test('status is 200', function(){ pm.response.to.have.status(200); });"), cfg). Verify: fence stripping (feed fake a fenced response, assert output has no fences), output file written to t.TempDir(), endpoint parsing ("GET /users" -> method GET path /users; "POST /a/b"), error when ResponsePath missing. Add a helper to parse endpoint that is unit-tested directly. Aim >80%.

Return: prompt design summary + how you strip fences + endpoint-parse rules.`,
    { label: 'feat-generate', phase: 'Features' }),

  // 4. AUDIT (Feature B) — uses coverage + scoring + AI narrative
  () => agent(BRIEF + `
# TASK: Implement FEATURE B — AI Collection Auditor: internal/audit/audit.go (SRS §10). Produces audit-report.md + audit-report.json.

func Run(ctx, cfg, client *ai.Client, opts Options) error // opts: InputPath, OutDir, Format ("md"|"json"|"both")
Behavior:
1. parser.Parse(opts.InputPath) -> *core.Collection (postman or openapi).
2. cov := coverage.Analyze(collection). Derive scoring.Subscores from the coverage Result categories (document the mapping: e.g. Coverage<-Overall or Edge/Error avg; Validation<-Validation category; Security<-Security category; ErrorHandling<-Error category; Documentation<- a deterministic measure of how many endpoints have non-empty Description). score := scoring.Compute(subscores).
3. AI narrative: build a REDACTED, compact summary of the collection (endpoint list with method/path/auth/has-tests, plus the computed coverage gaps) using redact.Collection. Call client.Analyze(ctx, system, user) to get a qualitative risk analysis + prioritized recommendations. If client==nil or no API key, SKIP the narrative gracefully (still emit scores) and note "AI narrative unavailable (no API key)". Wrap AI call in tui spinner.
4. Write outputs into opts.OutDir (MkdirAll):
   - audit-report.json: a stable struct {collection name, source, endpoint_count, coverage (the Result), quality (the Score), missing_checks, risk_level, ai_analysis (string, maybe empty), generated_by}. Marshal indented.
   - audit-report.md: render like SRS §10 example — "# ApiLens Audit Report", Coverage Score, Quality Score (+grade), per-category table, a "Missing:" bullet list, "Risk Level:", then the AI analysis section. Clean Markdown.
   - Honor Format: "md" only md, "json" only json, "both" (default) both. Default OutDir to cfg.ReportDir when empty.
5. Print a concise terminal summary (scores + risk).

Tests (audit_test.go): write a small Postman collection JSON to t.TempDir(), run with ai.NewWithProvider(fake.New("## Analysis\\nLooks risky."), cfg) and Format "both"; assert both files exist, JSON parses and contains the scores, MD contains "Coverage Score". Also test Format "json" only. Also a nil-client path producing files with the unavailable note. Aim >75%.

Return: the coverage->subscores mapping, the JSON schema of audit-report.json, and the MD layout.`,
    { label: 'feat-audit', phase: 'Features' }),

  // 5. REVIEW (Feature C)
  () => agent(BRIEF + `
# TASK: Implement FEATURE C — AI Collection Review: internal/review/review.go (SRS §11). Produces review.md.

func Run(ctx, cfg, client *ai.Client, opts Options) error // opts: InputPath, OutPath (default review.md)
Behavior:
1. parser.Parse(InputPath) -> collection. Run coverage.Analyze for signal.
2. Deterministically identify: untested endpoints (len(TestScripts)==0), weak endpoints (have tests but missing negative/auth/validation coverage — infer from coverage missing checks + per-endpoint auth/test presence), and a production-risk estimate (reuse coverage RiskLevel + counts).
3. Build a REDACTED summary (redact.Collection) listing endpoints with method/path/auth/test-count and the computed weaknesses. Call client.Review(ctx, system, user) to produce actionable recommendations: weak endpoints, untested endpoints, missing scenarios, improvements, production risk. System prompt: "You are a senior API QA reviewer. Be specific and actionable." Wrap in tui spinner.
4. Write review.md (OutPath, default review.md, MkdirAll) in the SRS §11 shape: "# ApiLens Review", a deterministic "High Risk / Untested Endpoints" section (computed), then the AI "Recommendations" section. If no client/API key: still write the deterministic sections + note AI recommendations unavailable.
5. Terminal summary (counts + risk).

Tests (review_test.go): postman collection in t.TempDir() with one endpoint having tests and one without; fake provider; assert review.md exists and lists the untested endpoint; assert deterministic weakness detection (unit-test the helper that classifies endpoints). nil-client path. Aim >75%.

Return: how you classify untested vs weak endpoints, and the md layout.`,
    { label: 'feat-review', phase: 'Features' }),

  // 6. EXPLAIN (Feature D)
  () => agent(BRIEF + `
# TASK: Implement FEATURE D — AI Failure Analyzer: internal/explain/explain.go (SRS §12). Produces failure-analysis.md.

func Run(ctx, cfg, client *ai.Client, opts Options) error // opts: ReportPath (Newman JSON), CollectionPath (optional), OutPath (default failure-analysis.md)
Behavior:
1. parser.ParseNewman(ReportPath) -> *core.NewmanReport. (If a .html report is given, attempt to read; if only HTML, note JSON is preferred — but support JSON fully.) Optionally parser.Parse(CollectionPath) for correlation context.
2. Deterministically: collect failed assertions from report.Executions, group related failures (by endpoint name/URL and by similar error message — e.g. normalize messages, cluster by ErrorName + a simplified message signature). Produce groups with counts.
3. For each group (or the whole set, your call — keep prompt sizes reasonable; cap groups), build a REDACTED prompt (redact.String on messages/urls) and call client.Explain(ctx, system, user) asking for: predicted root cause, a confidence percentage, supporting evidence, and suggested fix actions — in the SRS §12 shape. System: "You are an expert at diagnosing API test failures." Wrap in spinner.
4. Write failure-analysis.md (OutPath default failure-analysis.md): summary (total/passed/failed), then per-group: Root Cause, Confidence, Evidence, Suggested Actions. If zero failures: write a clean "All tests passed" report. If no client/API key: emit the deterministic grouping + note AI analysis unavailable.
5. Terminal summary.

Tests (explain_test.go): write a newman JSON report (with at least 2 failed assertions across endpoints) to t.TempDir(); fake provider returning a canned analysis; assert failure-analysis.md exists, mentions failure count, and includes the canned root cause. Unit-test the grouping/clustering helper directly (same ErrorName clusters together). Test the all-passed path. nil-client path. Aim >75%.

Return: the grouping/clustering heuristic and md layout.`,
    { label: 'feat-explain', phase: 'Features' }),

  // 7. DOCS (Feature E) — pipeline, shells out, graceful fallback
  () => agent(BRIEF + `
# TASK: Implement FEATURE E — Documentation Generator: internal/docs/docs.go (SRS §13). Produces docs/openapi.yaml + docs/index.html.

NOTE: cmd passes client=nil for docs. Do NOT use AI here. Be robust when external Node tools are absent.
func Run(ctx, cfg, client *ai.Client, opts Options) error // opts: InputPath, OutDir(default docs), Title, Logo, Theme, Deploy
Pipeline (SRS §13): Collection -> postman-to-openapi -> OpenAPI -> Redoc -> HTML.
1. Detect input type via parser.DetectFormat / parser.Parse. If input is a Postman collection: convert to OpenAPI. Prefer running the Node tool via os/exec (exec the binary directly, arg slice, NEVER sh -c): try "npx --yes postman-to-openapi <in> -f <outDir>/openapi.yaml" (or the documented CLI of that package). If postman-to-openapi is unavailable OR fails, FALL BACK to a built-in minimal converter: walk the parsed *core.Collection and emit a valid minimal OpenAPI 3.0 YAML (info from title/collection name, paths from endpoints with method, summary, and declared response codes). Always end with a valid openapi.yaml in OutDir.
   If input is already OpenAPI: normalize/copy it to <outDir>/openapi.yaml (you may load via parser.ParseOpenAPI then re-emit, or copy the file).
2. Render HTML: try "npx --yes @redocly/cli build-docs <outDir>/openapi.yaml -o <outDir>/index.html" (and/or redoc-cli) to produce branded HTML. If unavailable/fails, FALL BACK to writing a self-contained index.html that loads Redoc from the CDN (https://cdn.redocly.com/redoc/latest/bundles/redoc.standalone.js) pointing at ./openapi.yaml, applying Title (page <title> + heading) and Logo if provided, and Theme color via redoc theme options. This guarantees usable docs without local tooling.
3. If Deploy: also write <outDir>/../.github/workflows/apilens-docs.yml (or note) that publishes OutDir to GitHub Pages. Keep it optional and clearly logged.
4. Helper funcs: hasCommand(name) using exec.LookPath; runTool(ctx, name, args...) capturing combined output, returning a typed error on failure so callers can fall back. Default OutDir to cfg.DocsDir when empty.
5. Log each step (tool used vs fallback).

Tests (docs_test.go): MUST NOT require npx/redoc/postman-to-openapi installed. Test the BUILT-IN paths: feed a small Postman collection -> assert openapi.yaml produced by the fallback converter is non-empty and contains the path; assert index.html fallback contains "redoc" and the openapi.yaml reference and the Title. Feed an OpenAPI input -> assert it is normalized to openapi.yaml. Unit-test the minimal-OpenAPI emitter. Use t.TempDir(). Aim >70% (exec branches may be uncovered — that's fine, but isolate them so logic is testable).

Return: the exact external commands attempted, the fallback behavior, and the HTML template approach.`,
    { label: 'feat-docs', phase: 'Features' }),

  // 8. DATATEST (Feature F) — internal/testing, package datatest, shells out to newman
  () => agent(BRIEF + `
# TASK: Implement FEATURE F — Data-Driven Testing: internal/testing/ (PACKAGE NAME datatest) (SRS §14). Runs newman with a dataset.

NOTE: cmd passes client=nil. No AI. Package name MUST be datatest (dir stays internal/testing).
func Run(ctx, cfg, client *ai.Client, opts Options) error // opts: CollectionPath, DataPath (CSV|JSON|JSONL), OutDir(default reports), Parallel, Bail, DelayMs
Behavior:
1. Validate CollectionPath exists. Load dataset via parser.LoadDataset(DataPath). If format is JSONL, also produce a normalized temp JSON array file for newman (newman accepts .csv and .json data files). Use DetectFormat.
2. Build newman invocation (exec binary directly, arg slice, NEVER sh -c): base args: run <collection> --reporters cli,json,html --reporter-json-export <outDir>/report.json --reporter-html-export <outDir>/report.html [--iteration-data <dataFile>] [--bail] [--delay-request <DelayMs>]. Resolve the newman binary via exec.LookPath("newman") else fall back to "npx" with args ["--yes","newman", ...].
3. Parallel execution: if Parallel>1, split the dataset rows into Parallel shards, write each shard to a temp data file, run one newman process per shard CONCURRENTLY (bounded by Parallel, use sync/errgroup-style with a WaitGroup + error capture), writing per-shard reports (report-1.json ...). If Parallel<=1, single run. Respect ctx cancellation (exec.CommandContext).
4. After runs, parse the JSON report(s) with parser.ParseNewman to print a terminal summary (total/passed/failed, per-shard). Aggregate failures; if Bail and failures>0, return a non-nil error (so CI fails). 
5. If newman/npx unavailable, return a clear, friendly error telling the user to install newman (npm i -g newman) — do NOT crash. MkdirAll OutDir (default cfg.ReportDir).
6. Helpers: buildNewmanArgs(opts, dataFile) []string (PURE, unit-testable), shardRows(rows, n) [][]map[string]string (PURE), writeDataFile(rows, format) (path, error).

Tests (datatest_test.go): MUST NOT require newman installed. Unit-test buildNewmanArgs (asserts flags: reporters, --bail present when Bail, --delay-request when DelayMs>0, --iteration-data present), shardRows (even/uneven splits, n>len, n=1), and the JSONL->JSON normalization + writeDataFile. Use t.TempDir(). Aim >70% (exec branches isolated).

Return: the newman arg construction, sharding strategy, and fallback messaging.`,
    { label: 'feat-datatest', phase: 'Features' }),

  // 9. MONITOR (Feature G) — generates GH Actions workflow + dirs + alerts
  () => agent(BRIEF + `
# TASK: Implement FEATURE G — Monitoring Generator: internal/monitor/monitor.go (SRS §15). Generates a GitHub Actions monitoring workflow.

NOTE: cmd passes client=nil. No AI. Pure file/template generation producing VALID YAML.
func Run(ctx, cfg, client *ai.Client, opts Options) error // opts: OutDir(default "."), Mode ("15min"|"30min"|"hourly"|"daily"), Notify ([]string of email|slack|discord|telegram)
Behavior (for "monitor init"):
1. Map Mode -> cron: 15min="*/15 * * * *", 30min="*/30 * * * *", hourly="0 * * * *", daily="0 0 * * *". Validate Mode (default hourly on unknown, but warn).
2. Write <OutDir>/.github/workflows/apilens-monitor.yml — a valid GH Actions workflow: name, on.schedule[cron] + workflow_dispatch, a job that checks out, sets up Node, installs newman (npm i -g newman), runs newman against a collection (use a documented path like ./collection.json + ./environment.json with a comment to customize), exports JSON+HTML reports, uploads them as an artifact, and for EACH requested Notify channel appends a step using the conventional repo secret (SLACK_WEBHOOK_URL, DISCORD_WEBHOOK_URL, TELEGRAM_BOT_TOKEN+TELEGRAM_CHAT_ID, or an SMTP email action with SMTP_* secrets) that fires on failure(). Use proper YAML (2-space indent). Use a templating approach (text/template) with a struct {Cron, Notify, ...}.
3. Create <OutDir>/reports/.gitkeep and <OutDir>/alerts/.gitkeep, and <OutDir>/alerts/config.yaml — a notification config template documenting which secrets each enabled channel needs.
4. Default OutDir to "." ; honor cfg.MonitorDir if you think it fits (document choice). MkdirAll as needed. Log files written. Print next-steps (which secrets to set).

Tests (monitor_test.go): run into t.TempDir() with Mode "hourly" and Notify ["slack","discord"]; assert the workflow file exists, contains "0 * * * *", "schedule", and a slack + discord step referencing the right secrets; assert reports/ and alerts/ created. Test each Mode->cron mapping (unit-test the pure mapper). Test unknown mode defaults to hourly. Aim >85%.

Return: the cron mapping, the workflow template outline, and per-channel secret names.`,
    { label: 'feat-monitor', phase: 'Features' }),
]).then(rs => rs.filter(Boolean))

log(`Features written: ${featureResults.length}/9 returned. Integrating...`)

phase('Integrate')
const integrate = await agent(BRIEF + `
# TASK (Integrate): make the WHOLE module build + vet clean again after the parallel feature implementations.

Steps:
1. ${GORUN.replace('GO_ARGS','mod tidy')}
2. ${BUILD}
3. For each compile/vet error: open the file, fix with the minimal change that preserves the feature's intent and the contract (docs/CONTRACT.md). Common issues: signature drift vs contract, wrong core field names, unused imports/vars, package name datatest, import cycles, exec/os import typos, text/template parse issues. Do NOT delete features or tests to make it pass.
4. gofmt the tree: run  ${GORUN.replace('GO_ARGS','fmt ./...')}
5. Re-run ${BUILD} until BUILD_EXIT=0 and VET_EXIT=0.

Return structured status.`,
  { label: 'integrate-build', phase: 'Integrate',
    schema: { type: 'object', additionalProperties: false,
      required: ['buildOk','vetOk','fixesApplied','remainingIssues','notes'],
      properties: {
        buildOk: { type: 'boolean' }, vetOk: { type: 'boolean' },
        fixesApplied: { type: 'array', items: { type: 'string' } },
        remainingIssues: { type: 'array', items: { type: 'string' } },
        notes: { type: 'string' },
      } } })

log(`Integrate: build=${integrate?.buildOk} vet=${integrate?.vetOk}`)

phase('Test')
const testGreen = await agent(BRIEF + `
# TASK (Test): make 'go test ./...' pass for the whole module.

The build is green. Now run the tests:
${TEST}
For each failing test: decide if the TEST or the IMPLEMENTATION is wrong. Fix the root cause (usually the implementation; fix tests only if the test asserts something incorrect vs the contract/SRS). Tests must NOT require network or external binaries (newman/redoc/npx) — if any test depends on those, refactor it to test pure helpers / fallback paths instead, or skip with t.Skip only when the binary is genuinely required AND the pure logic is covered elsewhere. Re-run ${TEST} until all packages report ok (or "no test files"). Then report per-package coverage from the output.

Return structured status with per-package coverage where available.`,
  { label: 'test-green', phase: 'Test',
    schema: { type: 'object', additionalProperties: false,
      required: ['testsOk','coverage','failuresFixed','notes'],
      properties: {
        testsOk: { type: 'boolean' },
        coverage: { type: 'array', items: { type: 'object', additionalProperties: false,
          required: ['pkg','percent'], properties: { pkg: { type: 'string' }, percent: { type: 'number' } } } },
        failuresFixed: { type: 'array', items: { type: 'string' } },
        notes: { type: 'string' },
      } } })

log(`Tests green=${testGreen?.testsOk}`)

phase('Coverage')
const covGap = await agent(BRIEF + `
# TASK (Coverage gap): raise unit-test coverage toward 80% on the core logic packages.

The suite is green. Run a coverage report:
${GORUN.replace('GO_ARGS','test ./... -coverprofile=/tmp/apilens.cov -covermode=count')}
then  ${GORUN.replace('GO_ARGS','tool cover -func=/tmp/apilens.cov')}
Focus on the deterministic, high-value packages that should be well-covered: internal/coverage, internal/scoring, internal/core/redact, internal/core/config, internal/parser, internal/generate, internal/audit, internal/review, internal/explain, internal/monitor. For any of these under ~80%, READ the existing _test.go (do not duplicate), then ADD focused table-driven tests for the uncovered branches (edge cases, error paths, helper funcs). Do NOT chase coverage in exec/network branches (docs/datatest external-tool paths) or cmd wiring — note those as intentionally lower. Keep tests deterministic (fake provider, t.TempDir). After adding, re-run ${TEST} to confirm still green.

Return the before/after coverage for the packages you touched and overall.`,
  { label: 'coverage-gap', phase: 'Coverage',
    schema: { type: 'object', additionalProperties: false,
      required: ['testsOk','packagesImproved','overallNote'],
      properties: {
        testsOk: { type: 'boolean' },
        packagesImproved: { type: 'array', items: { type: 'object', additionalProperties: false,
          required: ['pkg','before','after'], properties: { pkg: { type: 'string' }, before: { type: 'number' }, after: { type: 'number' } } } },
        overallNote: { type: 'string' },
      } } })

log('Coverage pass done. Polishing project...')

phase('Polish')
const polish = await agent(BRIEF + `
# TASK (Polish): make this a complete, runnable, well-presented open-source project. Build + tests are green; keep them green.

Create/verify (do NOT break the build — run ${BUILD} and ${TEST} at the end):
1. README.md (repo root) — comprehensive: tagline, what ApiLens is/isn't (SRS §1-2), install (go install ./... and 'go build -o apilens .'), configuration (apilens.yaml + the four API-key env vars), and a usage section with a real example for EACH command: generate, audit, review, explain, test, docs, monitor, config. Include a "How it works" note: deterministic scoring + AI narrative + secret redaction. Add a project-structure section and a "Go 1.25+ required" note (explain the bubbles dependency).
2. examples/ — realistic sample inputs so users (and reviewers) can try commands offline:
   - examples/collection.json (a small but real Postman v2.1 collection: a few endpoints, some with pm.test scripts, some without, with an auth header to exercise redaction)
   - examples/response.json (a sample API response body for 'generate')
   - examples/newman-report.json (a newman JSON run report with some passed + some failed assertions for 'explain')
   - examples/users.csv (dataset for 'test')
   - examples/openapi.yaml (a small OpenAPI 3.0 spec)
   - examples/README.md (one-liners showing each command run against these files)
3. templates/ — extract any embedded templates worth exposing: e.g. templates/monitor-workflow.yml.tmpl reference, templates/redoc-index.html.tmpl (document if monitor/docs embed these via embed or string; keep code as source of truth, templates/ is reference docs).
4. Makefile — targets: build, test, cover, vet, fmt, install, clean, run (wrap the scripts/ helpers; note Go runs via WSL). Keep it POSIX/portable.
5. LICENSE — MIT, year 2026, holder "ApiLens contributors".
6. .gitignore — Go binaries, /apilens, reports/, docs/index.html build output, coverage files, .env, etc.
7. .github/workflows/ci.yml — a CI workflow for THE APILENS PROJECT ITSELF: on push/PR, setup-go (1.25.x), go build ./..., go vet ./..., go test ./... -race -cover. (This is the project's own CI, distinct from the monitor feature output.)
8. Remove any leftover scratch files (_*.sh) from the repo root if present (keep scripts/go-env.sh, scripts/check.sh, scripts/test.sh — those are the dev harness; document them in README under Development).
9. Verify 'examples/collection.json' actually works: optionally note it. Do NOT run AI commands (no key).

Finally run ${BUILD} and ${TEST}; confirm both green. Return a file inventory of what you added + confirmation build/tests still pass.`,
  { label: 'polish', phase: 'Polish',
    schema: { type: 'object', additionalProperties: false,
      required: ['filesAdded','buildOk','testsOk','notes'],
      properties: {
        filesAdded: { type: 'array', items: { type: 'string' } },
        buildOk: { type: 'boolean' }, testsOk: { type: 'boolean' },
        notes: { type: 'string' },
      } } })

log('Polish done. Running security + Go review in parallel...')

phase('Review')
const [secReview, goReview] = await parallel([
  () => agent(`Security review of the ApiLens Go codebase at repo root (/home/ubuntu/workspace/2026_projects/api-lens; file tools use the current working dir).

Focus on the SRS §20 security promise and general Go security:
1. SECRET LEAKAGE TO LLM: verify EVERY AI prompt is built from redacted data (internal/core/redact). Trace generate/audit/review/explain: does any raw collection/response/header/body reach client.Generate/Analyze/Review/Explain WITHOUT going through redact? Check internal/core/redact correctness — does it actually mask Authorization, Bearer/Basic, API keys, tokens, cookies, passwords, JWTs? Any bypass is CRITICAL.
2. NO HARDCODED SECRETS anywhere; keys only from env.
3. COMMAND INJECTION: internal/docs + internal/testing exec external tools (npx/newman/redocly). Confirm they exec binaries directly with arg slices (no sh -c, no string interpolation into a shell). Confirm file paths from user flags can't escape intended dirs.
4. Error messages / logs must not print secret values (check httpx error snippets are redacted).
5. Standard Go: unchecked errors on security-relevant ops, path traversal in os.WriteFile/MkdirAll targets, SSRF (BaseURL override is fine; just confirm no unexpected outbound).

Read the relevant files. Report findings by severity. For CRITICAL/HIGH, give the file:line and the exact fix. Be precise and adversarial; assume a careless caller.`,
    { label: 'security-review', phase: 'Review', agentType: 'ecc:security-reviewer',
      schema: { type: 'object', additionalProperties: false,
        required: ['critical','high','medium','redactionVerified','notes'],
        properties: {
          critical: { type: 'array', items: { type: 'string' } },
          high: { type: 'array', items: { type: 'string' } },
          medium: { type: 'array', items: { type: 'string' } },
          redactionVerified: { type: 'boolean', description: 'true if all AI prompts are confirmed to use redacted data' },
          notes: { type: 'string' },
        } } }),

  () => agent(`Go code review of the ApiLens codebase at repo root (file tools use the current working dir; build via: ${BUILD}).

Review for idiomatic Go quality across internal/ and cmd/ and pkg/: error wrapping consistency, context propagation, nil-safety, goroutine/concurrency correctness (esp. internal/testing parallel sharding — data races, WaitGroup/err capture), resource leaks (http bodies closed, files closed), JSON (un)marshal correctness, interface design, and adherence to the frozen contract. Flag any function >50 lines or file >450 lines, deep nesting, and dead code. You MAY run ${BUILD} to confirm green and 'go vet'. Report findings by severity with file:line and concrete fixes. Do not rewrite; just review.`,
    { label: 'go-review', phase: 'Review', agentType: 'ecc:go-reviewer',
      schema: { type: 'object', additionalProperties: false,
        required: ['critical','high','medium','notes'],
        properties: {
          critical: { type: 'array', items: { type: 'string' } },
          high: { type: 'array', items: { type: 'string' } },
          medium: { type: 'array', items: { type: 'string' } },
          notes: { type: 'string' },
        } } }),
]).then(rs => rs.filter(Boolean))

const critHigh = []
for (const r of [secReview, goReview]) {
  if (!r) continue
  for (const c of (r.critical || [])) critHigh.push('CRITICAL: ' + c)
  for (const h of (r.high || [])) critHigh.push('HIGH: ' + h)
}

let fix = null
if (critHigh.length > 0) {
  log(`Review found ${critHigh.length} CRITICAL/HIGH issues. Applying fixes...`)
  fix = await agent(BRIEF + `
# TASK (Fix): apply fixes for the CRITICAL and HIGH review findings below, then re-verify build + tests.

Findings to fix:
${critHigh.map((s, i) => (i + 1) + '. ' + s).join('\n')}

For each: open the file, apply the minimal correct fix preserving the contract. Security findings take priority — if any prompt path bypasses redaction, fix it so all data is redacted before reaching the AI client. After fixing, run ${BUILD} then ${TEST} and ensure both are green. If a finding is a false positive, explain why instead of changing code.

Return what you changed (file:line + fix) and final build/test status.`,
    { label: 'fix-criticals', phase: 'Review',
      schema: { type: 'object', additionalProperties: false,
        required: ['buildOk','testsOk','fixes','falsePositives','notes'],
        properties: {
          buildOk: { type: 'boolean' }, testsOk: { type: 'boolean' },
          fixes: { type: 'array', items: { type: 'string' } },
          falsePositives: { type: 'array', items: { type: 'string' } },
          notes: { type: 'string' },
        } } })
} else {
  log('No CRITICAL/HIGH review findings. Skipping fix phase.')
}

return {
  features: featureResults,
  integrate,
  testGreen,
  covGap,
  polish,
  review: { security: secReview, go: goReview },
  fix,
}