# Security Policy

## Reporting a vulnerability

Do not report security vulnerabilities in public Issues, pull requests, logs, or diagnostic bundles. In particular, never disclose tokens, credentials, private keys, source snapshots, `.ai-provenance/` data, SQLite databases, or private repository paths.

If this repository has GitHub private vulnerability reporting enabled, use the repository's **Security → Report a vulnerability** entry. Otherwise, contact the repository owner privately through the contact method listed on the GitHub profile for [@Crish07](https://github.com/Crish07).

Please include a minimal reproduction, affected version, impact, and mitigation ideas. Do not include sensitive project data.

## Scope

Security reports may cover the `ai-prov` CLI, `ai-prov-mcp` server, Git Hook installation, user-level installer/uninstaller, local storage handling, Release archives, and GitHub Actions workflows.

## Supported versions

Security fixes are evaluated for the latest released version. Pre-release builds may change without backward-compatibility guarantees.
