# Use AI Connector with AgentBoard to Monitor Codex, Claude Code, and Gemini CLI from Another Device

If Codex, Claude Code, or Gemini CLI is running on one computer and you want to monitor it from a phone, tablet, or another computer, use [AI Connector](https://github.com/parkerluxu/ai-connector) together with the [AgentBoard dashboard](https://aiboard.agentcaseshare.cn/).

The key idea is simple: **the AI task continues to run locally on the paired computer, while the browser only views a filtered status stream. The server does not store the paired computer's concrete usage content, such as transcripts, reasoning, local files, or tool inputs and outputs.** The Connector only opens an outbound connection to AgentBoard, so the paired computer normally does not need a public inbound port.

This guide uses the following example:

- Computer A runs the AI CLI and ai-connector.
- Computer B is a phone, tablet, or another computer used to monitor the work.

> **Most important privacy conclusion:** The server does not retain concrete usage content from paired devices. Conversation records, prompts, reasoning, local files, tool arguments, and tool outputs are not written to server-side history. The server only handles the minimum control-plane data needed for account/device binding and realtime relay.

## 1. What You Get

After setup, the data flow looks like this:

~~~text
Computer A: Codex / Claude Code / Gemini CLI
                 | local session logs
                 v
Computer A: ai-connector
                 | outbound HTTPS/WSS, encrypted and authenticated
                 v
AgentBoard: transient realtime relay
                 | forwards the current connection
                 v
Computer B: AgentBoard in a browser
~~~

From Computer B, you can see:

- Which sessions are active, idle, completed, or aborted.
- Whether a session belongs to Codex, Claude Code, or Gemini CLI.
- The working directory, session source, recent activity, and current tool status.
- The paired device's online state, operating system, architecture, Connector version, and last heartbeat.
- Bounded session messages and tool events after the device owner explicitly selects View details.
- A new prompt sent to an eligible finished session so that Computer A continues the work locally.

Computer B never receives direct filesystem access to Computer A and does not become an arbitrary remote shell.

## 2. Prerequisites

### Computer A: install the AI tool and Connector

Computer A must have at least one supported CLI installed and working:

| Agent | Default local session directory | Resume command |
| --- | --- | --- |
| Codex | CODEX_HOME/sessions, usually ~/.codex/sessions when unset | codex exec resume --skip-git-repo-check <session-id> <prompt> |
| Claude Code | ~/.claude/projects, override with AGENTBOARD_CLAUDE_HOME | claude --resume <session-id> --print <prompt> |
| Gemini CLI | ~/.gemini/tmp, override with AGENTBOARD_GEMINI_HOME | gemini --resume <session-id> --prompt <prompt> |

Download the matching Connector from [GitHub Releases](https://github.com/parkerluxu/ai-connector/releases):

| Platform | Example file |
| --- | --- |
| Windows x64 | ai-connector_<version>_windows_amd64.exe |
| Windows ARM64 | ai-connector_<version>_windows_arm64.exe |
| Linux x64 | ai-connector_<version>_linux_amd64 |
| Linux ARM64 | ai-connector_<version>_linux_arm64 |

On Windows, run the .exe directly. On Linux, make the downloaded binary executable:

~~~bash
chmod +x ai-connector_<version>_linux_<arch>
~~~

The current Release does not provide a prebuilt macOS binary. Build from source with Go 1.24 or newer:

~~~bash
go build ./cmd/ai-connector
~~~

Check the binary and the service health before pairing:

~~~powershell
.\ai-connector.exe version
.\ai-connector.exe doctor
~~~

On Linux/macOS, replace .\ai-connector.exe with ./ai-connector.

### Computer B: prepare a browser

Computer B only needs an internet-connected browser that can open [https://aiboard.agentcaseshare.cn/](https://aiboard.agentcaseshare.cn/). A phone, tablet, or office computer can be the monitoring device.

![AgentBoard sign-in entry](images/agentboard-login.png)

Figure 1: the unauthenticated AgentBoard entry. Select Continue to Agent Case Share login or create an account. The screenshot contains no real account information.

### Non-default directories or CLI paths

Set environment variables before starting the Connector when an Agent uses a non-default directory or when its CLI is not on PATH. Windows PowerShell example:

~~~powershell
$env:CODEX_HOME = "D:\AI\codex"
$env:AGENTBOARD_CLAUDE_HOME = "D:\AI\claude"
$env:AGENTBOARD_GEMINI_HOME = "D:\AI\gemini"
$env:AGENTBOARD_CODEX_BIN = "C:\Tools\codex.cmd"
$env:AGENTBOARD_CLAUDE_BIN = "C:\Tools\claude.cmd"
$env:AGENTBOARD_GEMINI_BIN = "C:\Tools\gemini.cmd"
.\ai-connector.exe observe serve
~~~

On Linux/macOS, use export CODEX_HOME=... and the corresponding AGENTBOARD_* variables. If you run the Connector as a background service, configure these variables in the service environment rather than only in a temporary terminal.

## 3. Sign In and Generate a One-Time Pairing Code

1. Open [AgentBoard](https://aiboard.agentcaseshare.cn/) in the Computer B browser.
2. Sign in with an Agent Case Share account, or register a new one.
3. Open Devices and select Pair device.
4. The page displays a one-time code and a command similar to:

~~~text
ai-connector pair --code ABCD1234
~~~

![AgentBoard device binding and one-time pairing code](images/agentboard-pairing.png)

Figure 2: the pairing-code area of the Devices page. ABCD1234 is a screenshot-only example; use the code generated by your own page.

The code is valid for **10 minutes** by default and becomes invalid immediately after it is claimed. It is a first-binding invitation, not a long-term password. Do not post it in public chats, tickets, screenshots, or repositories.

An account normally allows one bound device. To replace a computer, unbind the old device first and then generate a new code. Unbinding invalidates the old credentials and disconnects its current connection.

## 4. Pair Computer A

### Recommended: enter the code interactively

On Computer A, open a terminal in the Connector directory and run:

~~~powershell
.\ai-connector.exe pair
~~~

The program prompts Enter the one-time pairing code:. Paste the code shown on Computer B and press Enter. Interactive entry avoids placing the code directly in shell history.

You can also pass the code explicitly:

~~~powershell
.\ai-connector.exe pair --code "ABCD1234"
~~~

Linux/macOS:

~~~bash
./ai-connector pair --code "ABCD1234"
~~~

After pairing, the Connector generates random device_id, credential_id, and an Ed25519 key pair locally. It submits only the public key to AgentBoard. The Agent Case Share password is not passed to the Connector and is not stored in its configuration.

### Configuration and private-key location

The default configuration file is stored here:

| Platform | Path |
| --- | --- |
| Windows | %APPDATA%\AgentBoard\connector.json |
| Linux | ~/.config/AgentBoard/connector.json |
| macOS | ~/Library/Application Support/AgentBoard/connector.json |

Windows uses DPAPI to protect the private key. Unix systems use a configuration file with 0600 permissions. Do not copy, commit, or publish this file. When changing computers, pair the new computer instead of copying the old private key.

## 5. Start the Local Observer

Start Codex, Claude Code, or Gemini CLI once on Computer A so that a local session record exists. Then run:

~~~powershell
.\ai-connector.exe observe serve
~~~

The command:

1. Scans the three local Agent session directories.
2. Reads new or changed JSONL lines and reduces them into status fields.
3. Connects to wss://aiboard.agentcaseshare.cn/v1/connectors/ws.
4. Completes the device challenge signature and authentication.
5. Waits for the dashboard subscription before sending snapshots and deltas.

For the first verification, leave the command running in the foreground. In another terminal, inspect locally discovered sessions:

~~~powershell
.\ai-connector.exe observe status
~~~

Inspect pairing state and detected directories:

~~~powershell
.\ai-connector.exe status
~~~

When Computer B shows the device as Online, open Live sessions and select that device.

After pairing succeeds, the Devices page shows the device name, operating system, architecture, Connector version, online state, and last heartbeat:

![AgentBoard bound device list](images/agentboard-devices.png)

Figure 3: the bound-device list. The device name and timestamp are example data; a real page shows the information supplied during pairing.

### Run as a background service

For continuous monitoring, install the current-user service:

~~~powershell
.\ai-connector.exe service install
.\ai-connector.exe service start
.\ai-connector.exe service status
~~~

Linux uses the same subcommands:

~~~bash
./ai-connector service install
./ai-connector service start
./ai-connector service status
~~~

The service runs only the fixed observe serve command. It does not accept arbitrary shell commands or extra parameters from the browser. Stop or remove it with:

~~~text
ai-connector service stop
ai-connector service uninstall
~~~

## 6. Use the Dashboard from Computer B

### Live status

The Live sessions page shows a status projection, not a complete transcript. Filter by Agent or status to see which task is still running, which has finished, and whether a tool is currently active.

![AgentBoard live sessions](images/agentboard-live-sessions.png)

Figure 4: the relevant Live sessions area. The example shows the online device, Agent, status, working directory, source, recent activity, and current tool.

The public session_id is an opaque hash projection generated from the Agent and the local native ID. The native ID stays on Computer A for local resume commands, so equal IDs from Codex, Claude Code, and Gemini CLI cannot collide in the dashboard.

### View details

Only when the device owner explicitly selects View details does the Connector read bounded content from the local JSONL file on Computer A. It sends that content through the currently authenticated realtime connection to the current browser. The detail view has event and total-size limits and is not retained as history after the page is closed.

Details are not uploaded by default and do not expose the whole log directory. Claude Code tool-call details show the tool name and Tool invocation, not the tool input parameters. Default status updates also exclude reasoning content, raw JSONL lines, and local file contents.

### Continue an original session

Sessions with idle, completed, aborted, or unknown status and a working directory can be continued from the dashboard. Enter a new prompt and the Connector constructs a fixed local command for the selected Agent:

~~~text
Codex        -> codex exec resume --skip-git-repo-check <native ID> <prompt>
Claude Code  -> claude --resume <native ID> --print <prompt>
Gemini CLI   -> gemini --resume <native ID> --prompt <prompt>
~~~

The browser cannot choose an arbitrary binary, working directory, shell fragment, or extra flag. An active session is never force-injected, which avoids competing with a user typing on Computer A. Managed processes currently support cancellation; cancellation applies only to the resume process started by the Connector.

## 7. How It Works

### The Connector is a local observer, not a remote desktop

The Connector runs on Computer A and reads local session files through separate adapters:

- The Codex adapter observes CODEX_HOME/sessions/**/*.jsonl.
- The Claude Code adapter observes AGENTBOARD_CLAUDE_HOME/projects/**/*.jsonl.
- The Gemini CLI adapter observes AGENTBOARD_GEMINI_HOME/tmp/**/chats/*.jsonl and reads projects.json locally to map a project hash back to a working directory.

Adapters discover sessions, tail JSONL files, reduce allowlisted status fields, and parse details on demand. They do not expose the log directory as an HTTP file service and do not listen on a public inbound port.

### Outbound WebSocket delivery

Computer A initiates a TLS-encrypted wss:// connection to AgentBoard. Networks that allow outbound traffic but block inbound traffic therefore usually need no port forwarding or public IP. After the connection opens, AgentBoard sends a device challenge; the Connector signs it with its local Ed25519 private key and the server verifies it with the public key registered during pairing.

The logged-in browser on Computer B subscribes to the device. AgentBoard forwards data only over this authorized account/device connection. When the browser closes, the Connector exits, or the network disconnects, the realtime stream ends.

### Combining multiple Agents

The three local adapters produce one privacy-safe session model. multiobserver adds the agent field and generates an opaque public session ID, allowing the dashboard to show Codex, Claude Code, and Gemini CLI together while preserving each CLI's local resume command.

### Why the server does not retain concrete content

The server provides authentication, device authorization, and transient realtime relay, not a transcript archive. Default status updates use a field allowlist containing only minimal device/session identifiers, working directory, source, status, timestamps, tool name, and tool status. Realtime observations stay in API memory and the current browser connection instead of being written to historical server storage.

The View details action is also an explicit, temporary request. The detail is read on Computer A, returned over the authenticated connection, and not written to the server database. The server does not retain:

- Complete Codex, Claude Code, or Gemini CLI session records.
- User prompts, model reasoning, or raw JSONL lines.
- Local file contents.
- Tool-call arguments or tool output as server history. Claude Code tool details are name-level only.
- Computer A's private key or one-time pairing code.

This means the server does not retain concrete usage content from paired devices. Device management and revocation may still require minimal control-plane metadata, such as a device identifier, device name, operating system/architecture, credential status, and heartbeat. That metadata is not a transcript and cannot reconstruct local files or a complete conversation. A working directory can reveal a project name, so pair only a device whose path metadata you accept sharing with the signed-in AgentBoard account.

The Connector may also create a local observer metadata database on Computer A. It stores offsets, file fingerprints, session associations, and managed-process state for incremental observation after a restart. It does not store JSONL transcripts, prompts, reasoning, tool arguments, tool output, or local file contents.

## 8. Security Recommendations

1. Download the Connector from a trusted Release or build it from source, and verify the version.
2. Transfer pairing codes only briefly between Computer B and Computer A; generate a new one after expiry.
3. Never share connector.json, the private key, shell history, or screenshots containing a pairing code.
4. Use a strong AgentBoard password and protect the browser login session. Anyone with the account can see its authorized device status.
5. Unbind a computer from the Devices page, then stop and uninstall its local service when it is no longer needed.
6. Confirm that you accept working-directory visibility when project names are sensitive.
7. If a corporate proxy or TLS inspection appliance is involved, confirm its logging policy. The Connector-to-AgentBoard link itself uses TLS.

## 9. Troubleshooting

### The pairing code is invalid or expired

Return to Devices, generate a new code, and run pair --code within ten minutes. Each code can be claimed only once.

### The device remains offline

Run ai-connector service status on Computer A, or start ai-connector observe serve in the foreground. Run ai-connector doctor to check API health. Firewalls, corporate proxies, incorrect system time, and TLS inspection can block outbound WSS.

### No sessions appear

Confirm that the CLI runs under the same user on Computer A and that its session directory exists. Run ai-connector doctor --json to see the three directories detected by the Connector. Set CODEX_HOME, AGENTBOARD_CLAUDE_HOME, or AGENTBOARD_GEMINI_HOME for non-default locations.

### View details fails

The local log may have been deleted, be in the middle of a write, or use an unrecognized format. The Connector returns a bounded error and does not upload the file elsewhere.

### Resume fails

Confirm that the selected CLI is still installed on PATH, that the native session still exists, and that it is not active. Set AGENTBOARD_CODEX_BIN, AGENTBOARD_CLAUDE_BIN, or AGENTBOARD_GEMINI_BIN when a wrapper or non-default binary is needed. The Connector does not run a fallback command after a resume failure.

## 10. Stop Monitoring or Replace the Computer

Temporarily stop the service:

~~~powershell
.\ai-connector.exe service stop
~~~

Remove the current-user service:

~~~powershell
.\ai-connector.exe service uninstall
~~~

Then unbind the device from the AgentBoard Devices page. To replace a computer, install the Connector on the new computer, generate a new pairing code, and run pair again. Do not copy the old configuration file or private key. The old credentials can then be revoked and the new computer receives an independent identity.

## Related Links

- [AgentBoard dashboard](https://aiboard.agentcaseshare.cn/)
- [AI Connector repository](https://github.com/parkerluxu/ai-connector)
- [Chinese tutorial](using-ai-connector-with-agentboard.zh-CN.md)
- [Multi-agent observation architecture](../architecture/multi-agent-observation.md)
- [Privacy](../../PRIVACY.md)
- [Security](../../SECURITY.md)
