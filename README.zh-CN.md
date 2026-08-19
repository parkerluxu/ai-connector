# AI Connector 中文使用说明

AI Connector 是运行在 AI Agent CLI 所在电脑上的本地连接器。它读取本机 Codex、
Claude Code 和 Gemini CLI 会话日志，提取会话状态，并通过加密的出站 WebSocket
发送到 [AgentBoard 看板](https://aiboard.agentcaseshare.cn/)，让你在浏览器中查看实时会话。
它不是这些工具的替代品，也不会把本地终端暴露成公网服务。支持的适配器、续聊语义和
隐私边界见 [`docs/architecture/multi-agent-observation.md`](docs/architecture/multi-agent-observation.md)。

本文对应当前公开版本的实际流程：Agent Case Share 负责账号登录/注册，
AgentBoard 负责设备配对和实时看板，AI Connector 负责本地观察与连接。

## 一分钟看懂流程

```text
浏览器打开 AgentBoard
    ↓
Agent Case Share 登录或注册（统一账号）
    ↓
设备页生成 10 分钟有效的一次性配对码
    ↓
在 Codex 所在电脑运行 ai-connector pair --code <配对码>
    ↓
本地保存设备凭据并启动 ai-connector observe serve
    ↓
看板“实时会话”页显示该电脑上的 Codex 会话
```

Connector 只发起出站 HTTPS/WSS 连接，因此通常不需要为电脑开放入站端口。
配对码只用于首次绑定；服务启动后的长期认证使用本地生成的 Ed25519
设备密钥，不使用 Agent Case Share 密码或 API Key。

## 支持平台与安装

每个 [GitHub Release](https://github.com/parkerluxu/ai-connector/releases) 都提供
Windows 和 Linux 的独立可执行文件，不需要解压。请下载与系统和 CPU 架构匹配的
文件：

| 平台 | 发布文件 |
| --- | --- |
| Windows x64 | `ai-connector_<version>_windows_amd64.exe` |
| Windows ARM64 | `ai-connector_<version>_windows_arm64.exe` |
| Linux x64 | `ai-connector_<version>_linux_amd64` |
| Linux ARM64 | `ai-connector_<version>_linux_arm64` |

Linux 下载后先授予执行权限。macOS 用户可从源码构建；当前 Release 不提供 macOS
预编译文件。

```bash
chmod +x ai-connector_<version>_linux_<arch>
```

```powershell
# Windows PowerShell
.\ai-connector.exe version
```

```bash
# Linux（macOS 从源码构建后同样使用此命令）
./ai-connector version
```

从源码构建需要 Go 1.24 或更高版本：

```bash
go test ./...
go build ./cmd/ai-connector
```

首次使用建议先执行诊断。`doctor` 会检查 API 健康状态、配对状态和本机
Codex session 目录：

```powershell
.\ai-connector.exe doctor
```

## 1. 登录或注册 AgentBoard

1. 打开 [https://aiboard.agentcaseshare.cn/](https://aiboard.agentcaseshare.cn/)。
2. 未登录时会看到 **登录** 和 **注册** 两个入口。
3. 点击 **登录**，使用已有的 Agent Case Share 账号完成授权；首次使用点击
   **注册**，注册页面仍由 Agent Case Share 提供。
4. 授权完成后会自动返回 AgentBoard。看板不会创建第二套密码账号，浏览器
   使用看板自己的 HttpOnly 会话保持登录。

如果页面反复回到登录页，先确认浏览器允许该站点 Cookie，并检查系统时间和
网络连接；登录回调状态过期时重新从首页发起登录即可。

## 2. 在网页生成配对码

1. 登录后进入 **设备** 页面。
2. 点击 **绑定设备**。
3. 页面会显示一次性配对码和对应命令，例如：

   ```text
   ai-connector pair --code ABCD1234
   ```

4. 配对码默认有效 **10 分钟**，成功认领后立即失效。不要把配对码发到公开
   聊天、工单或脚本中。

账号默认最多绑定一台设备。已有设备时，需要先在设备页 **解绑**，再为新电脑
生成配对码；解绑会立即使旧设备凭据失效并断开连接。

## 3. 在 Codex 所在电脑完成配对

在安装了 Connector、并且能访问互联网的同一台电脑上执行。建议在交互式终端
中输入配对码，这样不会回显到屏幕或命令历史：

```powershell
# Windows
.\ai-connector.exe pair
```

程序会提示 `Enter the one-time pairing code:`，粘贴网页上的配对码并回车。
也可以显式传参：

```powershell
.\ai-connector.exe pair --code "ABCD1234"
```

Linux/macOS 使用 `./ai-connector pair` 或 `./ai-connector pair --code "ABCD1234"`。

配对成功后，Connector 会生成或复用本机 `device_id`、`credential_id` 和
Ed25519 密钥，并向 `/v1/devices/pair/claim` 提交公钥。私钥只保存在本机。
默认配置路径为：

| 系统 | 默认路径 |
| --- | --- |
| Windows | `%APPDATA%\AgentBoard\connector.json` |
| Linux | `~/.config/AgentBoard/connector.json` |
| macOS | `~/Library/Application Support/AgentBoard/connector.json` |

Windows 新安装会使用 DPAPI 保护私钥；Unix 系统使用权限为 `0600` 的配置文件。
不要复制、提交或公开该文件。

## 4. 启动本地观察服务

先确认本机 Codex 正常运行过至少一个会话，然后启动 Connector：

```powershell
# 前台运行，适合首次验证
.\ai-connector.exe observe serve
```

另开终端检查发现的本机会话：

```powershell
.\ai-connector.exe observe status
```

查看连接器配置和设备身份摘要：

```powershell
.\ai-connector.exe status
```

确认网页设备状态变为 **在线** 后，回到 **实时会话** 页面，选择该设备即可看到
会话快照和后续增量状态。若没有会话，请先在同一台电脑启动 Codex，并确认
`CODEX_HOME`（如有设置）下存在 `sessions` 目录。

### 设置为后台服务

Connector 提供当前用户级别的服务安装，不需要把 Connector 部署到云服务器：

```powershell
# Windows：写入当前用户的启动项
.\ai-connector.exe service install
.\ai-connector.exe service start
.\ai-connector.exe service status
```

```bash
# Linux：systemd user unit
./ai-connector service install
./ai-connector service start
./ai-connector service status
```

停止或移除服务：

```text
ai-connector service stop
ai-connector service uninstall
```

服务只执行固定的 `observe serve`，不会接受任意 Shell 命令或工具参数。

## 5. 看板功能

### 实时会话

- 查看选中设备上的 Codex 会话状态：活动中、空闲、已完成或已中止。
- 查看工作目录、会话来源（CLI/Desktop）、最近活动时间和当前工具状态。
- 可按会话目录或工作目录整理列表。
- 打开 **查看详情** 时，Connector 按需读取本地会话记录，并只通过当前实时
  连接返回浏览器；详情不写入服务端数据库。
- 对可继续的已结束会话使用 **继续原会话**，在本机启动 Codex resume 进程；
  运行中可以请求终止该受管进程。

### 设备管理

- 查看设备名称、系统、架构、Connector 版本、在线状态和最后心跳。
- 生成一次性配对码、绑定新电脑。
- 重命名设备，或在换机/遗失时解绑设备。
- 解绑或管理员吊销后，现有 WebSocket 会被断开，旧凭据不能再次认证。

## 6. 数据与安全边界

默认的实时状态同步只发送设备身份、会话 ID、工作目录/会话目录、来源、状态、
时间戳和当前工具名称/状态。

当设备所有者在看板中主动点击 **查看详情** 时，Connector 才会按需读取该本地
会话的一部分消息、工具调用和工具结果，并通过当前已认证的实时连接转发到该
用户的浏览器。详情不会写入 Connector 元数据或服务端数据库，页面关闭后不作为
历史记录保留。

以下内容不会作为默认状态上传，且不会持久化到看板数据库：

- 推理内容和原始 JSONL 行；
- 本地文件内容。

实时观察状态只在 API 内存和当前浏览器页面中转发，不作为历史记录持久化。完整
边界请阅读 [PRIVACY.md](PRIVACY.md)。发现安全问题请按 [SECURITY.md](SECURITY.md)
中的方式私下报告。

## 7. 常见问题

### `pair` 提示配对码无效或已过期

回到设备页重新生成配对码，并在 10 分钟内执行 `pair --code`。每个配对码只能
使用一次，注意不要包含引号或前后空格以外的字符。

### 网页显示设备离线

在目标电脑运行 `ai-connector service status` 或重新执行
`ai-connector observe serve`；再运行 `ai-connector doctor` 检查 API 健康状态。
公司代理、防火墙或 TLS 检查设备也可能阻止 WSS 出站连接。

### `observe status` 找不到会话

确认 Codex 在当前用户下运行，并检查 `CODEX_HOME\sessions`（Windows）或
`$CODEX_HOME/sessions`（Linux/macOS）。如果没有设置 `CODEX_HOME`，Connector
默认读取当前用户主目录下的 `.codex/sessions`。

### 换电脑或重装系统

在原设备仍可用时先在网页解绑；新电脑重新安装 Connector、生成新配对码并执行
`pair`。不要复制旧电脑的私钥文件来“迁移”设备。

## 相关链接

- [AgentBoard 看板](https://aiboard.agentcaseshare.cn/)
- [使用 AI Connector 监控 Codex、Claude Code 和 Gemini CLI 的详细教程](docs/guides/using-ai-connector-with-agentboard.zh-CN.md)
- [英文 README](README.md)
- [隐私说明](PRIVACY.md)
- [安全策略](SECURITY.md)
- [发布与升级说明](RELEASING.md)
