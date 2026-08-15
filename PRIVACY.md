# Privacy

AI Connector observes local Codex session metadata so that AgentBoard can show
live status. The Connector is deliberately designed as a field allowlist.

## Data sent to AgentBoard

- Device ID and credential ID
- Session ID and session-directory date path
- Working directory
- Session source, status, current tool name/status, and timestamps

## Data that is never sent

- Prompts, messages, reasoning, or transcript text
- Tool arguments, tool output, shell commands, or file contents
- Raw Codex JSONL lines or source JSONL filenames
- The device private key or pairing code

Working directories can reveal project names. Do not pair a device unless you
accept that this metadata will be visible to the signed-in owner of the
AgentBoard account. Unbind a device in the dashboard and run `ai-connector
service uninstall` to stop future observation.
