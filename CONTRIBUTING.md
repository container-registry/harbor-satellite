# Contributing to Harbor Satellite

Welcome! Harbor Satellite is developed in the open, and we're grateful to the community for contributing bug fixes and improvements. Read below to learn how to contribute to Harbor Satellite.

## Before You Start

- Read the [README](README.md) to understand the project scope and architecture
- Check [existing issues](https://github.com/goharbor/harbor-satellite/issues) to avoid duplicates
- Join [#harbor-satellite on CNCF Slack](https://cloud-native.slack.com/archives/C06NE6EJBU1)

## Ways to Contribute

- **Code**: Bug fixes, features, improvements
- **Documentation**: Tutorials, guides, examples
- **Testing**: Write tests, report bugs
- **Community**: Answer questions, participate in discussions

## Getting Started

### 1. Fork and Clone

```bash
# Fork the repository on GitHub
# Clone your fork
git clone https://github.com/YOUR_USERNAME/harbor-satellite.git
cd harbor-satellite

# Add upstream remote
git remote add upstream https://github.com/goharbor/harbor-satellite.git
```

### 2. Set Up Development Environment

Follow the setup instructions in [QUICKSTART.md](QUICKSTART.md).

### 3. Create a Branch

```bash
# Sync with upstream
git fetch upstream
git checkout main
git rebase upstream/main

# Create feature branch
git checkout -b feature/your-feature-name
```

## Contribution Workflow

### Small Changes

For trivial fixes (typos, small bug fixes), submit a PR directly.

### Larger Changes

1. **Discuss first**: Open an issue or discuss in Slack
2. **Get feedback**: Ensure the approach aligns with project goals
3. **Implement**: Write code following our standards
4. **Test**: Ensure all tests pass
5. **Submit PR**: Open a pull request

### Testing

- All new features require tests
- Bug fixes must include regression tests
- Tests should be clear and focused on behavior

## Pull Request Guidelines

### Before Submitting

- [ ] Code follows project style
- [ ] Tests pass locally
- [ ] Comments are accurate and updated
- [ ] Commits are logical and well-described
- [ ] PR description explains *what* and *why*

## Issue Reporting

### Bug Reports

Include:
- Expected behavior
- Actual behavior
- Steps to reproduce
- Environment details (OS, Go version, etc.)
- Relevant logs or error messages

### Feature Requests

Include:
- Use case description
- Proposed solution
- Alternatives considered
- Any additional context

## Developer Certificate of Origin (DCO)

All commits must be signed off to indicate you agree to the [Developer Certificate of Origin](https://developercertificate.org/):

```bash
git commit -s -m "Your commit message"
```

This adds a `Signed-off-by` line to your commit message.

## Review Process

- Maintainers will review PRs as time permits
- Address feedback constructively
- Be patient and respectful
- PRs may require multiple rounds of review

## Communication

- **Slack**: [#harbor-satellite](https://cloud-native.slack.com/archives/C06NE6EJBU1)
- **Issues**: GitHub issue tracker
- **Email**: For sensitive topics, contact maintainers directly

## Code of Conduct

Harbor Satellite follows the [CNCF Code of Conduct](https://github.com/cncf/foundation/blob/main/code-of-conduct.md). Be respectful and inclusive.

## Questions?

If you're unsure about anything, just ask! We're here to help. Open an issue, ask in Slack, or reach out to maintainers.

---

Thank you for contributing to Harbor Satellite!
