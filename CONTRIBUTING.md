# Contributing to chậmlại-vn

Thanks for your interest in contributing! chậmlại-vn is a Vietnamese-language
RAG system for detecting fraud/scam signals in documents and contracts. This
guide covers how to report issues and submit changes.

## Reporting issues

- Search [existing issues](../../issues) first to avoid duplicates.
- For bugs, include: what you expected, what happened, steps to reproduce, and
  your environment (Go version, OS, relevant config).
- For feature ideas, describe the problem you're trying to solve, not just the
  solution you have in mind.

## Submitting changes

1. Fork the repo and create a branch from `main`
   (e.g. `feat/hybrid-search`, `fix/chunking-offset`).
2. Make your change. Keep pull requests focused — one logical change per PR is
   easier to review than a large mixed one.
3. Make sure the project builds and existing tests pass before opening the PR.
4. Sign off your commits (see *Developer Certificate of Origin* below).
5. Open the PR with a clear description of **what** changed and **why**.

## Code style

- Format with `gofmt` (or `goimports`) — no unformatted code.
- Code must pass `golangci-lint run` cleanly. Lint and formatting are enforced
  via pre-commit hooks (`lefthook`), so install them after cloning.
- Never commit secrets. `gitleaks` runs on commit; do not bypass it.
- Prefer clear, standard-library-first Go. Match the conventions already in the
  codebase.

## Developer Certificate of Origin (DCO)

By signing off on your commits, you certify that you wrote the code or otherwise
have the right to submit it under the project's license. Add a sign-off line to
each commit:

```
git commit -s -m "your message"
```

This appends a `Signed-off-by: Your Name <you@example.com>` line using your git
config.

## Licensing of contributions

This project is licensed under the **Apache License 2.0**.

By submitting a contribution (code, documentation, or any other material) to
this project, you agree that:

1. Your contribution is licensed under the Apache License 2.0; **and**
2. You grant the project maintainer a perpetual, worldwide, non-exclusive,
   royalty-free, irrevocable license to use, reproduce, modify, sublicense, and
   **relicense** your contribution — including under different terms, whether
   open-source or proprietary — as part of this project or derivative works.

In short: you keep the copyright to your contribution, but you allow the
maintainer to license the project (including your contribution) under other
terms in the future. If you do not agree with this, please do not submit a
contribution.

## Questions

Open an issue with the `question` label, or reach out to the maintainer.