const stateEl = document.getElementById("state");
const extensionIdEl = document.getElementById("extensionId");
const tabsEl = document.getElementById("tabs");
const attachedEl = document.getElementById("attached");
const detailEl = document.getElementById("detail");

async function render() {
  extensionIdEl.textContent = chrome.runtime.id;
  try {
    const response = await chrome.runtime.sendMessage({ type: "popup_status" });
    const result = response?.result || {};
    const connected = Boolean(response?.ok && response?.native_connected);
    stateEl.textContent = connected ? "已连接" : "未连接";
    stateEl.className = `badge ${connected ? "ok" : "off"}`;
    tabsEl.textContent = String(Array.isArray(result.tabs) ? result.tabs.length : 0);
    attachedEl.textContent = String(Array.isArray(result.attached_tabs) ? result.attached_tabs.length : 0);
    detailEl.textContent = connected
      ? "Runtime 本地 Native Messaging 主机已连接。只有 Runtime 明确执行浏览器操作时，扩展才会附加对应标签页。"
      : "扩展已加载，但尚未连接 Runtime Native Messaging 主机。请确认 Runtime 安装器已注册本地主机。";
  } catch (error) {
    stateEl.textContent = "不可用";
    stateEl.className = "badge off";
    tabsEl.textContent = "—";
    attachedEl.textContent = "—";
    detailEl.textContent = error?.message || String(error);
  }
}

void render();
