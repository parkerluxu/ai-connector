# Privacy

AI Connector observes local Codex session metadata so that AgentBoard can show
live status. The Connector is deliberately designed as a field allowlist.

## Default status data sent to AgentBoard

- Device ID and credential ID
- Session ID and session-directory date path
- Working directory
- Session source, status, current tool name/status, and timestamps

## On-demand session details

When the signed-in device owner explicitly selects **View details** in the
dashboard, the Connector reads that local session JSONL file and sends a
bounded selection of user/assistant messages, tool calls, and tool results
through the authenticated real-time connection. This information is not stored
in the Connector metadata database or the AgentBoard server database; it is
displayed only in the requesting browser session.

## Data never sent

- Reasoning content
- Raw Codex JSONL lines or source JSONL filenames
- Local file contents
- The device private key or pairing code

Working directories can reveal project names. Do not pair a device unless you
accept that this metadata will be visible to the signed-in owner of the
AgentBoard account. Unbind a device in the dashboard and run `ai-connector
service uninstall` to stop future observation.
