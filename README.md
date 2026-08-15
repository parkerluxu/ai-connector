# AI Connector

[中文使用说明](README.zh-CN.md) · [Privacy](PRIVACY.md) · [Security](SECURITY.md)

AI Connector securely observes local Codex sessions and displays their current
state in the AgentBoard dashboard. It is a local companion application, not a
replacement for Codex.

The official release is preconfigured for `https://aiboard.agentcaseshare.cn`.
It contains no pairing code, device credential, API secret, or user data.

## Install

Download the archive for your operating system from the GitHub Releases page,
extract it, and run the binary from a terminal.

On Windows:

```powershell
.\ai-connector.exe doctor
.\ai-connector.exe pair
.\ai-connector.exe service install
.\ai-connector.exe service start
```

For the complete production workflow, including Agent Case Share login or
registration, generating a pairing code in the dashboard, and viewing live
Codex sessions, see the [Chinese user guide](README.zh-CN.md). The short version
is: sign in at [aiboard.agentcaseshare.cn](https://aiboard.agentcaseshare.cn/),
open **Devices**, choose **Bind device**, then run `pair --code <code>` on the
same computer before starting the observer service.

`pair` prompts for a short-lived code without echoing it. Create that code from
the Device page after signing in to AgentBoard. Do not paste pairing codes into
public chats or scripts. The Connector stores its local state at
`%APPDATA%\AgentBoard\connector.json`; Windows protects the device private key
with DPAPI.

On Linux and macOS, use the binary name without `.exe`. The service command
uses a user-level systemd unit on Linux and a LaunchAgent on macOS.

## What Leaves Your Device

The Connector reads Codex's local session JSONL files to derive a live status.
It sends only the device identity, session ID, working directory, session
directory, source, status, timestamps, and current tool name/status. It does
not send prompts, messages, reasoning, tool parameters, tool output, raw JSONL
lines, or local file contents.

Read [PRIVACY.md](PRIVACY.md) before pairing a device.

## Development

Requirements: Go 1.24 or later.

```powershell
go test ./...
go build ./cmd/ai-connector
```

The public client uses the official AgentBoard endpoint by default. The
`AGENTBOARD_API_URL` and `AGENTBOARD_WS_URL` environment variables remain
available only for maintainers testing a non-production endpoint.

## Security

Report vulnerabilities privately as described in [SECURITY.md](SECURITY.md).
Do not open a public issue containing pairing codes, configuration files, or
device credentials.
