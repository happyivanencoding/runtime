# Codex prompt — connect Runtime to ChatGPT Web

> Send the block below to Codex on the Windows machine that runs Runtime.

```text
你现在直接在这台 Windows 电脑上完成 Runtime → ChatGPT 网页版的正式连接、插件/App/MCP 设置，以及电脑控制相关的本机配置和真实 E2E 验收。

这不是“写教程”的任务。能安全自动完成的步骤直接完成；只有登录、2FA、重新授权、安全确认这类必须由本人操作的步骤才暂停让我接手。

====================
0. 权威现状：先核验，不要重建
====================

项目：
C:\dev\runtime-core

GitHub：
https://github.com/happyivanencoding/runtime

branch：
main

当前 Runtime Core 安装：
%LOCALAPPDATA%\RuntimeCore\bin\runtime-core.exe

Runtime state：
C:\dev\runtime-core-state

Runtime Chrome Bridge：
Extension ID = agidgjchdiodbkkaggifpflepjgoedff
Native Messaging Host = com.runtime.browser_bridge

Runtime 已有：
- MCP stdio transport
- Coding / Git
- Go / TypeScript / Python LSP
- Browser CDP
- Runtime Chrome Bridge（chrome.debugger + Native Messaging）
- tab / URL / DOM
- click / fill / type / select / upload
- screenshot
- network / download
- glowing visual cursor
- Runtime 工作 tab 的特殊标识
- Windows UI Automation fallback
- ACP high-level adapters：
  - acp_start
  - acp_resume
  - acp_status
  - acp_stop

现在还新增了 Runtime 自己的 Windows 客户端，不要另做第二套控制面板：
source：
C:\dev\runtime-core\desktop\windows\runtime-control

正式安装目标：
%LOCALAPPDATA%\RuntimeCore\control\Runtime.Control.exe

客户端名称：
Runtime Control

它已经负责显示：
- Runtime Core
- MCP
- ChatGPT Direct MCP
- Chrome Bridge
- ACP
- Windows UIA
- AgentDock fallback
- 日志与安全状态
- 系统托盘
- ChatGPT remote-control kill switch

不要改回 AgentDock 风格，也不要新建 competing control panel。

现有 AgentDock Dynamic MCP fallback：
runtime-core-preview

必须保留。
不要删除。
不要修改官方 AgentDock。

====================
1. 最重要的 ACP 约束
====================

ACP 永远默认 Dormant / OFF。

本任务绝对不要调用：
acp_start
acp_resume

允许用：
acp_status

但 status 必须只是检测，不启动 agent。

以下任何行为都不能自动启动 Codex/Claude/Grok/其他 ACP：
- Windows 登录
- 启动 Runtime Control
- 启动 Runtime Core
- ChatGPT 扫描 MCP tools
- ChatGPT 连接 Runtime
- health check
- status check
- Chrome Bridge attach

如果发现任何自动 ACP 行为，先修复，再继续。

====================
2. 先检查当前 OpenAI 官方能力 + 当前账户真实 UI
====================

这是会变化的产品能力，不要根据旧教程猜。

先阅读当前 OpenAI 官方文档：
- Developer mode and MCP apps in ChatGPT
- Apps in ChatGPT
- Secure MCP Tunnel 相关官方文档
- 如有新的 Plugins / Apps / Custom MCP 文档，以最新官方版本为准

然后用当前已经登录的 ChatGPT 网页版实际检查。

重点检查：
- Settings → Apps → Advanced Settings
- Workspace settings → Apps
- Workspace settings → Permissions & Roles
- Developer mode
- Create custom MCP app / connector
- Plugins / Custom Apps 的当前入口
- Action controls / confirmation controls
- Secure MCP Tunnel 的当前接入流程

记录当前账户真实能力：
1. 能不能创建 custom app / MCP connector
2. 能不能连接 private/local MCP
3. 是否只允许 read/fetch
4. 是否允许 write/modify actions
5. 是否能让 Runtime 执行 click/type/write/git/process/UIA 等 mutation
6. custom app 在普通 Chat / Work / Agent mode / Deep Research 的当前限制

截至任务启动前的官方文档可能仍显示：
- 本地 MCP 不能被 ChatGPT Web 直接连接，应通过 Secure MCP Tunnel 等官方私有连接方式
- Full MCP write/modify 的套餐范围可能与 read/fetch 不同

但最终必须以“本次执行时最新官方文档 + 当前账号实际 UI”为准。

如果当前账号没有 Runtime 所需的 write/modify 权限：
- 不要伪装 mutation 为 read-only
- 不要绕过 ChatGPT action confirmation
- 不要为了绕过套餐限制暴露不安全公网接口
- 不要购买/升级套餐
- 在 Runtime Control 的安全状态文件中写 BLOCKED 状态
- 保留 AgentDock → Runtime fallback
- 明确报告哪些能力被 ChatGPT 产品权限阻塞

====================
3. 优先目标架构
====================

目标优先级：

A. 首选：
ChatGPT Web
  → Runtime custom app / MCP
  → OpenAI 官方 Secure MCP Tunnel（如果当前官方仍是推荐方案）
  → 本机 Runtime

B. fallback：
ChatGPT
  → AgentDock
  → runtime-core-preview
  → Runtime

不要删除 B，直到 A 经过真实 E2E 验收。

禁止为了省事直接把 Runtime 裸露成：
- 0.0.0.0 MCP
- 无认证 HTTP MCP
- 路由器端口映射
- 随机 ngrok 公网地址
- 任意公共临时 tunnel

AgentDock 当前已有自己的公网/Cloudflare 配置，也不要直接复用它的 token、OAuth 密钥或公网地址作为 Runtime 的凭据。

====================
4. Runtime transport
====================

保留当前 stdio。

ChatGPT Web 无法直接使用 localhost/stdio 时，按当前 OpenAI Secure MCP Tunnel 官方实现接入。

不要猜 tunnel client 的命令行参数。
先查最新官方文档。

如果官方 tunnel 可以直接桥接 stdio：
使用官方方式，不加第二套 transport。

如果当前官方 tunnel 明确要求本地 Streamable HTTP：
才给 Runtime 增加 loopback-only transport：
127.0.0.1:<port>/mcp

要求：
- bind 只能 127.0.0.1
- 禁止 0.0.0.0
- stdio 继续保留
- HTTP 与 stdio 共用同一 tool registry / schema / handler
- 不复制业务逻辑
- 正确实现当前 MCP initialization/session
- health endpoint 不暴露 secrets
- graceful shutdown
- automated tests

如果修改 Runtime 代码：
- 正确测试
- commit
- push main 到 https://github.com/happyivanencoding/runtime

====================
5. Secure MCP Tunnel / 私有连接
====================

使用当前 OpenAI 官方支持的私有连接方式。

凭据要求：
- 不写入 Git
- 不写 README
- 不写 source code
- 不写 .env.example
- 不写 Runtime Control 状态文件
- 不打印到普通日志
- 不显示在最终回复

优先使用官方 credential store / Windows Credential Manager / ACL 受限的用户配置。

不要关闭 Windows Defender。
不要关闭 UAC。
不要全局降低 PowerShell ExecutionPolicy。
不要默认以 Administrator 启动 Runtime。
不要给 Runtime SYSTEM 权限。

Windows Firewall：
- 私有 tunnel 应 outbound-only
- 如果新增 loopback MCP，不创建公网 inbound rule

====================
6. ChatGPT 网页版配置
====================

在当前账号实际支持的前提下：

1. 启用 Developer mode / Custom App 能力
2. 创建私人 Runtime App
3. 名称优先：
   Runtime Local
4. 不发布到公共 Plugin Directory
5. 连接到 Runtime 的私有 MCP transport
6. 扫描/刷新 Runtime tools

重点检查：
- browser_session
- browser_act
- browser_snapshot
- desktop_inspect
- desktop_act
- desktop_screen
- filesystem/file tools
- Coding
- Git
- LSP
- runtime status/context
- acp_start/acp_resume/acp_status/acp_stop

Mutation metadata 必须诚实。
以下属于会改变外部状态的操作时，不得伪装 read-only：
- click
- fill/type
- select
- upload
- trigger download
- file write/edit/delete
- git commit/push
- process actions
- Windows UI actions
- ACP start/resume/stop

如果 ChatGPT 提供 Action Controls / confirmation：
按正常安全机制配置。
不要绕过确认。

====================
7. 与 Runtime Control 客户端对接
====================

Runtime Control 已经实现读取以下“非敏感状态文件”：

%LOCALAPPDATA%\RuntimeCore\chatgpt-connection.json

配置完成后写入类似：

{
  "app_name": "Runtime Local",
  "transport": "Secure MCP Tunnel",
  "tunnel_name": "Runtime Local",
  "status": "connected",
  "last_verified_at": "<ISO-8601>"
}

字段名称可以扩展，但以上字段保持兼容。

这个文件绝对不能包含：
- tunnel token
- OAuth secret
- bearer token
- API key
- cookie
- password
- authorization header
- credential path that reveals secrets

如果因为账户/产品限制无法完成：
status 写：
blocked

可以额外写非敏感 reason，例如：
"reason": "ChatGPT account currently lacks full MCP write/modify actions"

但不要把 secret 或完整认证响应写入。

Runtime Control 还会寻找：

%LOCALAPPDATA%\RuntimeCore\scripts\Start-RuntimeForChatGPT.ps1
%LOCALAPPDATA%\RuntimeCore\scripts\Stop-RuntimeForChatGPT.ps1
%LOCALAPPDATA%\RuntimeCore\scripts\Status-RuntimeForChatGPT.ps1

如果直接连接可以正式启用，请创建这三个脚本。

要求：
- idempotent
- 普通用户权限运行
- 不弹持续黑窗口
- 不输出 secrets
- 只有一个 tunnel/connector owner
- 不启动第二个重复 Runtime 实例
- Start：恢复 ChatGPT → Runtime 链路
- Stop：明确 kill switch，只停 ChatGPT → Runtime 远程链路
- Stop 不能杀 Chrome
- Stop 不能删除数据
- Stop 不能停官方 AgentDock
- Stop 不应破坏 runtime-core-preview fallback
- Status：只读状态，不启动 ACP、不修改系统

如果直接连接被产品权限 BLOCKED：
不要伪造 Start/Stop 能力；可以不创建 Start/Stop，或仅创建明确返回 BLOCKED 的安全脚本。

====================
8. Chrome Bridge 本机检查
====================

检查真实 Chrome Profile：
chrome://extensions

Runtime Chrome Bridge 必须 Enabled。

Extension ID 必须保持：
agidgjchdiodbkkaggifpflepjgoedff

不要重新生成 manifest key。
不要更换 Extension ID。

Native Messaging Host：
com.runtime.browser_bridge

HKCU manifest 必须指向正式 Runtime 安装版本。

验证：
extension_status connected=true
attached_tabs=[]（空闲时）

然后用一个无敏感内容的测试页面实际验收：
- attach
- URL
- DOM
- fill
- type
- select
- click
- screenshot
- network
- upload
- download

确认：
- 发光 cursor 正常
- 当前 Runtime tab 有可见 marker
- detach 后 marker 恢复
- 不破坏用户原有 tab group

不要要求日常 Chrome 使用 --remote-debugging-port。
Chrome Bridge 继续使用 chrome.debugger + Native Messaging。

====================
9. Windows 电脑控制
====================

验证 Runtime Windows UI Automation：
- desktop_inspect
- desktop_act
- desktop_screen

默认普通用户权限。

仅当目标程序自身 elevated 导致 privilege mismatch 时报告问题。
不要为了测试把 Runtime 永久改成管理员。

Browser fallback 继续遵守：
Browser/CDP operational failure
+ 调用方明确给 semantic UIA selector
→ 才允许 Windows UIA fallback

CSS selector 不得自动转换为屏幕坐标乱点。

====================
10. 登录后自动可用
====================

在 E2E 全部通过后再设置自动恢复。

目标：
用户登录 Windows 后，Runtime Direct MCP 私有连接可自动恢复，不需要常驻可见 PowerShell 窗口。

优先使用 OpenAI Tunnel Client 官方 autostart / service 机制。
如果官方没有：
使用普通用户级 Scheduled Task 或 Startup 入口。

要求：
- 不要管理员权限
- 断线可恢复
- 有合理 restart delay
- 不疯狂重启
- secret 不写进 task command line
- 不启动重复 Runtime owner

Runtime Control 自己已有托盘 + 登录启动开关。
不要通过另外一套脚本重复实现第二个 Runtime Control autostart。

====================
11. 真实 E2E 验收
====================

不是 curl-only。
至少验收：

A. Runtime Control
- 从 Start Menu 或桌面快捷方式启动
- 客户端显示真实 Runtime version
- Chrome Bridge 状态正确
- ACP 显示 Dormant
- ChatGPT 状态与实际连接一致

B. ChatGPT Web
- 在官方允许 custom app 的聊天模式中选择 Runtime Local
- 从真正 ChatGPT 网页调用 Runtime status

C. Browser
ChatGPT → Runtime → Chrome Bridge
在安全测试页：
- 输入文字
- 点击按钮
- 读 DOM
- 截图
- 发光 cursor
- Runtime tab marker

D. Windows UIA
ChatGPT → Runtime → Windows UIA
使用 Notepad 等安全测试程序：
- 聚焦
- 输入 Runtime UIA acceptance
- 验证内容
不要保存用户文件。

E. File
只在：
C:\dev\runtime-core-artifacts\acceptance
测试 create/read/delete。

F. Git
只执行：
git status
针对：
C:\dev\runtime-core
不要为验收制造假 commit。

G. ACP dormant
调用 acp_status。
前后比较 ACP/Codex agent 进程。
确认没有自动启动新 agent。
绝对不要调用 acp_start。

H. Chrome restart/reconnect
检查 Native Messaging 可恢复。

I. Tunnel restart/reconnect
检查 direct connection 可恢复。

J. AgentDock fallback
确认 runtime-core-preview 仍在并可用。
官方 AgentDock 未被修改。

====================
12. Runtime Control 最终状态
====================

配置成功后，Runtime Control 中应看到：
- Runtime Core：正常
- MCP：可用
- Chrome Bridge：Connected
- ChatGPT：Connected / Ready
- ACP：Dormant
- Windows UIA：Ready
- AgentDock fallback：Available

Runtime Control 的“启动连接 / 暂停远程控制”按钮必须调用前述安全脚本。

不要把 token 显示在 GUI。
不要增加“显示密码/显示 token”按钮。
不要让客户端读取浏览器密码。

====================
13. Git 与交付
====================

如果本任务修改任何 Runtime 代码：
- git diff --check
- 相关自动测试
- build
- 真实 E2E
- commit main
- push origin main

不要 reset/stash/覆盖其他人的工作。

最终给出表格：

ChatGPT Developer Mode      PASS / BLOCKED
Custom Runtime App          PASS / BLOCKED
Full MCP write/modify       PASS / BLOCKED
Private/Secure MCP transport PASS / BLOCKED
Runtime Control             PASS
Runtime MCP                 PASS
Chrome Bridge               PASS
Browser control             PASS / BLOCKED
Glowing cursor              PASS
Runtime tab marker          PASS
Windows UI Automation       PASS
File control                PASS / BLOCKED
Git control                 PASS / BLOCKED
ACP dormant                 PASS
Startup persistence         PASS / NOT NEEDED / BLOCKED
AgentDock fallback          PASS

并告诉我：
1. ChatGPT 里的 App 最终名称
2. 新聊天中如何选择 Runtime
3. 重启电脑后是否自动恢复
4. 本机查看状态的方法
5. 一键暂停 ChatGPT → Runtime 的方法
6. tunnel 名称/ID（不能显示 secret）
7. 若改代码，最终 commit SHA
8. 是否 push 成功
9. 所有产品/套餐限制

只有登录、2FA、安全确认必须让我操作时，才停下来叫我。
```
