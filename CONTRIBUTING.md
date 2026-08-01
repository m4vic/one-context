# Contributing

This repository is a new Go implementation, not a continuation of the archived
MCP server. Keep changes focused on automatic, local context compilation.

Before submitting a change, run:

```powershell
gofmt -w .
go test ./...
go vet ./...
```

Do not add API keys, generated `.one-context` artifacts, local logs, binaries,
or archived legacy code to a change.
