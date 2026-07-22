# Contributing

Issues and pull requests are welcome. Please keep changes focused and include
tests for behavior changes.

Before submitting a pull request, run:

```sh
gofmt -w .
go test ./...
go vet ./...
staticcheck ./...
```

Protocol-facing changes should be checked against a current Codex app-server
schema. Never add fixtures containing real conversation content, credentials,
absolute home-directory paths, or other personal data.
