# Contributing

Please open an issue before making a large behavioral or protocol change. Keep
the outbound observation model minimal: prompts, transcript data, tool
arguments, command lines, tool output, raw JSONL, and local file contents must
never be added to outbound payloads.

Before opening a pull request, run:

```bash
go test ./...
go vet ./...
```

Contributions are submitted under the repository license. Never add real user
session files, pairing codes, credentials, or secrets to tests or examples.
