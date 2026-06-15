# ApiLens SRS v1.0

## Product Name

ApiLens

## Tagline

AI-Powered API Quality Platform for Developers

---

# 1. Product Vision

ApiLens là một CLI-first platform giúp developer kiểm tra, đánh giá, monitor và cải thiện chất lượng API bằng AI.

ApiLens không phải là Postman alternative.

ApiLens là lớp AI nằm trên các công cụ API hiện có như:

* Postman
* Newman
* OpenAPI
* Swagger
* Bruno
* Hoppscotch

Mục tiêu:

* Tăng API test coverage
* Tự động tạo test
* Tìm lỗ hổng trong test suite
* Phân tích nguyên nhân test failure
* Sinh documentation
* Tạo monitoring miễn phí

---

# 2. Product Principles

## Principle 1

Do not reimplement existing mature tools.

## Principle 2

AI features must create the main value.

## Principle 3

CLI-first architecture.

## Principle 4

Works with existing developer workflow.

## Principle 5

Single binary deployment.

---

# 3. Tech Stack

Primary Language:

Go >= 1.24

CLI:

github.com/spf13/cobra

Configuration:

github.com/spf13/viper

Logging:

github.com/rs/zerolog

Terminal UI:

github.com/charmbracelet/bubbletea

OpenAPI:

github.com/getkin/kin-openapi

LLM Providers:

Anthropic
OpenAI
Gemini
OpenRouter

---

# 4. Out of Scope

Do not build:

* API client
* Request editor
* GUI application
* Collection editor
* API gateway
* Custom scheduler

---

# 5. Open Source Strategy

Reuse existing tools whenever possible.

## Use Existing

Newman

Purpose:
Collection execution

Redoc

Purpose:
Documentation rendering

Redocly CLI

Purpose:
OpenAPI linting and docs generation

postman-to-openapi

Purpose:
Collection conversion

GitHub Actions

Purpose:
Scheduling and monitoring

## Build Custom

AI Collection Auditor

AI Test Generator

AI Failure Analyzer

Coverage Scoring Engine

Quality Scoring Engine

Monitoring Recommendation Engine

---

# 6. User Personas

## Solo Developer

Wants:

* quick testing
* automation
* documentation

## Freelancer

Wants:

* API validation
* client documentation

## Startup Engineer

Wants:

* quality assurance
* monitoring

---

# 7. CLI Commands

apilens generate

apilens audit

apilens review

apilens explain

apilens test

apilens docs

apilens monitor

apilens config

---

# 8. Configuration System

File:

apilens.yaml

Example:

provider: anthropic

model: claude-sonnet-4

report_dir: reports

docs_dir: docs

monitor_dir: monitor

temperature: 0.1

max_tokens: 4000

---

# 9. Feature A — AI Test Generator

Purpose:

Generate test scripts automatically.

Command:

apilens generate 
--response response.json 
--endpoint "GET /users" 
--status 200

Input:

response JSON

endpoint metadata

expected status

Output:

generated-tests.js

Capabilities:

status validation

response time validation

required fields validation

schema validation

type validation

format validation

null validation

nested object validation

array validation

edge case suggestions

Output Quality Requirements:

Must be executable inside Postman Tests tab.

Must be Newman compatible.

---

# 10. Feature B — AI Collection Auditor

Purpose:

Audit API testing quality.

Command:

apilens audit collection.json

Inputs:

Postman Collection

OpenAPI Spec

Output:

audit-report.md

audit-report.json

Metrics:

Authentication Coverage

Error Coverage

Validation Coverage

Security Coverage

Performance Coverage

Edge Case Coverage

Coverage Score

Quality Score

Coverage Score:

0 - 100

Quality Score:

0 - 100

Coverage Checks:

401

403

404

422

429

500

invalid payload

missing fields

empty body

boundary values

large payload

authorization failure

expired token

duplicate data

invalid format

missing headers

Output Example:

Coverage Score: 67

Quality Score: 72

Missing:

401 Unauthorized

429 Rate Limit

Empty Payload

Invalid Email

Expired JWT

Risk Level:

Medium

---

# 11. Feature C — AI Collection Review

Purpose:

Provide actionable recommendations.

Command:

apilens review collection.json

Output:

review.md

Capabilities:

Identify weak endpoints

Identify untested endpoints

Suggest missing scenarios

Recommend improvements

Estimate production risk

Output Example:

High Risk Endpoints:

POST /users

DELETE /users

Reasons:

No validation tests

No authorization tests

No negative tests

Recommendations:

Add 401 tests

Add malformed payload tests

Add duplicate email tests

---

# 12. Feature D — AI Failure Analyzer

Purpose:

Explain failed tests.

Command:

apilens explain report.json

Inputs:

Newman JSON report

Newman HTML report

Collection file

Output:

failure-analysis.md

Capabilities:

Parse failures

Group related failures

Predict root causes

Recommend fixes

Generate confidence scores

Example:

Root Cause:

Schema Changed

Confidence:

84%

Evidence:

Field email renamed to email_address

Suggested Actions:

Update assertions

Update schema validation

---

# 13. Feature E — Documentation Generator

Purpose:

Generate branded API documentation.

Command:

apilens docs collection.json

Pipeline:

Collection

↓

postman-to-openapi

↓

OpenAPI

↓

Redoc

↓

HTML

Outputs:

docs/index.html

docs/openapi.yaml

Options:

title

logo

theme

custom CSS

deploy GitHub Pages

---

# 14. Feature F — Data Driven Testing

Purpose:

Run collection against datasets.

Command:

apilens test 
--collection collection.json 
--data users.csv

Supported Formats:

CSV

JSON

JSONL

Outputs:

HTML report

JSON report

terminal summary

Capabilities:

parallel execution

bail mode

delay mode

environment variables

---

# 15. Feature G — Monitoring Generator

Purpose:

Generate monitoring workflow.

Command:

apilens monitor init

Outputs:

.github/workflows/apilens-monitor.yml

reports/

alerts/

Notifications:

Email

Slack

Discord

Telegram

Monitoring Modes:

every 15 min

every 30 min

hourly

daily

---

# 16. Feature H — API Quality Score Engine

Purpose:

Create standardized API quality scoring.

Formula:

Coverage Score

*

Validation Score

*

Security Score

*

Error Handling Score

*

Documentation Score

Output:

0 - 100

Grades:

A

B

C

D

F

---

# 17. AI Provider Layer

Interface:

Provider

Methods:

Generate()

Analyze()

Review()

Explain()

Providers:

Anthropic

OpenAI

Gemini

OpenRouter

Future:

Local Models

Ollama

vLLM

LM Studio

---

# 18. Project Structure

apilens/

cmd/

internal/

audit/

review/

generate/

explain/

monitor/

docs/

testing/

coverage/

scoring/

parser/

providers/

anthropic/

openai/

gemini/

openrouter/

core/

pkg/

configs/

templates/

examples/

scripts/

docs/

tests/

---

# 19. Performance Requirements

Startup Time:

< 2 seconds

Audit 100 endpoints:

< 15 seconds

Audit 500 endpoints:

< 60 seconds

Memory Usage:

< 500 MB

---

# 20. Security Requirements

Never send secrets to LLM.

Mask:

Authorization

API Keys

Tokens

Cookies

Passwords

Before prompt generation.

---

# 21. MVP Scope

Phase 1

CLI Framework

Config

Provider Layer

Phase 2

Generate

Audit

Phase 3

Review

Phase 4

Explain

Phase 5

Docs

Phase 6

Monitor

Phase 7

Quality Score Engine

---

# 22. Success Metrics

GitHub Stars:

100+

Downloads:

500+

Generated Tests:

5000+

Audit Reports:

1000+

Failure Analyses:

1000+

---

# 23. Future Roadmap

VSCode Extension

GitHub PR Bot

CI Integration

OpenAPI Diff Analyzer

Security Scanner

Contract Testing

Load Testing Suggestions

AI Agent Mode

Team Dashboard

Web UI
