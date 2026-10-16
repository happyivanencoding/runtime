# Runtime 大文件上传（独立授权）

## 已有 Runtime MCP 与上传接口的区别

- MCP：`https://runtime.thegreatnovel.com/mcp`，仍使用原有 OAuth/Bearer 授权，包含文件修改、命令执行和桌面操作工具。
- ZIP 上传：`https://runtime.thegreatnovel.com/transfers/v1/uploads`，使用**另一枚只允许上传 ZIP 的令牌**。上传令牌不能用于 `/mcp`，MCP 登录令牌也不能用于上传。
- 访问路径走同一个 Runtime Cloudflare Tunnel，不需要开放路由器端口或另外购买 VPS。
- ZIP 落在本地 `C:\dev\incoming`（由 `public-connection.json` 的可选 `upload_dir` 属性覆盖）；**不会自动解压或覆盖现有项目**。
- 流式写入，每次最多 4 MiB；支持在重启、网络中断后按已提交偏移续传；总大小最高 50 GiB。超大文件的 HTTP 上传仍受本地磁盘空间等实际约束。

## 本地主人首次配置与撤销

在本地 PowerShell 执行：

```powershell
& "$env:LOCALAPPDATA\RuntimeCore\scripts\Set-RuntimeUploadToken.ps1" -CopyToClipboard
```

如之前未生成令牌，命令会生成独立的 256-bit 随机令牌并使用当前用户的 Windows DPAPI 加密保存在 `%LOCALAPPDATA%\RuntimeCore\secrets\upload-token.dpapi`。明文仅复制到剪贴板，不会回显到命令输出或 Git。把剪贴板内容粘贴到**可信的云端电脑环境变量** `RUNTIME_UPLOAD_TOKEN` 中，不要放入项目 Git 仓库或公开日志。复制到云端是**人工授权步骤**，本地 Runtime 不会自动授予任何远端 LLM 凭证。

首次创建或者轮换令牌以后，重启 Runtime 公网 HTTP 进程来载入新凭证（使用已有 Stop/Start 生命周期脚本；这会短暂中断当前 ChatGPT MCP 连接）。

```powershell
# 撤销旧令牌、生成新令牌并复制
& "$env:LOCALAPPDATA\RuntimeCore\scripts\Set-RuntimeUploadToken.ps1" -Rotate -CopyToClipboard
```

凭证被撤销或本地 Runtime 停止时，云端上传不可用。只上传不需要授权云端 LLM 调用 `exec_command`、`file_add` 等 MCP 工具。

## 云端 LLM/电脑操作

从本项目源码获取 `scripts/runtime/upload_to_runtime.py`，用云端电脑运行 Python 3（仅使用标准库）。云端操作系统需先把上传令牌配置到进程的 `RUNTIME_UPLOAD_TOKEN` 环境变量。

```bash
python upload_to_runtime.py /path/to/project.zip --url https://runtime.thegreatnovel.com
```

也接受 `--url https://runtime.thegreatnovel.com/mcp`。上传结束后客户端输出本地的最终 ZIP 路径。若上传失败、断网或进程重启，用**同一条命令**重试即可续传；云端 ZIP 所在目录会保留 `<zip>.runtime-upload.json`，成功后自动删除。该状态文件不包含凭证。

## HTTP API（其他客户端可直接实现）

所有请求使用请求头 `Authorization: Bearer <上传专用令牌>`：

1. `POST /transfers/v1/uploads`，JSON `{"filename":"build.zip","size":10485760}` → 201 `{"upload_id":"...","offset":0,"chunk_size":4194304,...}`。
2. `GET /transfers/v1/uploads/{upload_id}` → 当前持久化的 `offset`；可在连接中断后获取。
3. `PUT /transfers/v1/uploads/{upload_id}`，头 `Upload-Offset: <当前offset>`，body 为**原始二进制**分块，最大 4 MiB → 200 返回新的 `offset`。若 offset 已变化，409 返回实际 offset 供客户端重新同步。
4. 文件全部发送后 `POST /transfers/v1/uploads/{upload_id}/complete` → 最终路径，完成请求可重复调用。
5. `DELETE /transfers/v1/uploads/{upload_id}` 仅删除尚未完成的上传和暂存文件，不能删除交付成功的本地 ZIP。

这里不使用 MIME multipart，不将文件转成 base64，也不通过 MCP JSON-RPC 搬运文件。Cloudflare 请求大小上限也不会被 4 MiB 分块触及。

## 故障定位

- `401`：上传 token 未提供、错误或过期（轮换）；不能用 ChatGPT MCP OAuth 登录代替。
- Cloudflare `403 / 1010`：默认 `Python-urllib/...` 客户端签名可能被拦截；仓库提供的上传脚本使用明确的 `RuntimeTransfer/1.0` 客户端标识，已在公网实测通过。
- `404`：Runtime 没有载入上传令牌、新版本未运行或路径错误。
- `409`：偏移量失配/上传未完成，云端客户端会获取 offset 重试。
- `413`：单块超出 4 MiB。
- `5xx` / 超时：先查状态再续传；不必重新发全部文件。
- 仅有公网 MCP 地址**不等于已授权**；用户需给云端电脑配置上传令牌。

为减少权限暴露，上传客户端和服务端都不会自动解压 ZIP、执行其中代码或写入任意用户指定路径。
