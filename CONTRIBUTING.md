# Contributing to Harbor Satellite

Thank you for your interest in contributing to Harbor Satellite! We welcome contributions of all kinds - bug fixes, documentation improvements, new features, and discussion.

## Code of Conduct

This project is a CNCF project and follows the [CNCF Code of Conduct](https://github.com/cncf/foundation/blob/main/code-of-conduct.md). Please read it before participating.

## Developer Certificate of Origin (DCO)

All commits must be signed off, certifying that you have the right to submit the code under the project license.

To sign off a commit:

```bash
git commit -s -m "feat(state): add retry on replication failure"
```

This appends a `Signed-off-by` line to your commit message. Make sure the name and email in your Git config match your identity.

## Getting Started

### Prerequisites

- [Go](https://go.dev/dl/) `1.22+`
- [Task](https://taskfile.dev/installation/) — used for all build, lint, and test automation
- [Docker](https://docs.docker.com/get-docker/) and Docker Compose — required for local development and E2E tests

### Repository Structure

This repository contains two separate Go modules:

| Path | Purpose |
|---|---|
| `/` (root module) | Satellite edge daemon - CLI, config, registry, state replication |
| `ground-control/` | Ground Control cloud service - satellite management, Harbor integration, PostgreSQL |

When running Go commands, be aware of which module you are working in. Run commands from the appropriate directory (`/` for satellite, `ground-control/` for Ground Control).

### Building

```bash
# Build both components for the current platform
task build

# Build individual components
task _build:satellite
task _build:ground-control

# Run the satellite directly
go run cmd/main.go --token "<token>" --ground-control-url "http://127.0.0.1:8080"

# Run Ground Control directly (requires a configured .env file)
cd ground-control && go run main.go
```

For Ground Control local setup, copy `ground-control/.env.example` to `ground-control/.env` and fill in the required values. See [ground-control/README.md](ground-control/README.md) for details.

For satellite quickstart instructions, refer to [QUICKSTART.md](QUICKSTART.md).

## How to Contribute

### 1. Fork and Clone

```bash
git clone https://github.com/your-username/harbor-satellite.git
cd harbor-satellite
```

### 2. Create a Branch

Use a descriptive branch name prefixed by the type of change:

```bash
git checkout -b feat/<your-feature-name>
# or
git checkout -b fix/<short-description>
```

### 3. Make Your Changes

Follow the code style and standards described below. Keep changes focused — avoid mixing unrelated fixes or features in a single branch.

### 4. Run Tests and Lint

Before pushing, verify your changes pass all checks:

```bash
# Run unit tests for the satellite module
go test ./... -v -count=1

# Run unit tests for Ground Control
cd ground-control && go test ./... -v -count=1

# Run E2E tests
task e2e-test

# Lint both modules
task lint
```

CI runs lint and tests automatically on all pull requests. A clean local run saves review cycles.

### 5. Commit with a Clear Message

Use [Conventional Commits](https://www.conventionalcommits.org/) style:

```bash
git commit -s -m "fix(registry): handle zot startup race on slow hosts"
```

Common prefixes:

| Prefix | When to use |
|---|---|
| `feat` | New feature |
| `fix` | Bug fix |
| `docs` | Documentation only |
| `test` | Adding or updating tests |
| `refactor` | Code cleanup without behavior change |
| `chore` | Maintenance tasks |

### 6. Open an Issue First

Before opening a pull request, please open a GitHub Issue describing the problem or feature. This lets maintainers give early feedback and avoids duplicated or misdirected effort.

Pull requests without a linked issue may be closed without review.

### 7. Open a Pull Request

Once your issue has been acknowledged, open a pull request and link it to the issue (`Closes #<issue-number>`).

A good pull request includes:

- A clear description of what changed and why
- Evidence that the changes were tested (terminal output, test results, etc.)
- Well-structured, readable commits
- No unrelated changes

Note on AI-assisted contributions: using AI tools to assist your work is fine, but please ensure you understand every line you submit and can speak to the changes during review. Drive-by or bulk AI-generated pull requests add burden for maintainers and will be closed.

## Code Style

The project uses a strict `golangci-lint` configuration with 50+ linters. Key rules to follow:

- Prefer `any` over `interface{}` (Go 1.22+)
- Avoid package-level global variables (`gochecknoglobals`)
- Avoid `init()` functions (`gochecknoinits`)
- Use `t.TempDir()` in tests instead of `os.TempDir()`
- For configuration mutations, always use the `With()` modifiers on the config manager — never mutate directly via `GetConfig()`
- Keep functions under 100 lines and 50 statements

## Running Tests

Unit tests are colocated with source files (`*_test.go`). E2E tests live in `test/e2e/`.

```bash
# Unit tests — satellite module
go test ./... -v -count=1

# Unit tests — Ground Control module
cd ground-control && go test ./... -v -count=1

# E2E tests
task e2e-test        # standard E2E
task e2e-byo         # BYO registry E2E
task e2e-spiffe      # SPIFFE mTLS E2E
```

## Community and Communication

- **CNCF Slack**: [#harbor-satellite](https://cloud-native.slack.com/archives/C06NE6EJBU1) — request an invite at [slack.cncf.io](https://slack.cncf.io/)
- **Mailing lists**:
  - Users: harbor-users@lists.cncf.io
  - Developers: harbor-dev@lists.cncf.io
- **GitHub Issues**: For bug reports, feature requests, and questions

## License

All contributions are made under the [Apache 2.0 License](LICENSE).
