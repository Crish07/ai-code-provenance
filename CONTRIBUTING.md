# Contributing to ai-code-provenance

Thanks for improving ai-code-provenance. Please open an Issue before a large or behavior-changing contribution so the scope can be discussed.

## Before opening a pull request

1. Fork the repository and create a focused branch.
2. Keep production changes, tests, and user-facing documentation in sync.
3. Run:

   ```bash
   go test ./...
   go test -race ./...
   go vet ./...
   go build ./cmd/ai-prov ./cmd/ai-prov-mcp
   git diff --check
   ```

4. Add the repository MIT Header to every new Go file. The complete license is [LICENSE](LICENSE).
5. Complete the pull-request template, including compatibility and privacy checks.

## Contribution boundaries

- Do not commit `.ai-provenance/`, snapshots, SQLite databases, debug archives, binaries, release artifacts, tokens, credentials, private keys, or private machine paths.
- Do not change the MCP protocol, SQLite schema, snapshot format, provenance algorithm, Hook defaults, installation behavior, or Release workflow without tests and matching documentation.
- Keep tests offline and isolated with temporary directories and temporary Git repositories.

## Reviews

All changes to the official repository are reviewed through pull requests. Forks may be changed freely under the MIT License; repository maintainers decide what is merged into `main`.
