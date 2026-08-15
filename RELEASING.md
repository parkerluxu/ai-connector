# Releasing

## One-time repository setup

1. Create the public GitHub repository `agentboard/ai-connector` and push the
   `main` branch from this directory.
2. Enable GitHub private vulnerability reporting.
3. Require the CI workflow to pass before merging to `main`.
4. Before publishing a 1.0 release, obtain a Windows code-signing certificate
   and Apple Developer signing/notarization credentials. Store them only as
   GitHub Actions secrets.

## Release checklist

1. Review `CHANGELOG.md`, privacy documentation, and dependency updates.
2. Run `go test ./...`, `go vet ./...`, and build every target locally or in
   CI.
3. Create and push a signed tag such as `v0.1.0`.
4. The Release workflow publishes Windows ZIP, macOS/Linux tarballs, source
   archive, and `checksums.txt`.
5. Verify an archive checksum, run `ai-connector version`, `doctor`, `pair`,
   and `service install` on a clean test account before announcing it.

The GitHub workflow intentionally does not pretend to code-sign artifacts.
Enable signing only after real certificates have been provisioned and the
signed artifacts have been tested on clean machines.
