# Contributing to Onchain Monitor

Thank you for your interest in contributing! This document provides guidelines for contributing to this project.

## Getting Started

1. **Fork** the repository
2. **Clone** your fork: `git clone https://github.com/<you>/onchain-monitor.git`
3. **Create a branch**: `git checkout -b feature/your-feature`
4. **Install prerequisites**: Go 1.24+, PostgreSQL, Redis
5. **Run tests**: `make test`

## Development Setup

```bash
export DATABASE_URL="postgres://user:pass@localhost:5432/onchain_monitor?sslmode=disable"
export TELEGRAM_BOT_TOKEN="your-bot-token"
export REDIS_URL="redis://localhost:6379"

make run
```

## Adding a New Data Source

1. Create a new file in `internal/monitor/sources/`
2. Implement the `Source` interface:
   ```go
   type Source interface {
       Name() string
       Chain() string
       FetchSnapshot() (*Snapshot, error)
       FetchDailyReport() (string, error)
       URL() string
   }
   ```
3. Register the source in `cmd/server/main.go`
4. Add a corresponding migration to seed the event in `internal/store/migrations.go`
5. Write tests in `internal/monitor/sources/<name>_test.go`

## Code Guidelines

- Follow standard Go conventions (`gofmt`, `go vet`)
- All new code must have unit tests
- Use table-driven tests where appropriate
- Use `httptest` to test external API integrations (no live calls in tests)
- Keep functions small and focused
- Add comments only when the intent isn't obvious from the code

## Pull Request Process

1. Ensure all tests pass: `make test`
2. Ensure linting passes: `make lint`
3. Update documentation if your change affects the public API or configuration
4. Write a clear PR description explaining the change and motivation
5. Keep PRs focused — one feature or fix per PR

## Commit Messages

Use conventional commit format:

```
feat: add new data source for Uniswap
fix: handle nil snapshot in stats handler
docs: update README with new configuration options
test: add unit tests for dedup package
```

## Reporting Issues

- Use the [bug report template](.github/ISSUE_TEMPLATE/bug_report.md) for bugs
- Use the [feature request template](.github/ISSUE_TEMPLATE/feature_request.md) for new ideas
- Include reproduction steps and environment details for bugs

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
