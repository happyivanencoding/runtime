const NATIVE_HOST = "com.runtime.browser_bridge";
const BRIDGE_VERSION = "0.3.0";
const DEBUG_PROTOCOL_VERSION = "1.3";
const RECONNECT_ALARM = "runtime-native-reconnect";
const MAX_EVENTS_PER_TAB = 500;
const TAB_MARK_PREFIX = "✦ Runtime · ";
const RUNTIME_TAB_GROUP_TITLE = "Runtime";
const RUNTIME_GLOW_FAVICON = `data:image/svg+xml,${encodeURIComponent(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><defs><filter id="g"><feGaussianBlur stdDeviation="2.2" result="b"/><feMerge><feMergeNode in="b"/><feMergeNode in="SourceGraphic"/></feMerge></filter></defs><circle cx="16" cy="16" r="10" fill="#07111f" stroke="#38bdf8" stroke-width="3" filter="url(#g)"/><path d="M11 8l12 9-7 1-3 7z" fill="#e0f2fe" stroke="#38bdf8" stroke-width="1" filter="url(#g)"/></svg>`)}`;

let nativePort = null;
const attachedTabs = new Set();
const eventsByTab = new Map();
const requestByTab = new Map();

function tabSummary(tab) {
  return {
    page_id: String(tab.id),
    tab_id: tab.id,
    window_id: tab.windowId,
    url: tab.url || tab.pendingUrl || "",
    title: (tab.title || "").startsWith(TAB_MARK_PREFIX) ? (tab.title || "").slice(TAB_MARK_PREFIX.length) : (tab.title || ""),
    active: Boolean(tab.active),
    pinned: Boolean(tab.pinned),
    incognito: Boolean(tab.incognito),
    group_id: tab.groupId,
    runtime_attached: attachedTabs.has(tab.id),
  };
}

function errorText(error) {
  if (!error) return "unknown Runtime Chrome bridge error";
  if (typeof error === "string") return error;
  return error.message || String(error);
}

function eventBuffer(tabId) {
  if (!eventsByTab.has(tabId)) eventsByTab.set(tabId, []);
  return eventsByTab.get(tabId);
}

function pushEvent(tabId, event) {
  const buffer = eventBuffer(tabId);
  buffer.push({ ...event, timestamp: Date.now() });
  if (buffer.length > MAX_EVENTS_PER_TAB) buffer.splice(0, buffer.length - MAX_EVENTS_PER_TAB);
}

function requestMap(tabId) {
  if (!requestByTab.has(tabId)) requestByTab.set(tabId, new Map());
  return requestByTab.get(tabId);
}

chrome.debugger.onEvent.addListener((source, method, params = {}) => {
  const tabId = source.tabId;
  if (!Number.isInteger(tabId)) return;
  if (method === "Network.requestWillBeSent") {
    requestMap(tabId).set(params.requestId, {
      url: params.request?.url || "",
      method: params.request?.method || "",
    });
    return;
  }
  if (method === "Network.responseReceived") {
    const request = requestMap(tabId).get(params.requestId) || {};
    pushEvent(tabId, {
      kind: "network_response",
      url: params.response?.url || request.url || "",
      method: request.method || "",
      status: Math.trunc(params.response?.status || 0),
      mime_type: params.response?.mimeType || "",
      resource_type: params.type || "",
    });
    return;
  }
  if (method === "Network.loadingFinished") {
    requestMap(tabId).delete(params.requestId);
    return;
  }
  if (method === "Network.loadingFailed") {
    const request = requestMap(tabId).get(params.requestId) || {};
    requestMap(tabId).delete(params.requestId);
    pushEvent(tabId, {
      kind: "network_error",
      url: request.url || "",
      method: request.method || "",
      error_text: params.errorText || "network request failed",
    });
    return;
  }
  if (method === "Runtime.exceptionThrown") {
    pushEvent(tabId, {
      kind: "page_error",
      message: params.exceptionDetails?.exception?.description || params.exceptionDetails?.text || "unhandled page exception",
    });
    return;
  }
  if (method === "Runtime.consoleAPICalled") {
    if (params.type !== "error" && params.type !== "assert") return;
    const message = (params.args || []).map(arg => arg.value ?? arg.description ?? "").filter(Boolean).join(" ");
    pushEvent(tabId, { kind: "console_error", message: message || "console error" });
    return;
  }
  if (method === "Log.entryAdded" && params.entry?.level === "error") {
    pushEvent(tabId, { kind: "console_error", message: params.entry?.text || "console error" });
  }
});

chrome.debugger.onDetach.addListener(source => {
  if (Number.isInteger(source.tabId)) {
    const tabId = source.tabId;
    attachedTabs.delete(tabId);
    eventsByTab.delete(tabId);
    requestByTab.delete(tabId);
    void readTabMarker(tabId).then(async marker => {
      if (marker?.created_group_id !== null && marker?.created_group_id !== undefined) {
        try {
          const tab = await chrome.tabs.get(tabId);
          if (tab.groupId === marker.created_group_id) await chrome.tabs.ungroup(tabId);
        } catch (_) {}
      }
      await clearTabMarker(tabId).catch(() => {});
    }).catch(() => {});
  }
});

async function sendCDP(tabId, method, params = {}) {
  await ensureAttached(tabId);
  return chrome.debugger.sendCommand({ tabId }, method, params);
}

function tabMarkerKey(tabId) {
  return `runtime-tab-marker-${tabId}`;
}

async function readTabMarker(tabId) {
  const key = tabMarkerKey(tabId);
  const result = await chrome.storage.session.get(key);
  return result[key] || null;
}

async function writeTabMarker(tabId, value) {
  await chrome.storage.session.set({ [tabMarkerKey(tabId)]: value });
}

async function clearTabMarker(tabId) {
  await chrome.storage.session.remove(tabMarkerKey(tabId));
}

async function markRuntimeTab(tabId) {
  if (!attachedTabs.has(tabId)) return;
  let marker = await readTabMarker(tabId);
  const tab = await chrome.tabs.get(tabId);
  if (!marker) {
    marker = { created_group_id: null };
    if (tab.groupId === -1) {
      try {
        const groupId = await chrome.tabs.group({ tabIds: [tabId], createProperties: { windowId: tab.windowId } });
        await chrome.tabGroups.update(groupId, { title: RUNTIME_TAB_GROUP_TITLE, color: "cyan", collapsed: false });
        marker.created_group_id = groupId;
      } catch (_) {
        marker.created_group_id = null;
      }
    }
    await writeTabMarker(tabId, marker);
  }
  const prefix = JSON.stringify(TAB_MARK_PREFIX);
  const favicon = JSON.stringify(RUNTIME_GLOW_FAVICON.trim());
  try {
    await chrome.debugger.sendCommand({ tabId }, "Runtime.evaluate", {
      expression: `(() => {
        const prefix=${prefix};
        if (!document.title.startsWith(prefix)) document.title = prefix + document.title;
        let icon=document.getElementById('__runtime_tab_glow_icon');
        if(!icon){ icon=document.createElement('link'); icon.id='__runtime_tab_glow_icon'; icon.rel='icon'; document.head?.appendChild(icon); }
        if(icon) icon.href=${favicon};
        return true;
      })()`,
      returnByValue: true,
    });
  } catch (_) {
    // Some privileged Chrome pages reject Runtime.evaluate; the tab group still marks the active Runtime target.
  }
}

async function unmarkRuntimeTab(tabId) {
  const marker = await readTabMarker(tabId).catch(() => null);
  if (attachedTabs.has(tabId)) {
    const prefix = JSON.stringify(TAB_MARK_PREFIX);
    try {
      await chrome.debugger.sendCommand({ tabId }, "Runtime.evaluate", {
        expression: `(() => {
          const prefix=${prefix};
          if(document.title.startsWith(prefix)) document.title=document.title.slice(prefix.length);
          document.getElementById('__runtime_tab_glow_icon')?.remove();
          return true;
        })()`,
        returnByValue: true,
      });
    } catch (_) {}
  }
  if (marker?.created_group_id !== null && marker?.created_group_id !== undefined) {
    try {
      const tab = await chrome.tabs.get(tabId);
      if (tab.groupId === marker.created_group_id) await chrome.tabs.ungroup(tabId);
    } catch (_) {}
  }
  await clearTabMarker(tabId).catch(() => {});
}

async function ensureAttached(tabId) {
  if (!Number.isInteger(tabId)) throw new Error("tab_id must be an integer");
  if (attachedTabs.has(tabId)) return;
  await chrome.debugger.attach({ tabId }, DEBUG_PROTOCOL_VERSION);
  attachedTabs.add(tabId);
  for (const method of ["Page.enable", "Runtime.enable", "Network.enable", "Log.enable"]) {
    try {
      await chrome.debugger.sendCommand({ tabId }, method, {});
    } catch (_) {
      // Some targets omit optional domains; core Page/Runtime commands will still fail explicitly if unavailable.
    }
  }
}

async function detachTab(tabId) {
  if (!attachedTabs.has(tabId)) return;
  try {
    await unmarkRuntimeTab(tabId);
    await chrome.debugger.detach({ tabId });
  } finally {
    attachedTabs.delete(tabId);
    eventsByTab.delete(tabId);
    requestByTab.delete(tabId);
  }
}

async function activeTab() {
  const tabs = await chrome.tabs.query({ active: true, currentWindow: true });
  if (!tabs.length || !Number.isInteger(tabs[0].id)) throw new Error("Chrome has no active controllable tab");
  return tabs[0];
}

async function resolveTabId(raw) {
  if (raw !== undefined && raw !== null && raw !== "") {
    const value = Number(raw);
    if (!Number.isInteger(value) || value <= 0) throw new Error("tab_id/page_id must identify a Chrome tab");
    await chrome.tabs.get(value);
    return value;
  }
  return (await activeTab()).id;
}

function clearEvents(tabId) {
  eventsByTab.set(tabId, []);
  requestByTab.set(tabId, new Map());
}

function takeEvents(tabId) {
  const events = [...eventBuffer(tabId)];
  eventsByTab.set(tabId, []);
  return events;
}

function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

async function pollUntil(check, timeoutMs = 10000) {
  const deadline = Date.now() + Math.max(1, timeoutMs);
  let lastError = null;
  while (Date.now() <= deadline) {
    try {
      if (await check()) return;
    } catch (error) {
      lastError = error;
    }
    await sleep(50);
  }
  if (lastError) throw lastError;
  throw new Error("browser wait timed out");
}

async function waitForTabComplete(tabId, timeoutMs = 30000) {
  const current = await chrome.tabs.get(tabId);
  if (current.status === "complete") return;
  await new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      chrome.tabs.onUpdated.removeListener(listener);
      reject(new Error("navigation timed out"));
    }, Math.max(1, timeoutMs));
    const listener = (updatedId, changeInfo) => {
      if (updatedId !== tabId || changeInfo.status !== "complete") return;
      clearTimeout(timer);
      chrome.tabs.onUpdated.removeListener(listener);
      resolve();
    };
    chrome.tabs.onUpdated.addListener(listener);
  });
}

function domSnapshotExpression(maxInteractive) {
  return `(() => {
    const runtimeTabPrefix = ${JSON.stringify(TAB_MARK_PREFIX)};
    const norm = value => String(value || '').replace(/\\s+/g, ' ').trim();
    const visible = el => {
      const r = el.getBoundingClientRect();
      if (r.width <= 0 || r.height <= 0) return false;
      for (let cur = el; cur; cur = cur.parentElement) {
        const s = getComputedStyle(cur);
        if (s.display === 'none' || s.visibility === 'hidden' || s.visibility === 'collapse' || Number(s.opacity) === 0) return false;
      }
      return true;
    };
    const selectorFor = el => {
      if (el.id) return '#' + CSS.escape(el.id);
      const parts = []; let cur = el;
      while (cur && cur.nodeType === 1 && parts.length < 5) {
        let part = cur.tagName.toLowerCase();
        const parent = cur.parentElement;
        if (parent) {
          const siblings = Array.from(parent.children).filter(x => x.tagName === cur.tagName);
          if (siblings.length > 1) part += ':nth-of-type(' + (siblings.indexOf(cur) + 1) + ')';
        }
        parts.unshift(part); cur = parent;
      }
      return parts.join(' > ');
    };
    const describe = el => el ? {
      tag: el.tagName.toLowerCase(), id: el.id || '', name: el.getAttribute('name') || '', type: el.getAttribute('type') || '',
      text: norm(el.innerText || el.textContent).slice(0, 120), value: 'value' in el ? String(el.value || '').slice(0, 120) : '',
      aria_name: el.getAttribute('aria-label') || '', selector: selectorFor(el),
      is_editable: Boolean(el.isContentEditable || /^(input|textarea|select)$/i.test(el.tagName))
    } : null;
    const candidates = Array.from(document.querySelectorAll('a[href],button,input,textarea,select,[role="button"],[role="link"],[contenteditable="true"],[tabindex]'));
    const interactive = candidates.filter(visible).slice(0, ${Math.max(1, maxInteractive)}).map(el => {
      const type = (el.getAttribute('type') || '').toLowerCase();
      const safeValueText = ['button', 'submit', 'reset'].includes(type) ? String(el.value || '') : '';
      const editable = Boolean(el.isContentEditable || /^(input|textarea|select)$/i.test(el.tagName)) && !el.disabled && !el.readOnly;
      return {
        tag: el.tagName.toLowerCase(), type, name: el.getAttribute('name') || '',
        text: norm(el.innerText || safeValueText || el.textContent).slice(0, 120),
        aria_name: el.getAttribute('aria-label') || '', placeholder: el.getAttribute('placeholder') || '', role: el.getAttribute('role') || '',
        href: el.href || '', selector: selectorFor(el), disabled: Boolean(el.disabled), is_editable: editable,
        options: el.tagName === 'SELECT' ? Array.from(el.options).slice(0, 40).map(option => ({ value: String(option.value || ''), text: norm(option.textContent).slice(0, 120), disabled: Boolean(option.disabled) })) : []
      };
    });
    const doc = document.documentElement; const body = document.body;
    const cleanDOM = () => {
      if (!document.documentElement) return '';
      const clone = document.documentElement.cloneNode(true);
      clone.querySelector('#__runtime_visual_cursor')?.remove();
      clone.querySelector('#__runtime_visual_cursor_style')?.remove();
      clone.querySelector('#__runtime_tab_glow_icon')?.remove();
      const title = clone.querySelector('title');
      if (title?.textContent?.startsWith(runtimeTabPrefix)) title.textContent = title.textContent.slice(runtimeTabPrefix.length);
      return clone.outerHTML;
    };
    return {
      url: location.href, title: document.title, text: norm(body ? body.innerText : ''),
      dom: cleanDOM(),
      viewport: { width: window.innerWidth, height: window.innerHeight },
      page_size: {
        width: Math.max(doc?.scrollWidth || 0, body?.scrollWidth || 0, window.innerWidth),
        height: Math.max(doc?.scrollHeight || 0, body?.scrollHeight || 0, window.innerHeight)
      },
      focused_element: describe(document.activeElement), interactive_elements: interactive
    };
  })()`;
}

async function evaluateValue(tabId, expression) {
  const response = await sendCDP(tabId, "Runtime.evaluate", {
    expression,
    returnByValue: true,
    awaitPromise: true,
  });
  if (response.exceptionDetails) {
    throw new Error(response.exceptionDetails.exception?.description || response.exceptionDetails.text || "page evaluation failed");
  }
  return response.result?.value;
}

async function elementCenter(tabId, selector) {
  const encoded = JSON.stringify(selector);
  const result = await evaluateValue(tabId, `(() => {
    const el = document.querySelector(${encoded});
    if (!el) return {ok:false, reason:'target not found'};
    el.scrollIntoView({block:'center', inline:'center'});
    const r = el.getBoundingClientRect();
    if (r.width <= 0 || r.height <= 0) return {ok:false, reason:'target has no visible box'};
    return {ok:true, x:r.left + r.width/2, y:r.top + r.height/2};
  })()`);
  if (!result?.ok) throw new Error(result?.reason || "click target is unavailable");
  return result;
}

async function visualCursorMove(tabId, selector, pulse = false) {
  const encoded = JSON.stringify(selector);
  const rendered = await evaluateValue(tabId, `(() => {
    const target=document.querySelector(${encoded});
    if(!target) return false;
    target.scrollIntoView({block:'center',inline:'center'});
    const rect=target.getBoundingClientRect();
    if(rect.width<=0 || rect.height<=0) return false;
    const styleId='__runtime_visual_cursor_style';
    if(!document.getElementById(styleId)) {
      const style=document.createElement('style');
      style.id=styleId;
      style.textContent='@keyframes __runtimeCursorPulse{0%{transform:scale(.45);opacity:.95}100%{transform:scale(1.55);opacity:0}}';
      (document.head||document.documentElement).appendChild(style);
    }
    let host=document.getElementById('__runtime_visual_cursor');
    if(!host) {
      host=document.createElement('div');
      host.id='__runtime_visual_cursor';
      host.setAttribute('aria-hidden','true');
      Object.assign(host.style,{position:'fixed',left:'0',top:'0',width:'28px',height:'28px',zIndex:'2147483647',pointerEvents:'none',userSelect:'none',opacity:'0',transition:'transform 150ms cubic-bezier(.2,.8,.2,1), opacity 90ms ease',willChange:'transform,opacity',transform:'translate3d('+(window.innerWidth/2)+'px,'+(window.innerHeight/2)+'px,0)'});
      const glow=document.createElement('div');
      Object.assign(glow.style,{position:'absolute',left:'-5px',top:'-5px',width:'30px',height:'30px',border:'2px solid rgba(56,189,248,.92)',borderRadius:'999px',boxShadow:'0 0 9px rgba(56,189,248,.95),0 0 22px rgba(14,165,233,.65),inset 0 0 7px rgba(125,211,252,.42)',background:'rgba(14,165,233,.08)'});
      const arrow=document.createElement('div');
      Object.assign(arrow.style,{position:'absolute',left:'4px',top:'3px',width:'19px',height:'24px',background:'linear-gradient(135deg,#f8fafc 0%,#7dd3fc 48%,#0284c7 100%)',clipPath:'polygon(0 0,0 20px,5px 15px,9px 24px,13px 22px,9px 13px,18px 13px)',filter:'drop-shadow(0 0 4px rgba(125,211,252,1)) drop-shadow(0 0 10px rgba(14,165,233,.85))'});
      host.append(glow,arrow);
      document.documentElement.appendChild(host);
    }
    const x=Math.max(0,Math.min(window.innerWidth-1,rect.left+rect.width/2));
    const y=Math.max(0,Math.min(window.innerHeight-1,rect.top+rect.height/2));
    host.style.opacity='1';
    void host.offsetWidth;
    host.style.transform='translate3d('+(x-7)+'px,'+(y-5)+'px,0)';
    if(${pulse ? "true" : "false"}) {
      const ring=document.createElement('div');
      Object.assign(ring.style,{position:'absolute',left:'-13px',top:'-13px',width:'46px',height:'46px',border:'3px solid rgba(56,189,248,.92)',borderRadius:'999px',boxShadow:'0 0 16px rgba(56,189,248,.9)',animation:'__runtimeCursorPulse 430ms ease-out forwards'});
      host.appendChild(ring);
      setTimeout(()=>ring.remove(),480);
    }
    return true;
  })()`);
  if (rendered) await sleep(170);
}

async function clickElement(tabId, selector) {
  const point = await elementCenter(tabId, selector);
  await sendCDP(tabId, "Input.dispatchMouseEvent", { type: "mouseMoved", x: point.x, y: point.y });
  const encoded = JSON.stringify(selector);
  const clicked = await evaluateValue(tabId, `(() => {
    const el=document.querySelector(${encoded});
    if(!el) return false;
    el.click();
    return true;
  })()`);
  if (!clicked) throw new Error("click target was not found");
}

async function fillElement(tabId, selector, value) {
  const encodedSelector = JSON.stringify(selector);
  const encodedValue = JSON.stringify(String(value ?? ""));
  const result = await evaluateValue(tabId, `(() => {
    const el = document.querySelector(${encodedSelector});
    if (!el) return {ok:false, reason:'target not found'};
    if (el.disabled || el.readOnly) return {ok:false, reason:'target is not editable'};
    const value = ${encodedValue};
    el.focus();
    if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) {
      const prototype = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
      const setter = Object.getOwnPropertyDescriptor(prototype, 'value')?.set;
      if (!setter) return {ok:false, reason:'native value setter unavailable'};
      setter.call(el, value);
    } else if (el.isContentEditable) {
      el.textContent = value;
    } else {
      return {ok:false, reason:'target is not editable'};
    }
    el.dispatchEvent(new Event('input', {bubbles:true}));
    el.dispatchEvent(new Event('change', {bubbles:true}));
    return {ok:true};
  })()`);
  if (!result?.ok) throw new Error(result?.reason || "fill failed");
}

async function typeElement(tabId, selector, text) {
  const encodedSelector = JSON.stringify(selector);
  const focused = await evaluateValue(tabId, `(() => {
    const el = document.querySelector(${encodedSelector});
    if (!el) return false;
    el.focus(); return document.activeElement === el;
  })()`);
  if (!focused) throw new Error("type target was not focused");
  await sendCDP(tabId, "Input.insertText", { text: String(text ?? "") });
}

async function pressKey(tabId, selector, key) {
  if (selector) {
    const encoded = JSON.stringify(selector);
    const focused = await evaluateValue(tabId, `(() => { const el=document.querySelector(${encoded}); if(!el)return false; el.focus(); return true; })()`);
    if (!focused) throw new Error("press target was not found");
  }
  const common = {
    Enter: { code: "Enter", windowsVirtualKeyCode: 13 },
    Tab: { code: "Tab", windowsVirtualKeyCode: 9 },
    Escape: { code: "Escape", windowsVirtualKeyCode: 27 },
    Backspace: { code: "Backspace", windowsVirtualKeyCode: 8 },
    ArrowUp: { code: "ArrowUp", windowsVirtualKeyCode: 38 },
    ArrowDown: { code: "ArrowDown", windowsVirtualKeyCode: 40 },
    ArrowLeft: { code: "ArrowLeft", windowsVirtualKeyCode: 37 },
    ArrowRight: { code: "ArrowRight", windowsVirtualKeyCode: 39 },
  };
  const info = common[key] || { code: key.length === 1 ? `Key${key.toUpperCase()}` : key, windowsVirtualKeyCode: key.length === 1 ? key.toUpperCase().charCodeAt(0) : 0 };
  const base = { key, code: info.code, windowsVirtualKeyCode: info.windowsVirtualKeyCode, nativeVirtualKeyCode: info.windowsVirtualKeyCode };
  await sendCDP(tabId, "Input.dispatchKeyEvent", { ...base, type: "keyDown", text: key.length === 1 ? key : undefined });
  await sendCDP(tabId, "Input.dispatchKeyEvent", { ...base, type: "keyUp" });
}

async function selectElement(tabId, selector, value) {
  const encodedSelector = JSON.stringify(selector);
  const encodedValue = JSON.stringify(String(value ?? ""));
  const result = await evaluateValue(tabId, `(() => {
    const el = document.querySelector(${encodedSelector});
    if (!(el instanceof HTMLSelectElement)) return {ok:false, reason:'select target not found'};
    const value = ${encodedValue};
    if (![...el.options].some(option => option.value === value)) return {ok:false, reason:'option value not found'};
    el.value = value;
    el.dispatchEvent(new Event('input', {bubbles:true}));
    el.dispatchEvent(new Event('change', {bubbles:true}));
    return {ok:el.value === value};
  })()`);
  if (!result?.ok) throw new Error(result?.reason || "select failed");
}

async function uploadFiles(tabId, selector, paths) {
  if (!Array.isArray(paths) || !paths.length) throw new Error("upload requires file paths");
  const document = await sendCDP(tabId, "DOM.getDocument", { depth: 1, pierce: true });
  const queried = await sendCDP(tabId, "DOM.querySelector", { nodeId: document.root.nodeId, selector });
  if (!queried.nodeId) throw new Error("upload target was not found");
  await sendCDP(tabId, "DOM.setFileInputFiles", { nodeId: queried.nodeId, files: paths.map(String) });
}

async function clickAndWaitDownload(tabId, selector, timeoutMs) {
  const timeout = Math.max(1, timeoutMs || 30000);
  let createdListener;
  const created = new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      chrome.downloads.onCreated.removeListener(createdListener);
      reject(new Error("download did not start"));
    }, timeout);
    createdListener = item => {
      if (Number.isInteger(item.tabId) && item.tabId !== tabId) return;
      clearTimeout(timer);
      chrome.downloads.onCreated.removeListener(createdListener);
      resolve(item);
    };
    chrome.downloads.onCreated.addListener(createdListener);
  });
  await clickElement(tabId, selector);
  const item = await created;
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      chrome.downloads.onChanged.removeListener(changed);
      reject(new Error("download timed out"));
    }, timeout);
    const finish = async () => {
      clearTimeout(timer);
      chrome.downloads.onChanged.removeListener(changed);
      const found = await chrome.downloads.search({ id: item.id });
      const finalItem = found[0] || item;
      if (finalItem.state === "interrupted") {
        reject(new Error(finalItem.error || "download interrupted"));
        return;
      }
      resolve({
        guid: String(finalItem.id),
        url: finalItem.finalUrl || finalItem.url || "",
        suggested_filename: finalItem.filename ? finalItem.filename.split(/[\\/]/).pop() : "",
        path: finalItem.filename || "",
        state: finalItem.state || "complete",
        received_bytes: finalItem.bytesReceived || 0,
        total_bytes: finalItem.totalBytes || 0,
      });
    };
    const changed = delta => {
      if (delta.id !== item.id || !delta.state) return;
      if (delta.state.current === "complete" || delta.state.current === "interrupted") void finish();
    };
    chrome.downloads.onChanged.addListener(changed);
    if (item.state === "complete" || item.state === "interrupted") void finish();
  });
}

async function waitForSelector(tabId, selector, state, timeoutMs) {
  const encoded = JSON.stringify(selector);
  const wanted = state || "visible";
  await pollUntil(async () => Boolean(await evaluateValue(tabId, `(() => {
    const el=document.querySelector(${encoded});
    const visible=el => { if(!el)return false; const r=el.getBoundingClientRect(); if(r.width<=0||r.height<=0)return false; const s=getComputedStyle(el); return s.display!=='none'&&s.visibility!=='hidden'&&Number(s.opacity)!==0; };
    if(${JSON.stringify(wanted)}==='attached') return Boolean(el);
    if(${JSON.stringify(wanted)}==='detached') return !el;
    if(${JSON.stringify(wanted)}==='hidden') return !el || !visible(el);
    return visible(el);
  })()`)), timeoutMs || 10000);
}

async function waitForText(tabId, text, exact, state, timeoutMs) {
  const wanted = state || "visible";
  await pollUntil(async () => Boolean(await evaluateValue(tabId, `(() => {
    const expected=${JSON.stringify(String(text))}; const exact=${Boolean(exact)}; const wanted=${JSON.stringify(wanted)};
    const norm=v=>String(v||'').replace(/\\s+/g,' ').trim();
    const visible=el=>{const r=el.getBoundingClientRect(); if(r.width<=0||r.height<=0)return false; const s=getComputedStyle(el); return s.display!=='none'&&s.visibility!=='hidden'&&Number(s.opacity)!==0;};
    const matches=[...document.querySelectorAll('body *')].filter(el=>{const value=norm(el.textContent); return exact ? value===norm(expected) : value.includes(norm(expected));});
    if(wanted==='detached')return matches.length===0; if(wanted==='attached')return matches.length>0;
    if(wanted==='hidden')return matches.length===0||matches.every(el=>!visible(el)); return matches.some(visible);
  })()`)), timeoutMs || 10000);
}

function responseMatches(event, action) {
  if (event.kind !== "network_response") return false;
  if (action.url && !event.url.includes(action.url)) return false;
  if (action.url_pattern) {
    try { if (!(new RegExp(action.url_pattern)).test(event.url)) return false; } catch (_) { return false; }
  }
  if (action.method && String(event.method).toUpperCase() !== String(action.method).toUpperCase()) return false;
  if (action.status && Number(event.status) !== Number(action.status)) return false;
  return true;
}

async function waitForResponse(tabId, action) {
  await pollUntil(async () => eventBuffer(tabId).some(event => responseMatches(event, action)), action.timeout_ms || 10000);
}

async function navigateHistory(tabId, delta, timeoutMs) {
  const history = await sendCDP(tabId, "Page.getNavigationHistory", {});
  const next = history.currentIndex + delta;
  if (next < 0 || next >= history.entries.length) throw new Error("browser history entry is unavailable");
  await sendCDP(tabId, "Page.navigateToHistoryEntry", { entryId: history.entries[next].id });
  await waitForTabComplete(tabId, timeoutMs || 10000);
}

async function snapshotTab(tabId, options = {}, downloads = []) {
  await ensureAttached(tabId);
  if (options.show_cursor === false) await unmarkRuntimeTab(tabId);
  else await markRuntimeTab(tabId);
  const maxText = Math.max(1, Math.min(50000, Number(options.max_text_chars) || 8000));
  const maxDOM = Math.max(1, Math.min(200000, Number(options.max_dom_chars) || 20000));
  const maxInteractive = Math.max(1, Math.min(200, Number(options.max_interactive_elements) || 40));
  const state = await evaluateValue(tabId, domSnapshotExpression(maxInteractive));
  state.text = String(state.text || "").slice(0, maxText);
  state.dom = String(state.dom || "").slice(0, maxDOM);
  const metrics = await sendCDP(tabId, "Page.getLayoutMetrics", {});
  const screenshotParams = { format: "png", fromSurface: true };
  if (options.full_page && metrics.cssContentSize) {
    screenshotParams.captureBeyondViewport = true;
    screenshotParams.clip = {
      x: metrics.cssContentSize.x || 0,
      y: metrics.cssContentSize.y || 0,
      width: metrics.cssContentSize.width,
      height: metrics.cssContentSize.height,
      scale: 1,
    };
  }
  const screenshot = await sendCDP(tabId, "Page.captureScreenshot", screenshotParams);
  const tab = await chrome.tabs.get(tabId);
  const tabs = await chrome.tabs.query({ windowId: tab.windowId });
  const events = takeEvents(tabId);
  return {
    page_id: String(tabId),
    pages: tabs.map(tabSummary),
    url: state.url || tab.url || "",
    title: (state.title || tab.title || "").startsWith(TAB_MARK_PREFIX) ? (state.title || tab.title || "").slice(TAB_MARK_PREFIX.length) : (state.title || tab.title || ""),
    text: state.text || "",
    dom: state.dom || "",
    viewport: state.viewport || { width: 0, height: 0 },
    page_size: state.page_size || { width: 0, height: 0 },
    focused_element: state.focused_element || null,
    interactive_elements: state.interactive_elements || [],
    console_errors: events.filter(event => event.kind === "console_error").map(event => ({ message: event.message || "console error" })),
    network_events: events.filter(event => event.kind === "network_response").map(event => ({
      url: event.url || "", method: event.method || "", status: event.status || 0, mime_type: event.mime_type || "", resource_type: event.resource_type || "",
    })),
    network_errors: events.filter(event => event.kind === "network_error").map(event => ({ url: event.url || "", method: event.method || "", error_text: event.error_text || "network error" })),
    page_errors: events.filter(event => event.kind === "page_error").map(event => ({ message: event.message || "page error" })),
    downloads,
    png_base64: screenshot.data || "",
  };
}

async function runBrowserActions(initialTabId, params) {
  let tabId = initialTabId;
  const showCursor = params.show_cursor !== false;
  if (showCursor) await markRuntimeTab(tabId);
  else await unmarkRuntimeTab(tabId);
  clearEvents(tabId);
  const downloads = [];
  for (const action of params.actions || []) {
    const timeoutMs = Math.max(1, Number(action.timeout_ms) || 10000);
    switch (action.action) {
      case "goto":
        await chrome.tabs.update(tabId, { url: action.url });
        await waitForTabComplete(tabId, timeoutMs);
        if (showCursor) await markRuntimeTab(tabId);
        break;
      case "click":
        if (showCursor) await visualCursorMove(tabId, action.selector, true);
        await clickElement(tabId, action.selector);
        break;
      case "fill":
        if (showCursor) await visualCursorMove(tabId, action.selector, false);
        await fillElement(tabId, action.selector, action.value);
        break;
      case "type":
        if (showCursor) await visualCursorMove(tabId, action.selector, false);
        await typeElement(tabId, action.selector, action.text);
        break;
      case "press":
        if (showCursor && action.selector) await visualCursorMove(tabId, action.selector, false);
        await pressKey(tabId, action.selector || "", String(action.key || ""));
        break;
      case "upload":
        if (showCursor) await visualCursorMove(tabId, action.selector, false);
        await uploadFiles(tabId, action.selector, action.paths || []);
        break;
      case "download":
        if (showCursor) await visualCursorMove(tabId, action.selector, true);
        downloads.push(await clickAndWaitDownload(tabId, action.selector, timeoutMs));
        break;
      case "select":
        if (showCursor) await visualCursorMove(tabId, action.selector, false);
        await selectElement(tabId, action.selector, action.value);
        break;
      case "scroll":
        await evaluateValue(tabId, `window.scrollBy(${Number(action.delta_x) || 0}, ${Number(action.delta_y) || 0})`);
        break;
      case "wait":
        await sleep(Math.max(0, Number(action.value) || 0));
        break;
      case "wait_for_selector":
        await waitForSelector(tabId, action.selector, action.state, timeoutMs);
        break;
      case "wait_for_url":
        await pollUntil(async () => {
          const tab = await chrome.tabs.get(tabId);
          const pattern = String(action.url || "");
          if (pattern.includes("*") || pattern.includes("?")) {
            const regex = new RegExp("^" + pattern.split("").map(ch => ch === "*" ? ".*" : ch === "?" ? "." : ch.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("") + "$");
            return regex.test(tab.url || "");
          }
          return (tab.url || "").includes(pattern);
        }, timeoutMs);
        break;
      case "wait_for_text":
        await waitForText(tabId, action.text, action.exact, action.state, timeoutMs);
        break;
      case "wait_for_response":
        await waitForResponse(tabId, action);
        break;
      case "reload":
        await chrome.tabs.reload(tabId);
        await waitForTabComplete(tabId, timeoutMs);
        if (showCursor) await markRuntimeTab(tabId);
        break;
      case "back":
        await navigateHistory(tabId, -1, timeoutMs);
        if (showCursor) await markRuntimeTab(tabId);
        break;
      case "forward":
        await navigateHistory(tabId, 1, timeoutMs);
        if (showCursor) await markRuntimeTab(tabId);
        break;
      case "tab_new": {
        const previousTabId = tabId;
        const tab = await chrome.tabs.create({ url: action.url || "about:blank", active: true });
        tabId = tab.id;
        if (previousTabId !== tabId) await detachTab(previousTabId).catch(() => {});
        await ensureAttached(tabId);
        if (showCursor) await markRuntimeTab(tabId);
        clearEvents(tabId);
        break;
      }
      case "tab_switch": {
        const previousTabId = tabId;
        tabId = await resolveTabId(action.page_id);
        if (previousTabId !== tabId) await detachTab(previousTabId).catch(() => {});
        await chrome.tabs.update(tabId, { active: true });
        await ensureAttached(tabId);
        if (showCursor) await markRuntimeTab(tabId);
        break;
      }
      case "tab_close": {
        const closeId = action.page_id ? await resolveTabId(action.page_id) : tabId;
        await detachTab(closeId).catch(() => {});
        await chrome.tabs.remove(closeId);
        if (closeId === tabId) tabId = (await activeTab()).id;
        await ensureAttached(tabId);
        if (showCursor) await markRuntimeTab(tabId);
        break;
      }
      default:
        throw new Error(`unsupported browser action: ${action.action}`);
    }
  }
  return snapshotTab(tabId, params, downloads);
}

async function dispatch(method, params = {}) {
  switch (method) {
    case "bridge.status": {
      const tabs = await chrome.tabs.query({});
      return {
        connected: true,
        version: BRIDGE_VERSION,
        extension_id: chrome.runtime.id,
        attached_tabs: [...attachedTabs],
        tabs: tabs.map(tabSummary),
      };
    }
    case "tabs.list": {
      const tabs = await chrome.tabs.query({});
      return { tabs: tabs.map(tabSummary) };
    }
    case "cdp.command": {
      const tabId = await resolveTabId(params.tab_id);
      const result = await sendCDP(tabId, String(params.command || ""), params.command_params || {});
      return { tab_id: tabId, result: result || {} };
    }
    case "cdp.detach": {
      const tabId = await resolveTabId(params.tab_id);
      await detachTab(tabId);
      return { tab_id: tabId, detached: true };
    }
    case "downloads.list": {
      const limit = Math.max(1, Math.min(200, Number(params.limit) || 50));
      const items = await chrome.downloads.search({ orderBy: ["-startTime"], limit });
      return { downloads: items };
    }
    case "browser.snapshot": {
      const tabId = await resolveTabId(params.tab_id ?? params.page_id);
      return snapshotTab(tabId, params, []);
    }
    case "browser.act": {
      const tabId = await resolveTabId(params.tab_id ?? params.page_id);
      return runBrowserActions(tabId, params);
    }
    default:
      throw new Error(`unknown Runtime Chrome bridge method: ${method}`);
  }
}

function connectNative() {
  if (nativePort) return;
  try {
    const port = chrome.runtime.connectNative(NATIVE_HOST);
    nativePort = port;
    port.onMessage.addListener(message => {
      if (!message || typeof message.id !== "string" || typeof message.method !== "string") return;
      dispatch(message.method, message.params || {})
        .then(result => port.postMessage({ id: message.id, ok: true, result }))
        .catch(error => port.postMessage({ id: message.id, ok: false, error: errorText(error) }));
    });
    port.onDisconnect.addListener(() => {
      nativePort = null;
      void chrome.runtime.lastError;
    });
    port.postMessage({ type: "hello", extension_id: chrome.runtime.id, version: BRIDGE_VERSION });
  } catch (_) {
    nativePort = null;
  }
}

chrome.runtime.onInstalled.addListener(() => {
  chrome.alarms.create(RECONNECT_ALARM, { periodInMinutes: 1 });
  connectNative();
});
chrome.runtime.onStartup.addListener(connectNative);
chrome.alarms.onAlarm.addListener(alarm => {
  if (alarm.name === RECONNECT_ALARM) connectNative();
});
chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
  if (message?.type !== "popup_status") return false;
  dispatch("bridge.status", {})
    .then(result => sendResponse({ ok: true, native_connected: Boolean(nativePort), result }))
    .catch(error => sendResponse({ ok: false, native_connected: Boolean(nativePort), error: errorText(error) }));
  return true;
});
connectNative();
