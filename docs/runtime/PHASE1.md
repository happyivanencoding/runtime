# runtime-core 第一阶段交付与使用

本目录记录 2026-09-10 实际实现的第一阶段，不代表全部长期路线已完成。
产品名仍是内部 working name；现有 Go module/import 路径不是最终品牌决定。

## 源码与运行边界

- 独立源码：`C:\dev\runtime-core`。
- 开发分支：`runtime/coding-foundation`。
- 上游 remote：`upstream-agentdock`，从官方 v0.8.1 的真实运行提交开始。
- 最新交付提交以 `git log -1` 为准；LICENSE、NOTICE 和修改来源标记保留。
- 第一阶段工作版本：`0.1.0-runtime-core.1`。
- 构建与验证产物：`C:\dev\runtime-core-artifacts\phase1`。
- 独立预览状态：`C:\dev\runtime-core-state`。
- 当前已安装的 AgentDock 0.8.1 未替换、未重启；其他业务项目未修改。
- 当前仓库为本地独立 fork，尚未创建自有 GitHub origin 或推送远端。

现有 AgentDock 的 Dynamic MCP 注册名为 `runtime-core-preview`，通过 stdio
运行本次构建的 Runtime。它是当前对话调用原生 Go 能力的预览接入方式，不是
ACP/Codex，也不是独立执行 Agent。直接连接新二进制时不需要这层旧 Runtime
转发；长期不存在必须保留两个 Runtime 或一个 WebCodex Server 的架构要求。

预览环境显式设置：

```text
AGENTDOCK_HOME=C:\dev\runtime-core-state
AGENTDOCK_DEFAULT_DIR=C:\dev\runtime-core
AGENTDOCK_ACP_ENABLED=false
```

不要让旧版和 fork 同时写同一个 state home。旧版本不理解新增 Coding 字段。
正式切换安装、桌面托盘和已有状态迁移不属于这次已完成内容。

## 已实现的原生工具

| 工具 | 实际作用 |
|---|---|
| project_registry | 登记、读取、列出本机项目；保存机器、路径、远端、规则、技术栈和声明式设备/部署信息 |
| work_on_project | 开始或恢复一个既有 Task 身份下的 Coding 执行，返回完整项目与 workspace 上下文 |
| coding_task | 读取同一个 Task 的代码工作区、命令、验证、Artifacts 与收尾结果 |
| git_status / git_diff / git_changed_files | 结构化状态、差异、已修改及任务期间已提交的文件 |
| git_commits / git_branches | 结构化提交记录和本地分支 |
| git_worktree | 列出或明确移除当前任务的 managed worktree；创建在项目入口完成 |
| git_commit | 只提交明确指定的文件，保留无关暂存内容，不自动推送 |
| validation_run | 使用现有 command/session 执行有明确目的的真实检查，保存退出码、耗时和日志 Artifact |
| finish_coding_task | 保存真实 Git、改动文件、验证证据、剩余问题和 ready_for_review，不替代人工接受 |

底层仍是 `app.Runtime`、`taskstate.Task`、原有命令 Session 和 publicartifacts。
没有新建平行 Task/Job/Workflow Session/Plugin 系统，没有调用模型来执行 Git。

## 当前对话怎么使用

先通过现有 Dynamic MCP 的发现工具读取完整 schema：

```text
mcp_tool_search(server="runtime-core-preview", query="project")
mcp_tool_inspect(name="runtime-core-preview:work_on_project")
```

进入已经登记的项目：

```json
{
  "project": "jobpilot",
  "task": "修改 Android 任务卡片，并同步 Web",
  "execution_mode": "interactive-owner",
  "assignee": "Jingxuan"
}
```

从返回值取得 `task_id` 后，文件与命令工具可以只传相对路径：

```json
{
  "task_id": "<返回的任务 id>",
  "path": "AGENTS.md"
}
```

这不是全局的“当前项目”。HTTP MCP 无状态，多个对话使用各自的任务句柄，
不会因为另一个对话进入 TGN 就把 JobPilot 的相对路径重定向到 TGN。
未传 `task_id` 的 General 工具保持原有行为。

检查前必须填写真实理由，例如：

```json
{
  "task_id": "<返回的任务 id>",
  "cmd": "git diff --check",
  "purpose": "发现即将提交补丁中的冲突标记或空白错误。",
  "on_failure": "根据具体位置修正补丁，不把这项结果当成功。"
}
```

返回 running 时用原有 `session_observe` 观察同一个 `session_id`，不重新执行。
再次进入已有任务时传 `work_on_project({"task_id":"..."})` 即可，不重建任务。

已登记的真实本机项目为：

| id | 仓库 |
|---|---|
| runtime-core | C:\dev\runtime-core |
| jobpilot | C:\dev\career-ops |
| project-os | C:\dev\project_os |
| tgn | C:\dev\tgn_live |

登记数据保存在独立 Runtime home，不写入这些业务项目。设备和部署字段是
操作者声明的元数据；`connected_devices` 当前明确标示未探测，不伪装实时连接状态。

## 两种执行模式与收尾

`interactive-owner` 使用当前 checkout，保留进入前的脏文件与暂存内容。
`delegated-task` 从明确的基准提交建立任务专属 managed worktree。
两者使用同一 Project、同一 Task 类型和同一工具集；委派模式不意味着启动模型。

managed worktree 的位置、分支和基准提交先保存，再执行 Git 创建。恢复使用
原任务和原路径。移除没有 force，脏 worktree 拒绝清理，分支保留；已完成任务
也能明确清理资源，完成结论与审核证据不被修改。

`finish_coding_task` 不自动 commit、push、merge、deploy、install、清理 worktree
或把 Task 标为已完成。事件 `coding.started`、`coding.ready_for_review`、
`coding.finished` 保存于既有 Task.Events，作为未来 Project OS 接入边界；
本轮没有实现 Project OS 自动状态同步、webhook 投递或人工审批端点。

没有运行检查时返回 `not_run`，不是 passed；需要说明原因才可声明可送审。
失败或中断的验证不能声明 ready。同一命令/工作目录的成功重跑可以替代旧失败，
历史失败仍保留。验证证据描述实际执行，不自动证明外部编辑后的源码仍是同一状态。

## 真实验证与修复

`coding-tests-02.log` 中的 13 个顶层测试通过，覆盖原生 Git、工作区隔离、任务
恢复、真实命令证据、工具 schema、禁止官方二进制更新及版本入口。
`cleanup-tests.log` 中的 2 个测试通过，其中 1 个为新增的已完成任务资源清理
回归，另 1 个覆盖 owner/delegated 现有路径。
`taskstate-regression-mcp.log` 通过真实 MCP 调用运行，16 个既有 Task 顶层回归通过。
没有运行独立 ACP/模型测试，也没有因此启动 Codex。

测试中发现并修复两处真实问题：

1. 旧 session_observe 会消费并移除已完成命令，导致新的 Coding 层后来把成功
   命令误判为中断。修复是在同一原生 Session 完成时保存证据，保留旧工具的
   消费语义，而不是另建 Job 记录系统。
2. 已完成 Task 原本不可变，使清理 managed worktree 后无法更新资源状态。
   现在只允许清理元数据更新，原有完成和审核结论保持不变，并支持清理后的重试。

断线不会自动重跑命令。正常关闭会保存命令最终状态；异常进程退出后找不到的
未完成 Session 标为 interrupted。异常恢复测试使用缺失 Session 的故障注入；
本轮不声称完成了跨进程子进程收养或跨机器 Job 恢复。

## 明确未实现的第二阶段

LSP 原生 server supervisor/navigation、远端 Runner 的项目清单与 Job 对账、
Desktop Accessibility、Project OS 状态同步、WSL 项目入口、实时设备探测、
新的桌面安装/更新发行流程，均未冒充完成。边界和源码取舍见 ARCHITECTURE.md
及 UPSTREAM.md。WebCodex 已研究但没有作为服务启动或引入强依赖。

现有 Artifact 的下载保留期仍受原实现约束，最长七天；Task 中保留命令摘要与
Artifact id，不承诺 Artifact 下载永久可用。不要把 state home 或其中秘密文件
当作源码包分发。
