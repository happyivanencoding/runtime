# Codex prompt — connect Runtime to ChatGPT Web

> Send the block below to Codex on the Windows machine that runs Runtime.

```text
你现在直接在这台 Windows 电脑上完成 Runtime → ChatGPT 网页版的正式连接、插件/App/MCP 设置，以及电脑控制相关的本机配置和真实 E2E 验收。

这不是“写教程”的任务。能安全自动完成的步骤直接完成；只有登录、2FA、重新授权、安全确认这类必须由本人操作的步骤才暂停让我接手。

本项目的公网 MCP 架构已经由用户永久确定，不再比较或迁移到 OpenAI Secure MCP Tunnel：

ChatGPT Web
  → https://runtime.thegreatnovel.com/mcp
  → Runtime 独立 Cloudflare named Tunnel
  → http://127.0.0.1:8767/mcp
  → Runtime Core

除非用户未来明确改变这一架构，否则：
- 不使用 OpenAI Secure MCP Tunnel
- 不建议 OpenAI Secure MCP Tunnel
- 不把它作为 fallback
- 不为了“更官方”而替换 Cloudflare
- AgentDock 的 https://agent.thegreatnovel.com/mcp 与 Runtime 必须是两个独立公网入口、两个独立 Cloudflare Tunnel
- Runtime 可以迁移 AgentDock 现有 Bearer/OAuth 凭据以免用户重新查找输入，但不能把 AgentDock 的 tunnel-specific Cloudflare token 当成 Runtime 独立 tunnel 的 active token

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

本机在 2026-09-10 已完成的公网前置状态，先核验后继续：
- `%LOCALAPPDATA%\RuntimeCore\public-connection.json` 已初始化为 `runtime.thegreatnovel.com` / loopback 8767
- AgentDock 的 Bearer Token、OAuth 密码、OAuth signing secret 已通过 DPAPI 迁移到 Runtime 自己的 `secrets` 目录
- AgentDock Cloudflare tunnel token 只迁移了 reference-only 副本，没有激活
- `%LOCALAPPDATA%\RuntimeCore\bin\cloudflared.exe` 已有独立副本
- Runtime 自己的独立 Cloudflare tunnel 尚未完成时，`secrets\cloudflared-token.dpapi` 应不存在，状态应是 `tunnel_pending`

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
- ChatGPT custom MCP app / connector 对 HTTPS MCP endpoint 的当前要求
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
- 是否允许直接填写 https://runtime.thegreatnovel.com/mcp 作为自定义 MCP endpoint

记录当前账户真实能力：
1. 能不能创建 custom app / MCP connector
2. 能不能连接 private/local MCP
3. 是否只允许 read/fetch
4. 是否允许 write/modify actions
5. 是否能让 Runtime 执行 click/type/write/git/process/UIA 等 mutation
6. custom app 在普通 Chat / Work / Agent mode / Deep Research 的当前限制

截至任务启动前的官方文档可能仍显示：
- ChatGPT Web 需要可达的 HTTPS MCP endpoint；本项目固定使用 runtime.thegreatnovel.com 经 Cloudflare Tunnel 提供该 endpoint
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
3. 固定目标架构
====================

唯一目标：

ChatGPT Web
  → Runtime custom app / MCP
  → https://runtime.thegreatnovel.com/mcp
  → Runtime 独立 Cloudflare named Tunnel
  → http://127.0.0.1:8767/mcp
  → Runtime Core

fallback 继续保留：
ChatGPT
  → AgentDock
  → runtime-core-preview
  → Runtime

Runtime 与 AgentDock 必须使用不同 hostname 和不同 Cloudflare Tunnel。不要把 agent.thegreatnovel.com 改指 Runtime，也不要让 Runtime 复用 AgentDock tunnel 作为生产入口。

禁止：
- OpenAI Secure MCP Tunnel
- 0.0.0.0 裸 MCP
- 无认证公网 HTTP MCP
- 路由器端口映射
- ngrok/trycloudflare 作为正式入口
- 把 AgentDock 的 tunnel token 直接当作 Runtime 独立 tunnel token

====================
4. Runtime transport
====================

保留 stdio，供本机和 AgentDock fallback 使用。

为固定公网入口启用 Runtime 已支持的 loopback HTTP transport：
http://127.0.0.1:8767/mcp

要求：
- bind 只能 127.0.0.1
- 公网只能通过 Cloudflare Tunnel 进入
- stdio 与 HTTP 共用同一 tool registry / schema / handlers
- 不复制业务逻辑
- health endpoint 不暴露 secrets
- ACP 默认仍为 false/dormant

初始化非敏感公网配置：
%LOCALAPPDATA%\RuntimeCore\scripts\Initialize-RuntimePublicConnection.ps1

该脚本应生成：
%LOCALAPPDATA%\RuntimeCore\public-connection.json

其中固定：
- local_mcp_url = http://127.0.0.1:8767/mcp
- public_mcp_url = https://runtime.thegreatnovel.com/mcp
- architecture = public_https_cloudflare_named_tunnel

====================
5. Cloudflare Tunnel + 凭据迁移
====================

先运行：
%LOCALAPPDATA%\RuntimeCore\scripts\Import-AgentDockCredentials.ps1

它会从当前用户的：
%LOCALAPPDATA%\AgentDock
读取已有 DPAPI 凭据，在内存中解密后以 Runtime 自己的 DPAPI entropy 重新加密到：
%LOCALAPPDATA%\RuntimeCore\secrets

允许自动迁移：
- Bearer Token
- OAuth 密码
- OAuth signing/token secret
- AgentDock Cloudflare tunnel token 的 reference-only 副本

重要：AgentDock 的 Cloudflare tunnel token 是 tunnel-specific。它可以安全导入作为参考，但绝对不能写成 Runtime active cloudflared-token.dpapi，也不能用于启动 Runtime 正式 tunnel，否则 Runtime 就不是独立 tunnel。

为 Runtime 新建一个独立 Cloudflare named Tunnel，名称优先：runtime。
优先使用用户当前已登录的 Cloudflare Dashboard/浏览器会话创建，不要求用户手工查找或重新输入 AgentDock 的密码/token。

Runtime 使用自己安装目录中的 cloudflared：
%LOCALAPPDATA%\RuntimeCore\bin\cloudflared.exe
如果不存在，可以从当前 AgentDock 安装复制同一 cloudflared.exe 后先验证 `--version`；不要让 Runtime 的长期运行直接依赖 AgentDock 安装目录中的二进制。

创建后将 Runtime tunnel 的新 token 通过临时文件交给：
%LOCALAPPDATA%\RuntimeCore\scripts\Set-RuntimeCloudflareToken.ps1

并用：
%LOCALAPPDATA%\RuntimeCore\scripts\Set-RuntimePublicTunnelState.ps1 -TunnelId <Runtime tunnel id> -TunnelName runtime -Status configured
写入非敏感 tunnel ID/状态，供 Runtime Control 展示。

该脚本只把新 token 以 CurrentUser DPAPI 保存为：
%LOCALAPPDATA%\RuntimeCore\secrets\cloudflared-token.dpapi

不得把 token 输出到终端、聊天、日志或 Git。临时明文文件保存成功后立即删除。

Cloudflare 配置必须把：
runtime.thegreatnovel.com
路由到：
http://127.0.0.1:8767

认证继续由 Runtime 自己的 Bearer/OAuth 层负责；Cloudflare Tunnel 不是应用认证的替代品。

安全要求：
- secrets 不写 Git/README/.env.example/普通日志
- 不显示在 Runtime Control
- 不关闭 Defender/UAC
- 不默认管理员权限
- Cloudflare tunnel 仅 outbound connection，不增加公网 inbound firewall rule

====================
6. ChatGPT 网页版配置
====================

在当前账号实际支持的前提下：

1. 启用 Developer mode / Custom App 能力
2. 创建私人 Runtime App
3. 名称优先：
   Runtime
4. 不发布到公共 Plugin Directory
5. MCP endpoint 固定填写：
   https://runtime.thegreatnovel.com/mcp
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
  "app_name": "Runtime",
  "transport": "Cloudflare HTTPS MCP",
  "endpoint": "https://runtime.thegreatnovel.com/mcp",
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

这三个脚本已经由 Runtime 实现并安装；先验证现有实现，不要重新写第二套 lifecycle。Runtime 独立 tunnel token 未配置时，Start 必须保持不可用/明确失败，不能退回去使用 AgentDock tunnel token。

要求：
- idempotent
- 普通用户权限运行
- 不弹持续黑窗口
- 不输出 secrets
- Runtime 的 HTTP MCP 与独立 cloudflared 各自只能有一个 owner
- 不启动第二个重复 Runtime 实例
- Start：恢复 ChatGPT → Runtime 链路
- Stop：明确 kill switch，只停 ChatGPT → Runtime 远程链路
- Stop 不能杀 Chrome
- Stop 不能删除数据
- Stop 不能停官方 AgentDock
- Stop 不应破坏 runtime-core-preview fallback
- Status：只读状态，不启动 ACP、不修改系统

如果 ChatGPT 产品权限 BLOCKED：
保留现有 Cloudflare/Runtime lifecycle 代码，但不要伪造 ChatGPT App 已连接；`chatgpt-connection.json` 标记 blocked，AgentDock fallback 继续可用。

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
用户登录 Windows 后，Runtime HTTP MCP + Runtime 独立 cloudflared named tunnel 可自动恢复，不需要常驻可见 PowerShell 窗口。

优先使用普通用户级 Scheduled Task 或 Startup 入口分别管理 Runtime HTTP MCP 与 cloudflared；不要依赖 OpenAI Tunnel Client。两个进程都必须有单实例/幂等保护。

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
- 在官方允许 custom app 的聊天模式中选择 Runtime
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

I. Cloudflare Tunnel restart/reconnect
重启 Runtime 独立 cloudflared，确认 https://runtime.thegreatnovel.com/mcp 恢复，并确认没有影响 agent.thegreatnovel.com。

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
Cloudflare Public MCP       PASS / BLOCKED
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
6. Runtime 独立 Cloudflare tunnel 名称/ID（不能显示 secret）
7. 若改代码，最终 commit SHA
8. 是否 push 成功
9. 所有产品/套餐限制

只有登录、2FA、安全确认必须让我操作时，才停下来叫我。
```
