# ApiLens Core Contract (FROZEN)

This document is the authoritative, frozen type contract for the ApiLens
foundation layer. Every provider, parser, feature package, and CLI command MUST
build against exactly these types and signatures. Treat anything here as stable;
propose changes before deviating.

- Module: `github.com/apilens/apilens`
- Go directive: `go 1.25` (auto-selected by the toolchain; the latest
  `charmbracelet/bubbles`/`charmbracelet/x` deps require >= 1.25, so the SRS's
  nominal 1.24 is not satisfiable with `@latest` deps). No `toolchain` line.
- All scores (coverage, quality) are computed in deterministic Go, not by AI.
- All data leaving the process toward an LLM MUST first pass through
  `internal/core/redact`.

---

## Package `internal/core` (import path `github.com/apilens/apilens/internal/core`)

The normalized domain model shared by every feature. Plain data, safe to copy.

### Types

```go
type SourceType string

const (
	SourcePostman SourceType = "postman"
	SourceOpenAPI SourceType = "openapi"
	SourceNewman  SourceType = "newman"
)

// Parameter locations for Param.In
const (
	InQuery  = "query"
	InPath   = "path"
	InHeader = "header"
)

type Header struct {
	Key   string
	Value string
}

type Param struct {
	Name string
	In   string // InQuery | InPath | InHeader
	Value string
}

type ExampleResponse struct {
	Name    string
	Status  int
	Body    string
	Headers []Header
}

type Endpoint struct {
	ID          string   // stable id within the collection ("METHOD path" or request id)
	Name        string
	Method      string   // upper-case HTTP verb
	Path        string   // path template, e.g. "/users/{id}"
	RawURL      string   // full original URL if present
	Description string
	Headers     []Header
	QueryParams []Param
	PathParams  []Param
	HasAuth     bool
	AuthType    string   // "bearer" | "apikey" | "basic" | ...
	RequestBody string   // raw body (usually JSON)
	TestScripts []string // Postman test/assertion script sources
	Responses   []ExampleResponse
	Tags        []string // OpenAPI tags / Postman folder names
}

type Collection struct {
	Name        string
	Description string
	Source      SourceType
	BaseURL     string
	Endpoints   []Endpoint
}

type NewmanReport struct {
	CollectionName string
	Total          int
	Failed         int
	Passed         int
	Executions     []Execution
}

type Execution struct {
	Name           string
	Method         string
	URL            string
	Status         int
	ResponseTimeMs int
	Assertions     []Assertion
}

type Assertion struct {
	Name         string
	Failed       bool
	ErrorName    string
	ErrorMessage string
}
```

### Methods / functions

```go
func (c *Collection) EndpointCount() int   // nil-safe
func (r *NewmanReport) HasFailures() bool   // nil-safe
```

### Sentinel errors + helper

```go
var ErrNotImplemented   = errors.New("not implemented")
var ErrUnsupportedFormat = errors.New("unsupported format")
var ErrNoProvider       = errors.New("no provider registered")
var ErrMissingAPIKey    = errors.New("missing API key")

func FormatUserError(err error) string // maps sentinels to user-facing text
```

Wrap sentinels at call sites: `fmt.Errorf("parse postman: %w", core.ErrUnsupportedFormat)`.

---

## Package `internal/core/config`

```go
type Config struct {
	Provider   string
	Model      string
	ReportDir  string
	DocsDir    string
	MonitorDir string

	Temperature float64
	MaxTokens   int
	Timeout     time.Duration

	APIKey  string // resolved from env ONLY, never from file; may be empty
	BaseURL string // optional provider base-URL override
}

func Default() Config
func Load(path string) (Config, error) // "" => ./apilens.yaml; APILENS_ env overrides
func (c Config) EnvVarName() string     // ANTHROPIC_API_KEY / OPENAI_API_KEY / GEMINI_API_KEY / OPENROUTER_API_KEY, or ""
func (c Config) RequireAPIKey() error   // wraps core.ErrMissingAPIKey when empty
```

Defaults: provider=`anthropic`, model=`claude-sonnet-4`, report_dir=`reports`,
docs_dir=`docs`, monitor_dir=`monitor`, temperature=`0.1`, max_tokens=`4000`,
timeout=`120s`. `Load` validates: provider must be known and max_tokens > 0
(temperature >= 0). A missing `apilens.yaml` is not an error when `path == ""`.

yaml keys: `provider, model, report_dir, docs_dir, monitor_dir, temperature,
max_tokens, timeout, base_url`. Env override example: `APILENS_PROVIDER=openai`.

---

## Package `internal/providers`

The provider seam. Concrete providers live in subpackages and self-register via
`init()`; importing the subpackage is enough to make it resolvable by `New`.

```go
type Role string
const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role    Role
	Content string
}

type ChatRequest struct {
	Model       string
	System      string
	Messages    []Message
	Temperature float64
	MaxTokens   int
}

type ChatResponse struct {
	Content      string
	Model        string
	InputTokens  int
	OutputTokens int
	Raw          string // undecoded provider body, for debugging
}

type Provider interface {
	Name() string
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
}

type Factory func(cfg config.Config) (Provider, error)

func Register(name string, f Factory) // call from subpackage init(); ignores empty name / nil factory
func Registered() []string            // sorted registered names
func New(cfg config.Config) (Provider, error) // wraps core.ErrNoProvider when unknown
```

Provider subpackage authors (anthropic/openai/gemini/openrouter): implement the
`Provider` interface using `net/http`+`encoding/json` only, and add an `init()`
that calls `providers.Register("<name>", <factory>)`. Feature logic does NOT go
in providers — only HTTP transport + request/response mapping.

---

## Package `internal/core/ai`

High-level client realizing SRS §17 (Generate/Analyze/Review/Explain). All four
methods share identical shape and delegate to one private `complete`; they
differ only in caller intent. Prompt construction lives in feature packages, not
here.

```go
type Client struct { /* unexported: provider + config */ }

func New(cfg config.Config) (*Client, error)                      // resolves provider via registry
func NewWithProvider(p providers.Provider, cfg config.Config) *Client // for tests / fakes
func (c *Client) Provider() providers.Provider
func (c *Client) Config() config.Config

func (c *Client) Generate(ctx context.Context, system, user string) (string, error)
func (c *Client) Analyze(ctx context.Context, system, user string) (string, error)
func (c *Client) Review(ctx context.Context, system, user string) (string, error)
func (c *Client) Explain(ctx context.Context, system, user string) (string, error)
```

Each method builds a `ChatRequest{Model, System, Messages:[{RoleUser, user}],
Temperature, MaxTokens}` from `cfg` and returns `strings.TrimSpace(resp.Content)`.

---

## Package `internal/core/redact` (SECURITY — SRS §20)

Masks secrets before any data reaches an LLM. Never mutates input; always
returns a new value/deep copy. Favors over-redaction.

```go
const Mask = "***REDACTED***"

func String(s string) string                  // mask secrets in free text
func Headers(h []core.Header) []core.Header     // new slice; sensitive values masked, others scrubbed
func Endpoint(e core.Endpoint) core.Endpoint    // new endpoint, fully scrubbed
func Collection(c *core.Collection) *core.Collection // new deep-copied, scrubbed collection (nil-safe)

func IsSensitiveHeader(key string) bool
func IsSensitiveName(name string) bool
```

`String` masks: auth header lines, `Bearer`/`Basic` credentials (scheme word
preserved), JWTs (`eyJ...`), secret-bearing JSON values, secret URL query
params, and `key=value` secrets. Sensitive headers (Authorization,
X-Api-Key, Api-Key, Cookie, Set-Cookie, X-Auth-Token, ...) are fully masked.

**Rule for all feature packages: redact BEFORE prompting.** Build prompts from
`redact.Collection(c)` / `redact.Endpoint(e)`, never from raw input.

---

## Package `pkg/logging`

```go
func Setup(verbose bool) zerolog.Logger // console writer -> stderr; Debug if verbose else Info
func L() *zerolog.Logger                // pointer to a copy of the current package logger
```

---

## Package `pkg/tui`

```go
func RunWithSpinner(ctx context.Context, label string, fn func(context.Context) error) error
```

Shows a Bubble Tea spinner labeled `label` while `fn` runs in a goroutine; stops
when `fn` returns and propagates its error. When stdout is not a TTY, prints the
label to stderr and runs `fn` directly. `fn` receives a context cancelled when
the spinner stops (including Ctrl+C).

---

# Feature package contracts (to be implemented later)

All command features share ONE entry-point shape:

```go
func Run(ctx context.Context, cfg config.Config, client *ai.Client, opts Options) error
```

`opts` is the per-feature `Options` struct below. The engines (`coverage`,
`scoring`) are pure/deterministic and take no AI client.

### `internal/generate`
```go
type Options struct {
	ResponsePath string // --response response.json
	Endpoint     string // --endpoint "GET /users"
	Status       int    // --status 200
	OutputPath   string // default generated-tests.js
}
func Run(ctx context.Context, cfg config.Config, client *ai.Client, opts Options) error
```

### `internal/audit`
```go
type Options struct {
	InputPath string // collection.json or openapi spec
	OutDir    string
	Format    string // "md" | "json" | "both"
}
func Run(ctx context.Context, cfg config.Config, client *ai.Client, opts Options) error
```

### `internal/review`
```go
type Options struct {
	InputPath string
	OutPath   string // default review.md
}
func Run(ctx context.Context, cfg config.Config, client *ai.Client, opts Options) error
```

### `internal/explain`
```go
type Options struct {
	ReportPath     string // Newman JSON/HTML report
	CollectionPath string // optional collection for correlation
	OutPath        string // default failure-analysis.md
}
func Run(ctx context.Context, cfg config.Config, client *ai.Client, opts Options) error
```

### `internal/docs`
```go
type Options struct {
	InputPath string
	OutDir    string // default docs
	Title     string
	Logo      string
	Theme     string
	Deploy    bool // deploy to GitHub Pages
}
func Run(ctx context.Context, cfg config.Config, client *ai.Client, opts Options) error
```

### `internal/testing` (package name `datatest`, NOT `testing`)
```go
type Options struct {
	CollectionPath string
	DataPath       string // CSV | JSON | JSONL
	OutDir         string
	Parallel       int
	Bail           bool
	DelayMs        int
}
func Run(ctx context.Context, cfg config.Config, client *ai.Client, opts Options) error
```

### `internal/monitor`
```go
type Options struct {
	OutDir string
	Mode   string   // "15min" | "30min" | "hourly" | "daily"
	Notify []string // "email" | "slack" | "discord" | "telegram"
}
func Run(ctx context.Context, cfg config.Config, client *ai.Client, opts Options) error
```

### `internal/coverage` (deterministic engine — NO AI)
```go
type CategoryScore struct {
	Name    string
	Score   int      // 0-100
	Found   []string
	Missing []string
}

type Result struct {
	Overall       int      // 0-100
	Categories    []CategoryScore
	MissingChecks []string
	RiskLevel     string   // "Low" | "Medium" | "High"
}

func Analyze(c *core.Collection) Result
```

Coverage categories map to SRS §10: Authentication, Error, Validation, Security,
Performance, Edge Case. Checks include: 401, 403, 404, 422, 429, 500, invalid
payload, missing fields, empty body, boundary values, large payload,
authorization failure, expired token, duplicate data, invalid format, missing
headers.

### `internal/scoring` (deterministic engine — NO AI)
```go
type Subscores struct {
	Coverage      int // 0-100
	Validation    int
	Security      int
	ErrorHandling int
	Documentation int
}

type Score struct {
	Value     int    // weighted 0-100
	Grade     string // "A" | "B" | "C" | "D" | "F"
	Subscores Subscores
}

func Compute(s Subscores) Score
```

Grade bands (suggested): A >= 90, B >= 80, C >= 70, D >= 60, else F.

### `internal/parser` (dispatch — detects format, returns normalized model)
```go
func Parse(path string) (*core.Collection, error)   // postman | openapi auto-detect; core.ErrUnsupportedFormat otherwise
func ParseNewman(path string) (*core.NewmanReport, error)
```
(Exact parser API is owned by the parser-phase agent; the above is the expected
shape so feature packages can depend on `*core.Collection` / `*core.NewmanReport`.)
