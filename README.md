# AI Connector

[中文使用说明](README.zh-CN.md) · [Privacy](PRIVACY.md) · [Security](SECURITY.md)

AI Connector runs on the computer where AI agent CLIs are installed. It reads
local Codex, Claude Code, and Gemini CLI session logs, derives session state,
and sends it over an encrypted outbound WebSocket to the [AgentBoard dashboard](https://aiboard.agentcaseshare.cn/).
It is a companion to those tools, not a replacement, and it never exposes a
local terminal as a public service. The supported adapters, resume behavior,
and privacy constraints are documented in
[`docs/architecture/multi-agent-observation.md`](docs/architecture/multi-agent-observation.md).

This guide describes the public production flow: Agent Case Share manages
account sign-in and registration, AgentBoard manages device pairing and the
real-time dashboard, and AI Connector observes and connects from the local
machine.

## End-to-End Flow

```text
Open AgentBoard in a browser
    -> Sign in or register with Agent Case Share
    -> Generate a 10-minute, one-time pairing code from Devices
    -> Run ai-connector pair --code <pairing-code> on the Codex computer
    -> Store the local device credential and start ai-connector observe serve
    -> Select the online device in Live sessions to view Codex activity
```

The Connector opens only outbound HTTPS/WSS connections, so the workstation
normally does not need an inbound firewall rule. The pairing code is used only
for initial device binding. Subsequent connections use an Ed25519 device key
generated locally, never an Agent Case Share password or API key.

## Install and Preflight

Each [GitHub Release](https://github.com/parkerluxu/ai-connector/releases)
contains standalone Windows and Linux executables, with no archive to extract.
Download the asset matching your operating system and CPU architecture:

| Platform | Release asset |
| --- | --- |
| Windows x64 | `ai-connector_<version>_windows_amd64.exe` |
| Windows ARM64 | `ai-connector_<version>_windows_arm64.exe` |
| Linux x64 | `ai-connector_<version>_linux_amd64` |
| Linux ARM64 | `ai-connector_<version>_linux_arm64` |

On Linux, make the downloaded file executable. macOS users can build from
source; this release line does not publish a prebuilt macOS asset.

```bash
chmod +x ai-connector_<version>_linux_<arch>
```

```powershell
# Windows PowerShell
.\ai-connector.exe version
```

```bash
# Linux (and macOS after a source build)
./ai-connector version
```

Building from source requires Go 1.24 or later:

```bash
go test ./...
go build ./cmd/ai-connector
```

Run the diagnostic before your first pairing. `doctor` checks the public API,
the current pairing state, and the local Codex sessions directory:

```powershell
.\ai-connector.exe doctor
```

## 1. Sign In or Register

1. Open [https://aiboard.agentcaseshare.cn/](https://aiboard.agentcaseshare.cn/).
2. Choose **Sign in** if you already have an Agent Case Share account, or
   **Register** for a new account.
3. Complete the Agent Case Share authorization flow. The registration screen is
   provided by Agent Case Share.
4. You are returned to AgentBoard automatically. AgentBoard does not create a
   second password account; the browser receives its own HttpOnly dashboard
   session.

If you repeatedly return to the sign-in page, allow cookies for the site and
check the system clock and network connection. Start the flow again from the
dashboard home page if the authorization state expires.

## 2. Generate a Pairing Code

1. After signing in, open **Devices**.
2. Select **Bind device**.
3. The dashboard displays a one-time code and its command, for example:

   ```text
   ai-connector pair --code ABCD1234
   ```

4. The code is valid for **10 minutes** and becomes invalid as soon as a device
   claims it. Never post a pairing code in a public chat, issue, or script.

An account is limited to one active device by default. Unbind the existing
device from the Devices page before pairing a replacement computer; unbinding
immediately invalidates its credentials and disconnects it.

## 3. Pair the Codex Computer

Run the Connector on the same computer that runs Codex and can reach the
internet. Prefer the interactive command so the code is not echoed or saved in
shell history:

```powershell
# Windows
.\ai-connector.exe pair
```

At `Enter the one-time pairing code:`, paste the code from the dashboard and
press Enter. Passing the code explicitly is also supported:

```powershell
.\ai-connector.exe pair --code "ABCD1234"
```

On Linux or a source-built macOS binary, run `./ai-connector pair` or
`./ai-connector pair --code "ABCD1234"`.

After a successful pairing, the Connector generates or reuses a local
`device_id`, `credential_id`, and Ed25519 key, then submits only the public key
to `/v1/devices/pair/claim`. The private key remains on the computer. The
default configuration location is:

| System | Default path |
| --- | --- |
| Windows | `%APPDATA%\AgentBoard\connector.json` |
| Linux | `~/.config/AgentBoard/connector.json` |
| macOS | `~/Library/Application Support/AgentBoard/connector.json` |

New Windows installations protect the private key with DPAPI. Unix systems
write the configuration file with `0600` permissions. Do not copy, commit, or
share this file.

## 4. Start Local Observation

First make sure Codex has run at least one local session, then start the
Connector:

```powershell
# Run in the foreground for initial verification.
.\ai-connector.exe observe serve
```

Use a second terminal to inspect discovered local sessions:

```powershell
.\ai-connector.exe observe status
```

To print the Connector configuration and device identity summary:

```powershell
.\ai-connector.exe status
```

Once the device is **Online** in the dashboard, return to **Live sessions** and
select it to receive a session snapshot and subsequent status updates. If no
sessions appear, start Codex on that computer and ensure `CODEX_HOME`, when
set, contains a `sessions` directory.

### Run as a Background Service

The Connector installs a service for the current user. It is not deployed to a
cloud server:

```powershell
# Windows: current-user startup entry
.\ai-connector.exe service install
.\ai-connector.exe service start
.\ai-connector.exe service status
```

```bash
# Linux: systemd user unit
./ai-connector service install
./ai-connector service start
./ai-connector service status
```

Stop or remove the service with:

```text
ai-connector service stop
ai-connector service uninstall
```

The service runs only the fixed `observe serve` command. It does not accept
arbitrary shell commands or tool parameters.

## 5. Dashboard Features

### Live Sessions

- See the selected device's Codex sessions as active, idle, completed, or
  aborted.
- See the working directory, source (CLI/Desktop), latest activity time, and
  current tool state.
- Group sessions by session directory or working directory.
- Select **View details** to read a local session on demand. Details are sent
  only over the active real-time connection and are not written to the server
  database.
- Use **Continue original session** on an eligible finished session to start a
  local Codex resume process, then request termination of that managed process
  if necessary.

### Device Management

- See the device name, operating system, architecture, Connector version,
  online state, and last heartbeat.
- Generate a one-time pairing code and bind a new computer.
- Rename a device, or unbind it when replacing or retiring a computer.
- Unbinding or administrative revocation closes the existing WebSocket and
  prevents the old credentials from authenticating again.

## 6. Data and Security Boundaries

Default live-status synchronization sends only the device identity, session ID,
working-directory/session-directory metadata, source, status, timestamps, and
current tool name/status.

When the device owner explicitly selects **View details**, the Connector reads
a bounded portion of that local session's messages, tool calls, and tool
results and relays it through the current authenticated real-time connection to
the owner's browser. Details are not written to the Connector metadata or the
AgentBoard server database, and they are not retained as a dashboard history.

The following data is never part of default status synchronization and is never
persisted in the AgentBoard database:

- Reasoning content or raw JSONL lines.
- Local file contents.

Read [PRIVACY.md](PRIVACY.md) for the complete scope. Report security issues
privately according to [SECURITY.md](SECURITY.md).

## 7. Troubleshooting

### `pair` reports an invalid or expired code

Generate a new code from Devices and run `pair --code` within ten minutes. A
pairing code can be claimed only once. Do not include extra characters beyond
the code itself.

### The dashboard shows the device as offline

Run `ai-connector service status` on the target computer, or start
`ai-connector observe serve` again. Then run `ai-connector doctor` to check the
API health. Corporate proxies, firewalls, or TLS-inspection appliances can also
block outbound WSS connections.

### `observe status` finds no sessions

Confirm that Codex runs under the current user. Check `CODEX_HOME\sessions` on
Windows or `$CODEX_HOME/sessions` on Linux/macOS. When `CODEX_HOME` is not set,
the Connector reads `.codex/sessions` under the current user's home directory.

### Replacing a computer or reinstalling the operating system

Unbind the old device from the dashboard while it is still available. On the
new computer, install the Connector, generate a new pairing code, and run
`pair`. Do not copy the previous computer's private-key file as a migration.

## Links

- [AgentBoard dashboard](https://aiboard.agentcaseshare.cn/)
- [Chinese README](README.zh-CN.md)
- [Privacy](PRIVACY.md)
- [Security](SECURITY.md)
- [Release process](RELEASING.md)
