# 原生 LSP 与 Windows Accessibility — 第二阶段交付

日期：2026-09-10。工作版本：`0.2.0-runtime-core.1`。
源码分支：`runtime/lsp-desktop`，从第一阶段 `c610d636b3c85f2e84c9b58210f2d1f702d193ee` 继续。
最终提交以该分支 `git log -1` 及交付包 build-info.json 为准。

## 本轮实现

LSP 位于 `internal/coding/lsp`，由同一个 `app.Runtime` 管理。
`lsp_manage` 负责发现、启动、状态、停止；`lsp_query` 提供 definition、references、
hover、diagnostics、document symbols、workspace symbols、incoming/outgoing call hierarchy。

进入 Coding 项目后使用既有 task_id，不建立平行任务身份。一个 workspace/language
复用一个真实 language server，底层使用原有 process.Controller 管理进程树。
JSON-RPC、Content-Length、初始化、服务端配置请求、诊断与文档同步均为 Go 原生实现，
不依赖 ACP、Codex、WebCodex Server、编辑器插件或运行中的 VS Code。

每次查询从磁盘同步该文件及已打开文件。输入 line 从 1 开始，character 为从 0 开始
的 UTF-16 offset；输出保留 LSP 的 0-based range。代码修改后重新查询可以得到更新
的结果。尚未实现全项目文件监视；新增/重命名依赖、外部配置或未打开文件的结构变化
可能需要显式 stop 后重新查询。诊断 pending 不等于 clean；无版本的 push 诊断会
说明其时效限制。语言服务器退出可重启，但不会重跑 Coding 命令或启动模型。

Windows Desktop 位于 `internal/computer/desktop`，使用原生 UI Automation COM
与 Win32 API。支持应用/窗口清单、ControlView accessibility tree、语义查找、焦点、
Invoke、Value、Scroll、Toggle、SelectionItem、Unicode 剪贴板与 PNG 截图。

新增工具为：`desktop_inspect`、`desktop_act`、`desktop_clipboard`、`desktop_screen`。
动作限定在明确 HWND 下，组合 runtime_id / automation_id / name / control_type；
必须匹配唯一元素。没有匹配、歧义、不支持对应 Pattern 时返回明确错误，不暗中切换
坐标点击或键盘输入。type 是 ValuePattern 的整值替换，不是键盘事件；press 是
InvokePattern，不是任意键盘按键。密码控件值不读取。截图发布与任务附件复用既有
publicartifacts，stdio 模式不伪造公共下载 URL，可通过同一 Runtime 的 view_image 读取。

该实现不是“PowerShell 代替 native backend”：只有测试窗口使用 PowerShell/WPF；
运行时的 Accessibility、截图和剪贴板实现均直接位于 Go/Windows 源码中。

## 本机依赖与验证结果

已安装到 `C:\dev\runtime-core-state\tools`，没有改变业务项目的依赖：

| 能力 | 实际版本 |
|---|---|
| Go language server | gopls 0.23.0 |
| TypeScript/JavaScript language server | typescript-language-server 6.0.0 |
| TypeScript 分析 SDK | 5.9.3 |
| Python language server | Pyright 1.1.414 |
| Go 工具链 | 1.26.5，C:\dev\_tools\go1.26.5\go\bin\go.exe |
| Node.js | 24.15.0 |

本机 JobPilot 的 TypeScript 7.0.2 包没有传统 tsserver.js，因此 Runtime 独立固定
经典 5.9.3 分析 SDK。它不冒充项目 TS7 编译器；编译、测试仍必须运行项目原生命令。
Rust 的 PATH 可执行入口能够被发现，但本轮没有原生 Rust 服务验收；Java/Kotlin
不是已交付能力。可在机器本地 lsp-servers.json 配置其他真实 LSP server。

实际日志在 `C:\dev\runtime-core-artifacts\phase2`：

- `lsp-01.log`：4 个顶层测试通过，含 Go/TS/Python 3 个真实语言子测试。每种语言
  运行七类查询、错误诊断修复、进程复用和停止；另有真实服务进程退出重启测试。
- `desktop-01.log`：首次原生 COM/Win32 自有窗口验收通过。
- `integration-02.log`：最终回调生命周期修复后 15 个顶层测试通过，含新工具 handler/
  schema、旧 Coding 工作流回归以及真实 Desktop 场景。没有用重复测试冒充新增用例。
- `mcp-acceptance.json`：通过当前 ChatGPT -> 原 AgentDock Dynamic MCP -> 新 Runtime
  的真实调用记录。原生验收任务为 `tsk_d18201f7a5aa830c`。

真实 MCP 验收读取了 Runtime 自身 Go 源码符号，并在专用窗口输入
“中文 MCP 原生桌面验收”，调用 Invoke 后由应用写出了完全相同的结果文件。
截图 `3eae6d0efc473ee7b9694e387488a7be` 通过同一 Runtime 的 view_image 读取，
确认显示输入与 applied 状态，而不是空白 PNG。测试窗口随后由其专属进程 Session 关闭。
没有操作用户业务应用或已有文档；没有为了测试覆盖用户的全局剪贴板。

真实 Desktop 测试覆盖窗口/树/查找、中文输入、焦点、按钮的应用侧效果、选中状态、
Toggle、Scroll 调用、歧义拒绝与截图。剪贴板读写已实现，但没有在用户当前剪贴板上
执行破坏性验收；不声称测遍所有应用或所有 UIA provider。

## 本机安装与现有 GPT 入口

参见 `INSTALL.md`。独立安装位置：

```text
%LOCALAPPDATA%\RuntimeCore\bin\runtime-core.exe
%LOCALAPPDATA%\RuntimeCore\scripts\Start-RuntimeCore.ps1
%LOCALAPPDATA%\RuntimeCore\install.json
C:\dev\runtime-core-state\lsp-servers.json
```

现有 `runtime-core-preview` Dynamic MCP 注册已改为独立安装路径，保持 ACP=false。
四个已有项目与 Task state 仍在原独立 home。官方 AgentDock 二进制、托盘、现有
ChatGPT 连接和业务项目代码没有被替换。

## 明确边界

Desktop 当前仅 Windows amd64，需当前交互用户桌面；没有 macOS AX、Linux AT-SPI、
锁屏桌面、UAC 安全桌面支持，也没有坐标点击后备或通用键盘事件接口。UIA 的可操作性
取决于应用真正暴露的 accessibility pattern；视觉控件不保证天然有语义接口。

多机器 Runner、Project OS 自动事件投递/审核状态同步、新托盘/自动升级仍未完成。
本轮安装器不创建 Windows 服务或无人登录自动启动；session-0 服务不适合作为当前
用户桌面的交互执行器。LSP 与 Desktop 不消耗 Codex 执行 Agent 配额，因为本实现
没有调用该执行链；正常 ChatGPT 对话本身的使用限制与计费不是 Runtime 的决定范围。

## 参考实现与一手资料

保留 WebCodex 参考提交和 AgentDock 归属信息，未复制其 Rust Server/Plugin 系统。
UIA COM slot 来自本机 Windows SDK UIAutomationClient.h；新增 Windows 源码与测试
保留 Apache-2.0 标记。协议和安装资料：

```text
https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/
https://go.dev/gopls/
https://github.com/typescript-language-server/typescript-language-server
https://github.com/microsoft/pyright
https://learn.microsoft.com/en-us/windows/win32/winauto/uiauto-controlpatternsoverview
https://developers.openai.com/plugins/deploy/connect-chatgpt
```
