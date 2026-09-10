# 本机安装、运行与 ChatGPT 接入

## 这台机器现在怎么用

本轮已建立独立 RuntimeCore 安装，继续使用 `C:\dev\runtime-core-state`，不替换
`%LOCALAPPDATA%\AgentDock`。Go/TypeScript/Python language server 和配置已安装。
现有 `runtime-core-preview` Dynamic MCP 入口指向新安装，不必再手动开一个实例。

当前调用路径：

```text
ChatGPT 网页版已有 AgentDock 连接
  -> AgentDock 的 mcp_tool_search / inspect / call
  -> runtime-core-preview
  -> 本机 runtime-core.exe
  -> 原生 Coding / LSP / Browser / Windows UI Automation / 可选 ACP Adapter
```

第三阶段还加入了 Runtime Chrome Bridge；它属于 Browser 的可选 transport，不是另一套插件协议。
正常 Runtime Browser 默认使用原生 CDP；需要控制用户已登录的真实 Chrome tab 时再选择
`transport=extension`。这个外层 AgentDock 转发仍只是过渡接入方式，不是 Runtime 核心对旧 AgentDock 的永久依赖。

以后可直接在对话中要求：

```text
使用 runtime-core-preview 的原生工具进入 jobpilot；
先用 work_on_project 获取 task_id，再用 LSP 查找相关定义和引用。
普通编码直接完成，不调用 ACP/Codex。
```

Desktop 操作先调用 desktop_inspect 获取当前 window_handle 和真实语义元素，再
调用 desktop_act；操作系统 runtime_id 不能跨窗口重建后长期缓存。

## 从源码重新构建与安装

先禁用自己的 `runtime-core-preview`，不要停止官方 AgentDock 或其他 MCP。
从仓库目录运行以下 PowerShell 命令：

```powershell
Set-Location C:\dev\runtime-core
$go = 'C:\dev\_tools\go1.26.5\go\bin\go.exe'
$binary = 'C:\dev\runtime-core-artifacts\phase3\runtime-core.exe'
& $go build -trimpath -o $binary ./cmd/agentdock
if ($LASTEXITCODE -ne 0) { throw 'Build failed' }

.\scripts\runtime\Install-RuntimeCore.ps1 `
  -Binary $binary `
  -RuntimeHome 'C:\dev\runtime-core-state' `
  -ProjectRoot 'C:\dev\runtime-core'
```

安装器复制到 `%LOCALAPPDATA%\RuntimeCore`，保留上一份已安装 binary 为
`bin\runtime-core.previous.exe`，并同步 `chrome-extension`、生成 Native Messaging host manifest、
在当前用户 HKCU 注册 `com.runtime.browser_bridge`。它不动项目登记、任务或 LSP 配置，也不创建系统服务。
目标 Runtime 进程仍在运行时它会报错，不会擅自杀进程。安装后重新启用自己的 Dynamic MCP。

从交付包安装时，保留包内 `scripts/runtime` 目录结构，把上面 Binary 参数改为
解压目录中的 `runtime-core.exe`。这里的安装不需要管理员权限。

## Dynamic MCP 本机注册值

已存在同名入口时不重复添加。仅在新机器首次注册，或有意重新指向安装路径时使用：

```text
name:      runtime-core-preview
transport: stdio
command:   C:\Users\jingx\AppData\Local\RuntimeCore\bin\runtime-core.exe
args:      ["--stdio"]
cwd:       C:\dev\runtime-core
timeout:   60000 ms

isolated environment:
AGENTDOCK_HOME=C:\dev\runtime-core-state
AGENTDOCK_DEFAULT_DIR=C:\dev\runtime-core
AGENTDOCK_ACP_ENABLED=false
AGENTDOCK_BROWSER_ENABLED=true
```

路径中的用户目录应替换为新机器真实的 `%LOCALAPPDATA%`。AgentDock registry 的
command 字段使用绝对路径，不假设会展开 `%LOCALAPPDATA%` 文本。
本轮已使用原生 mcp_manage 完成注册和环境设置；无需你重复配置。

官方 AgentDock 的安装/登录方式决定外层连接如何随电脑启动恢复。这一轮没有
新建 Runtime 独立开机任务，也没有修改官方托盘启动设置。

## Runtime Chrome Bridge

安装器已经把扩展复制到：

```text
%LOCALAPPDATA%\RuntimeCore\chrome-extension
```

并注册 Native Messaging host。Chrome 对本机未上架 unpacked extension 的首次持久加载需要浏览器确认：
打开 `chrome://extensions`，启用 Developer mode，选择 **Load unpacked**，指向上面的目录。
固定 Extension ID 为 `agidgjchdiodbkkaggifpflepjgoedff`；不要重新生成 manifest key，否则 Native Messaging allowed origin 会失配。

扩展加载后，`browser_session {action:"extension_status"}` 可以只检查连接；需要复用当前已登录 Chrome 时，
使用 `browser_session {action:"start", transport:"extension"}`。`show_cursor` 默认开启：页面内会显示青色发光光标，当前 Runtime 工作 tab 会临时显示 `✦ Runtime ·` 标题/发光 favicon，并在原本未分组时加入同一窗口的青色 `Runtime` 标签组；关闭 session 后自动恢复。已有用户标签组不会被替换。可用 `show_cursor:false` 关闭这些视觉标识。
不需要复用登录态时优先使用默认 native CDP transport。

## 新机器首次安装语言服务器

已经配置好的这台机器无需再次运行。新机器先准备 Go 和 Node.js；本轮验证工具链
为 Go 1.26.5、Node 24.15.0，Node 至少 22.22.2。下面命令安装本轮已验证的固定版本：

```powershell
& "$env:LOCALAPPDATA\RuntimeCore\scripts\Install-LanguageServers.ps1" `
  -RuntimeHome 'C:\dev\runtime-core-state' `
  -GoExecutable 'C:\dev\_tools\go1.26.5\go\bin\go.exe'
```

脚本把 server 放在 Runtime home 下，不在 JobPilot / Project OS / TGN 执行 npm install。
已存在 `lsp-servers.json` 时拒绝覆盖；升级服务器或改变 SDK 应明确编辑这份配置。
修改配置后停止对应语言服务器，再次查询时重新读取。Go 的配置 PATH 必须能找到
对应 go.exe；Node 服务用 node.exe + JS entrypoint，不把 .cmd 当原生进程启动。

TypeScript LSP 本轮使用 5.9.3 分析 SDK，并不替代实际项目的编译版本或构建验证。

## 不经过原 AgentDock，单独运行

这是可选的本地调用方式；不要与 Dynamic MCP 实例同时执行同一个任务。

```powershell
# 本地 MCP stdio 客户端
& "$env:LOCALAPPDATA\RuntimeCore\scripts\Start-RuntimeCore.ps1"

# 或，本机 HTTP 客户端；前台保持运行
& "$env:LOCALAPPDATA\RuntimeCore\scripts\Start-RuntimeCore.ps1" -Transport http -Port 8766
```

HTTP launcher 只绑定 `127.0.0.1`，地址为 `http://127.0.0.1:8766/mcp`。
它不是新的公网地址；不能直接把这个 localhost URL 当成 ChatGPT 网页版可达的端点。
窗口与桌面功能需要在目标用户已登录、桌面可交互的会话里运行。不要把它装成
session-0 Windows service，再期待它控制登录用户的应用；锁屏/UAC安全桌面未实现。
一般应用也不保证有 UIA Pattern，缺少语义接口会明确报错，不强行坐标点击。

## 以后建立独立 ChatGPT Runtime 连接

当前没有必要；彻底退出旧 AgentDock 转发时再做。根据 2026-09-10 查阅的 OpenAI
官方文档，在账户/工作区允许时：

1. Settings -> Security and login -> Developer mode。
2. 进入 ChatGPT Plugins，选择加号，填名称 Runtime Core 和说明。
3. Connection 选择可达的 HTTPS MCP endpoint（含 `/mcp`），或已配置好的 Secure
   MCP Tunnel。这里需要真实端点或 tunnel_id，不能填本机 exe 路径。
4. 完成相应认证，检查工具列表包含 work_on_project、lsp_query、desktop_inspect 等。
5. 更新工具元数据后部署/重启服务，在连接上选择 Refresh，开启新对话验证。

本轮没有创建新的公网 Runtime URL、OAuth 凭据或 Secure MCP Tunnel，也没有
修改你的 ChatGPT 设置。因此不要从本文猜一个不存在的地址填进去。
自有公网部署必须保留现有认证边界；不要把无认证的本机执行能力直接暴露到公网。

网页版 UI 可能随账户/工作区而不同；本条请求未收到设置截图，本文不是对某张
截图的逐栏确认。当前确定可用的是前述已有 AgentDock -> Dynamic MCP 路径。
官方步骤来源：

```text
https://developers.openai.com/plugins/deploy/connect-chatgpt
```

## 状态、产物与卸载边界

任务和项目在独立 Runtime home；publicartifacts 沿用已有最长七天保留期。
stdio 模式的图片可以通过同一 Runtime 的 view_image 读取；需要公网下载时，
使用已有可达 AgentDock 的 file_publish 发布本机产物，不把空 URL 当下载链接。

停用 Dynamic MCP 并删除独立安装目录即可停止使用本版本。不要自动删除
`C:\dev\runtime-core-state`：其中含项目登记、任务和本机配置。回退时只替换已停止
运行的独立 binary；官方安装和第一阶段源码历史均保留。
