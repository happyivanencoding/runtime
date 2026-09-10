# ACP Adapter + Browser Layer — 第三阶段交付

日期：2026-09-10。工作版本：`0.3.0-runtime-core.1`。

## ACP Adapter

Runtime 正式提供四个高层入口：`acp_start`、`acp_resume`、`acp_status`、`acp_stop`。

- 高层 Adapter Registry 始终存在，但 `default_active=false`；能力发现和 `acp_status` 不启动 Agent 进程。
- `acp_start` 默认选择 Codex，可选择 Claude、Grok，或用绝对 executable + args 接入其他 ACP Agent。
- `acp_resume` 根据持久 Session 自动恢复原 Agent；`acp_stop` 取消当前 turn 并关闭 Session。
- `acp_start/acp_resume` 可直接携带 prompt，返回异步 `run_id`；`acp_status` 可读取事件、状态和待处理 interaction。
- 原 `acp_session/acp_prompt/acp_interaction` 仅保留兼容，并继续受旧 `AGENTDOCK_ACP_ENABLED` 开关控制。
- ACP 仍是 intelligence transport only，不属于 Coding 执行依赖；普通开发、Git、LSP、Browser、Desktop 不触发 ACP。

## Browser Layer

Browser 现在默认属于 Runtime Core 能力。主实现是 Go 原生 CDP/chromedp；没有再建立 Node/Playwright sidecar 或第二套 Browser 生命周期。

`browser_session` 支持：

- `transport=cdp`：启动 Chrome/Chromium/Edge 或连接已配置 CDP；
- `transport=extension`：通过 Runtime Chrome Bridge 附加用户当前/指定的真实 Chrome tab；
- `extension_status`：只读取桥接状态和标签页，不启动新浏览器。

`browser_act` 支持 goto、click、fill、真实 type、press、select、upload、download、scroll、reload/back/forward、selector/text/url/response waits，以及 tab_new/tab_switch/tab_close。结果包含 URL、live DOM、正文、viewport、page size、focused element、interactive elements、console/page errors、成功 network responses、network failures 和 screenshot Artifact。显式 download 会等待完成并发布为 Artifact。

`browser_snapshot` 同样支持 native CDP 与 Extension session，并返回 bounded live DOM + PNG Artifact。

`browser_session.start` 还提供 `show_cursor`（默认 `true`）。可见页面交互时 Runtime 会显示一个不接收 pointer event 的青色发光光标：输入/选择/上传前平滑移动到目标，click/download 额外显示点击脉冲。视觉层从序列化 DOM 中剔除，不改变选择器语义，也不会依赖页面动画帧阻塞后台 tab；显式 `show_cursor=false` 可关闭。

## Chrome Extension

源码位于 `extensions/runtime-chrome`，Manifest V3，固定 Extension ID：

```text
agidgjchdiodbkkaggifpflepjgoedff
```

扩展使用 Chrome 官方 `chrome.debugger` 作为 CDP transport，并结合 `chrome.tabs`、`chrome.tabGroups`、`chrome.downloads`、Native Messaging。它不使用远程脚本，也不要求给正常 Chrome profile 添加 `--remote-debugging-port`。

当 Runtime 附加一个真实 Chrome tab 且 `show_cursor=true` 时，该 tab 还会获得标签栏级可见标识：标题临时加 `✦ Runtime ·` 前缀、favicon 替换为青色发光 Runtime 指针；如果该 tab 原本未分组，则在原窗口内创建青色 `Runtime` 标签组。已有用户标签组不会被替换。session 结束/detach 后恢复标题/favicon，并仅撤销 Runtime 自己创建的分组。

Runtime 安装器会：

1. 将扩展复制到 `%LOCALAPPDATA%\RuntimeCore\chrome-extension`；
2. 生成 `%LOCALAPPDATA%\RuntimeCore\chrome-native-host.json`；
3. 在当前用户的 `HKCU\Software\Google\Chrome\NativeMessagingHosts\com.runtime.browser_bridge` 注册本地主机；
4. Native Messaging 直接启动已安装 `runtime-core.exe`，Runtime 验证固定 extension origin，再建立仅 loopback 的短期 bridge。

Chrome 对未上架 unpacked extension 的持久加载仍由 Chrome 自己要求用户确认。开发/本机安装时在 `chrome://extensions` 开启 Developer mode，选择 Load unpacked，并指向上述 `chrome-extension` 目录。后续若发布 Chrome Web Store，可在不改变 Runtime Browser API 的情况下替换为正式更新渠道。

官方协议/API参考：

```text
https://developer.chrome.com/docs/extensions/reference/api/debugger
https://developer.chrome.com/docs/extensions/reference/api/tabs
https://developer.chrome.com/docs/extensions/reference/api/downloads
https://developer.chrome.com/docs/extensions/develop/concepts/native-messaging
https://chromedevtools.github.io/devtools-protocol/
```

## Windows UI Automation fallback

`browser_act.desktop_fallback` 是唯一 Browser → Desktop fallback。它必须显式给出 Windows window handle、UIA semantic selector 和 action；仅当 Browser 层返回实际的 action/page/CDP/timeout operational error 时执行。

Runtime 不把 CSS selector 猜成 UIA selector，不把 DOM 元素猜成坐标，也不静默使用鼠标/键盘。优先级固定为：结构化 API > CDP/DOM > Windows Accessibility > visual pointer。

## 安装状态与验证

本阶段完成后，本机安装仍沿用 `%LOCALAPPDATA%\RuntimeCore` 和独立 Runtime home；ACP 默认关闭，Browser 默认开启。第三阶段的最终 Git SHA、安装版本和真实 Chrome 验收结果以当前仓库 `git log -1` 与本阶段交付记录为准。
