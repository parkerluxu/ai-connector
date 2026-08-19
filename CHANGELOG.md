# Changelog

## 0.3.1 - 2026-08-19

- Prevented Codex detail views from showing the same user or assistant message twice when JSONL contains both event representations.
- Added regression coverage for cross-representation deduplication and separate repeated turns.

## 0.3.0 - 2026-08-19

- Added starting new Codex conversations from an observed working directory.
- Added directory validation, startup locking, and cooldown protection for new sessions.
- Classified Codex Desktop writer conflicts as `session_writer_busy` for callers.
- Added realtime start acknowledgements and asynchronous startup failure reporting.

## 0.1.0 - 2026-08-15

- Extracted the Connector into a standalone distributable Go module.
- Defaulted new installs to the official AgentBoard service.
- Added hidden interactive pairing-code entry and removed development bootstrap.
- Added Windows DPAPI protection for newly stored device private keys.
- Added a macOS LaunchAgent service implementation for source builds.
- Added complete English and Chinese production pairing guides.
- Configured release assets as standalone Windows and Linux executables.
