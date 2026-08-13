## Summary

Describe the behavior change and the related issue or plan.

## Validation

- [ ] `go test ./...`
- [ ] `go test -race ./...`
- [ ] `go vet ./...`
- [ ] `go build ./cmd/ai-prov ./cmd/ai-prov-mcp`
- [ ] `git diff --check`

## Compatibility and documentation

- [ ] I documented any CLI, MCP API, configuration, storage/schema, provenance algorithm, Hook, install, or Release impact.
- [ ] Any new Go file has the repository MIT Header.

## Security and privacy

- [ ] This PR contains no tokens, credentials, private keys, personal paths, `.ai-provenance/` data, source snapshots, databases, or diagnostic archives.
