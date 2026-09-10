package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/computer/desktop"
	toolbrowser "github.com/uvwt/agentdock/internal/tool/browser"
)

func (r *Runtime) browserSession(ctx context.Context, args map[string]any) (Result, error) {
	action, err := requiredString(args, "action")
	if err != nil {
		return browserFailure(err), nil
	}
	switch action {
	case "start":
		if err := validateBrowserKeys(args, "action", "transport", "tab_id", "show_cursor", "url", "browser", "headless", "viewport", "profile_id", "cdp_url", "cookies", "local_storage", "reload_after_local_storage", "timeout_ms"); err != nil {
			return browserFailure(err), nil
		}
		req, err := parseBrowserStart(args)
		if err != nil {
			return browserFailure(err), nil
		}
		started, err := r.browser.Start(ctx, req)
		if err != nil {
			return browserFailure(err), nil
		}
		result := browserResultMap(started)
		result["browser_ok"] = true
		return result, nil
	case "extension_status":
		if err := validateBrowserKeys(args, "action", "timeout_ms"); err != nil {
			return browserFailure(err), nil
		}
		timeout, err := durationArg(args, "timeout_ms", 5*time.Second, time.Millisecond, 30*time.Second)
		if err != nil {
			return browserFailure(err), nil
		}
		statusCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		status, err := r.browser.ExtensionStatus(statusCtx)
		if err != nil {
			return browserFailure(err), nil
		}
		result := browserResultMap(status)
		result["browser_ok"] = true
		return result, nil
	case "close":
		if err := validateBrowserKeys(args, "action", "session_id", "timeout_ms"); err != nil {
			return browserFailure(err), nil
		}
		if _, err := durationArg(args, "timeout_ms", 30*time.Second, time.Millisecond, 5*time.Minute); err != nil {
			return browserFailure(err), nil
		}
		sessionID, err := requiredString(args, "session_id")
		if err != nil {
			return browserFailure(err), nil
		}
		closed, err := r.browser.CloseSession(toolbrowser.CloseRequest{SessionID: sessionID})
		if err != nil {
			return browserFailure(err), nil
		}
		result := browserResultMap(closed)
		result["browser_ok"] = true
		return result, nil
	case "cleanup_stale":
		if err := validateBrowserKeys(args, "action", "max_age_ms", "timeout_ms"); err != nil {
			return browserFailure(err), nil
		}
		if _, err := durationArg(args, "timeout_ms", 30*time.Second, time.Millisecond, 5*time.Minute); err != nil {
			return browserFailure(err), nil
		}
		maxAge, err := durationArg(args, "max_age_ms", 6*time.Hour, time.Millisecond, 365*24*time.Hour)
		if err != nil {
			return browserFailure(err), nil
		}
		cleaned := r.browser.CleanupStale(toolbrowser.CleanupRequest{MaxAge: maxAge})
		result := browserResultMap(cleaned)
		result["browser_ok"] = true
		return result, nil
	default:
		return browserFailure(browserInvalid("unsupported browser_session action", map[string]any{"action": action})), nil
	}
}

func (r *Runtime) browserAct(ctx context.Context, args map[string]any) (Result, error) {
	if err := validateBrowserKeys(args, "session_id", "page_id", "actions", "desktop_fallback", "full_page", "max_text_chars", "max_dom_chars", "max_interactive_elements", "retention_seconds", "close_after", "timeout_ms"); err != nil {
		return browserFailure(err), nil
	}
	sessionID, err := requiredString(args, "session_id")
	if err != nil {
		return browserFailure(err), nil
	}
	actions, err := parseBrowserActions(args["actions"])
	if err != nil {
		return browserFailure(err), nil
	}
	fallback, err := parseDesktopBrowserFallback(args["desktop_fallback"])
	if err != nil {
		return browserFailure(err), nil
	}
	pageID, err := optionalString(args, "page_id", "")
	if err != nil {
		return browserFailure(err), nil
	}
	fullPage, err := boolArgStrict(args, "full_page", false)
	if err != nil {
		return browserFailure(err), nil
	}
	closeAfter, err := boolArgStrict(args, "close_after", false)
	if err != nil {
		return browserFailure(err), nil
	}
	maxText, err := intArgRange(args, "max_text_chars", 8000, 1, 50000)
	if err != nil {
		return browserFailure(err), nil
	}
	maxDOM, err := intArgRange(args, "max_dom_chars", 20000, 1, 200000)
	if err != nil {
		return browserFailure(err), nil
	}
	maxInteractive, err := intArgRange(args, "max_interactive_elements", 40, 1, 200)
	if err != nil {
		return browserFailure(err), nil
	}
	retention, err := intArgRange(args, "retention_seconds", 0, 0, 604800)
	if err != nil {
		return browserFailure(err), nil
	}
	timeout, err := durationArg(args, "timeout_ms", 30*time.Second, time.Millisecond, 5*time.Minute)
	if err != nil {
		return browserFailure(err), nil
	}

	snapshot, err := r.browser.Act(ctx, toolbrowser.ActRequest{
		SessionID: sessionID, PageID: pageID, Actions: actions, FullPage: fullPage,
		MaxTextChars: maxText, MaxDOMChars: maxDOM, MaxInteractiveElements: maxInteractive, Timeout: timeout,
	})
	if err != nil {
		if fallback != nil && browserAllowsDesktopFallback(err) {
			fallbackResult, fallbackErr := r.desktop.Act(ctx, *fallback)
			if fallbackErr == nil {
				browserErr := browserFailure(err)
				return Result{
					"browser_ok": true, "fallback_used": true, "fallback_backend": "windows_uia",
					"browser_error": browserErr["error"], "fallback_result": fallbackResult,
				}, nil
			}
		}
		return browserFailure(err), nil
	}
	result, err := r.publishBrowserSnapshot(ctx, snapshot, retention)
	if err != nil {
		return nil, err
	}
	if closeAfter {
		if _, err := r.browser.CloseSession(toolbrowser.CloseRequest{SessionID: sessionID}); err != nil {
			return browserFailure(err), nil
		}
		result["closed"] = true
	}
	return result, nil
}

func (r *Runtime) browserSnapshot(ctx context.Context, args map[string]any) (Result, error) {
	if err := validateBrowserKeys(args, "session_id", "page_id", "full_page", "max_text_chars", "max_dom_chars", "max_interactive_elements", "retention_seconds", "close_after", "timeout_ms"); err != nil {
		return browserFailure(err), nil
	}
	sessionID, err := requiredString(args, "session_id")
	if err != nil {
		return browserFailure(err), nil
	}
	pageID, err := optionalString(args, "page_id", "")
	if err != nil {
		return browserFailure(err), nil
	}
	fullPage, err := boolArgStrict(args, "full_page", false)
	if err != nil {
		return browserFailure(err), nil
	}
	closeAfter, err := boolArgStrict(args, "close_after", false)
	if err != nil {
		return browserFailure(err), nil
	}
	maxText, err := intArgRange(args, "max_text_chars", 8000, 1, 50000)
	if err != nil {
		return browserFailure(err), nil
	}
	maxDOM, err := intArgRange(args, "max_dom_chars", 20000, 1, 200000)
	if err != nil {
		return browserFailure(err), nil
	}
	maxInteractive, err := intArgRange(args, "max_interactive_elements", 40, 1, 200)
	if err != nil {
		return browserFailure(err), nil
	}
	retention, err := intArgRange(args, "retention_seconds", 0, 0, 604800)
	if err != nil {
		return browserFailure(err), nil
	}
	timeout, err := durationArg(args, "timeout_ms", 30*time.Second, time.Millisecond, 5*time.Minute)
	if err != nil {
		return browserFailure(err), nil
	}

	snapshot, err := r.browser.Snapshot(ctx, toolbrowser.SnapshotRequest{
		SessionID: sessionID, PageID: pageID, FullPage: fullPage,
		MaxTextChars: maxText, MaxDOMChars: maxDOM, MaxInteractiveElements: maxInteractive, Timeout: timeout,
	})
	if err != nil {
		return browserFailure(err), nil
	}
	result, err := r.publishBrowserSnapshot(ctx, snapshot, retention)
	if err != nil {
		return nil, err
	}
	if closeAfter {
		if _, err := r.browser.CloseSession(toolbrowser.CloseRequest{SessionID: sessionID}); err != nil {
			return browserFailure(err), nil
		}
		result["closed"] = true
	}
	return result, nil
}

func (r *Runtime) publishBrowserSnapshot(ctx context.Context, snapshot toolbrowser.Snapshot, retention int) (Result, error) {
	published, err := r.media.PublishBrowserScreenshot(ctx, snapshot.PNG, retention)
	if err != nil {
		return nil, err
	}
	result := browserResultMap(snapshot)
	result["browser_ok"] = true
	result["screenshot"] = published
	if len(snapshot.Downloads) > 0 {
		downloadArtifacts := make([]any, 0, len(snapshot.Downloads))
		for _, download := range snapshot.Downloads {
			if strings.TrimSpace(download.Path) == "" {
				continue
			}
			artifact, err := r.media.FilePublish(ctx, map[string]any{"path": download.Path, "retention_seconds": retention})
			if err != nil {
				return nil, fmt.Errorf("publish browser download %s: %w", download.SuggestedFilename, err)
			}
			downloadArtifacts = append(downloadArtifacts, map[string]any{
				"guid": download.GUID, "suggested_filename": download.SuggestedFilename, "artifact": artifact,
			})
		}
		result["download_artifacts"] = downloadArtifacts
	}
	return result, nil
}

func parseDesktopBrowserFallback(raw any) (*desktop.Request, error) {
	if raw == nil {
		return nil, nil
	}
	value, ok := raw.(map[string]any)
	if !ok {
		return nil, browserInvalid("desktop_fallback must be an object", map[string]any{"field": "desktop_fallback"})
	}
	if err := validateBrowserKeys(value, "action", "window_handle", "selector", "text", "direction", "amount"); err != nil {
		return nil, err
	}
	action, err := requiredString(value, "action")
	if err != nil {
		return nil, err
	}
	switch action {
	case "focus", "press", "type", "scroll", "toggle", "select":
	default:
		return nil, browserInvalid("desktop_fallback action must be focus, press, type, scroll, toggle, or select", map[string]any{"field": "action"})
	}
	if action == "type" {
		if _, exists := value["text"]; !exists {
			return nil, browserInvalid("desktop_fallback type requires an explicit text field", map[string]any{"field": "text"})
		}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, browserInvalid("desktop_fallback could not be encoded", map[string]any{"field": "desktop_fallback"})
	}
	var request desktop.Request
	if err := json.Unmarshal(encoded, &request); err != nil {
		return nil, browserInvalid("desktop_fallback has invalid field types", map[string]any{"field": "desktop_fallback"})
	}
	if request.WindowHandle == 0 {
		return nil, browserInvalid("desktop_fallback requires window_handle", map[string]any{"field": "window_handle"})
	}
	if request.Selector.RuntimeID == "" && request.Selector.AutomationID == "" && request.Selector.Name == "" && request.Selector.NameContains == "" && request.Selector.ControlType == "" {
		return nil, browserInvalid("desktop_fallback requires a semantic UI Automation selector", map[string]any{"field": "selector"})
	}
	return &request, nil
}

func browserAllowsDesktopFallback(err error) bool {
	var browserErr *toolbrowser.Error
	if !errors.As(err, &browserErr) {
		return false
	}
	switch browserErr.Code {
	case toolbrowser.ErrActionFailed, toolbrowser.ErrTimeout, toolbrowser.ErrCDPFailed, toolbrowser.ErrPageNotFound, toolbrowser.ErrNotFound:
		return true
	default:
		return false
	}
}

func parseBrowserStart(args map[string]any) (toolbrowser.StartRequest, error) {
	transport, err := optionalString(args, "transport", "cdp")
	if err != nil {
		return toolbrowser.StartRequest{}, err
	}
	transport = strings.ToLower(strings.TrimSpace(transport))
	if transport != "cdp" && transport != "extension" {
		return toolbrowser.StartRequest{}, browserInvalid("transport must be cdp or extension", map[string]any{"field": "transport"})
	}
	defaultURL := "about:blank"
	if transport == "extension" {
		defaultURL = ""
	}
	urlValue, err := optionalString(args, "url", defaultURL)
	if err != nil {
		return toolbrowser.StartRequest{}, err
	}
	tabID, err := intArgRange(args, "tab_id", 0, 1, math.MaxInt32)
	if err != nil {
		return toolbrowser.StartRequest{}, err
	}
	showCursor, err := boolArgStrict(args, "show_cursor", true)
	if err != nil {
		return toolbrowser.StartRequest{}, err
	}
	browserValue, err := optionalString(args, "browser", string(toolbrowser.BrowserAuto))
	if err != nil {
		return toolbrowser.StartRequest{}, err
	}
	kind := toolbrowser.Kind(strings.ToLower(browserValue))
	switch kind {
	case toolbrowser.BrowserAuto, toolbrowser.BrowserChrome, toolbrowser.BrowserChromium, toolbrowser.BrowserEdge:
	default:
		return toolbrowser.StartRequest{}, browserInvalid("browser must be auto, chrome, chromium, or edge", map[string]any{"browser": browserValue})
	}
	headless, err := boolArgStrict(args, "headless", true)
	if err != nil {
		return toolbrowser.StartRequest{}, err
	}
	viewport, err := parseViewport(args["viewport"])
	if err != nil {
		return toolbrowser.StartRequest{}, err
	}
	profileID, err := optionalString(args, "profile_id", "")
	if err != nil {
		return toolbrowser.StartRequest{}, err
	}
	cdpURL, err := optionalString(args, "cdp_url", "")
	if err != nil {
		return toolbrowser.StartRequest{}, err
	}
	cookies, err := parseCookies(args["cookies"])
	if err != nil {
		return toolbrowser.StartRequest{}, err
	}
	storage, err := parseLocalStorage(args["local_storage"])
	if err != nil {
		return toolbrowser.StartRequest{}, err
	}
	reload, err := boolArgStrict(args, "reload_after_local_storage", true)
	if err != nil {
		return toolbrowser.StartRequest{}, err
	}
	timeout, err := durationArg(args, "timeout_ms", 30*time.Second, time.Millisecond, 5*time.Minute)
	if err != nil {
		return toolbrowser.StartRequest{}, err
	}
	return toolbrowser.StartRequest{
		Transport: transport, TabID: tabID, ShowCursor: showCursor,
		URL: urlValue, Browser: kind, Headless: headless, Viewport: viewport,
		ProfileID: profileID, CDPURL: cdpURL,
		Cookies: cookies, LocalStorage: storage,
		ReloadAfterLocalStorage: reload, Timeout: timeout,
	}, nil
}

func parseViewport(raw any) (toolbrowser.Viewport, error) {
	if raw == nil {
		return toolbrowser.Viewport{Width: 1280, Height: 800}, nil
	}
	value, ok := raw.(map[string]any)
	if !ok {
		return toolbrowser.Viewport{}, browserInvalid("viewport must be an object", nil)
	}
	if err := validateBrowserKeys(value, "width", "height"); err != nil {
		return toolbrowser.Viewport{}, err
	}
	width, err := intArgRange(value, "width", 1280, 320, 7680)
	if err != nil {
		return toolbrowser.Viewport{}, err
	}
	height, err := intArgRange(value, "height", 800, 200, 4320)
	if err != nil {
		return toolbrowser.Viewport{}, err
	}
	return toolbrowser.Viewport{Width: width, Height: height}, nil
}

func parseCookies(raw any) ([]toolbrowser.Cookie, error) {
	if raw == nil {
		return nil, nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil, browserInvalid("cookies must be an array", nil)
	}
	cookies := make([]toolbrowser.Cookie, 0, len(values))
	for index, rawCookie := range values {
		value, ok := rawCookie.(map[string]any)
		if !ok {
			return nil, browserInvalid("each cookie must be an object", map[string]any{"index": index})
		}
		if err := validateBrowserKeys(value, "name", "value", "url", "domain", "path", "expires", "http_only", "secure", "same_site"); err != nil {
			return nil, err
		}
		name, err := requiredString(value, "name")
		if err != nil {
			return nil, err
		}
		cookieValue, err := requiredStringAllowEmpty(value, "value")
		if err != nil {
			return nil, err
		}
		urlValue, err := optionalString(value, "url", "")
		if err != nil {
			return nil, err
		}
		domain, err := optionalString(value, "domain", "")
		if err != nil {
			return nil, err
		}
		if urlValue == "" && domain == "" {
			return nil, browserInvalid("cookie requires url or domain", map[string]any{"index": index})
		}
		pathValue, err := optionalString(value, "path", "")
		if err != nil {
			return nil, err
		}
		expires, err := floatArg(value, "expires", 0)
		if err != nil || expires < 0 {
			return nil, browserInvalid("cookie expires must be a non-negative number", map[string]any{"index": index})
		}
		httpOnly, err := boolArgStrict(value, "http_only", false)
		if err != nil {
			return nil, err
		}
		secure, err := boolArgStrict(value, "secure", false)
		if err != nil {
			return nil, err
		}
		sameSite, err := optionalString(value, "same_site", "")
		if err != nil {
			return nil, err
		}
		if sameSite != "" {
			sameSite = strings.ToLower(sameSite)
			if sameSite != "strict" && sameSite != "lax" && sameSite != "none" {
				return nil, browserInvalid("cookie same_site must be strict, lax, or none", map[string]any{"index": index})
			}
		}
		cookies = append(cookies, toolbrowser.Cookie{Name: name, Value: cookieValue, URL: urlValue, Domain: domain, Path: pathValue, Expires: expires, HTTPOnly: httpOnly, Secure: secure, SameSite: sameSite})
	}
	return cookies, nil
}

func parseLocalStorage(raw any) (map[string]map[string]string, error) {
	if raw == nil {
		return nil, nil
	}
	origins, ok := raw.(map[string]any)
	if !ok {
		return nil, browserInvalid("local_storage must be an object keyed by origin", nil)
	}
	result := make(map[string]map[string]string, len(origins))
	for origin, rawEntries := range origins {
		entries, ok := rawEntries.(map[string]any)
		if !ok || strings.TrimSpace(origin) == "" {
			return nil, browserInvalid("local_storage origins must contain string maps", map[string]any{"origin": origin})
		}
		typed := make(map[string]string, len(entries))
		for key, rawValue := range entries {
			value, ok := rawValue.(string)
			if !ok {
				return nil, browserInvalid("local_storage values must be strings", map[string]any{"origin": origin, "key": key})
			}
			typed[key] = value
		}
		result[origin] = typed
	}
	return result, nil
}

func parseBrowserActions(raw any) ([]toolbrowser.Action, error) {
	values, ok := raw.([]any)
	if !ok || len(values) == 0 {
		return nil, browserInvalid("actions must be a non-empty array", nil)
	}
	if len(values) > 100 {
		return nil, browserInvalid("actions cannot contain more than 100 items", map[string]any{"count": len(values)})
	}
	actions := make([]toolbrowser.Action, 0, len(values))
	for index, rawAction := range values {
		value, ok := rawAction.(map[string]any)
		if !ok {
			return nil, browserInvalid("each action must be an object", map[string]any{"action_index": index})
		}
		action, err := parseBrowserAction(value)
		if err != nil {
			var browserErr *toolbrowser.Error
			if errors.As(err, &browserErr) {
				if browserErr.Details == nil {
					browserErr.Details = &toolbrowser.ErrorDetails{}
				}
				browserErr.Details.ActionIndex = browserIntPointer(index)
			}
			return nil, err
		}
		actions = append(actions, action)
	}
	return actions, nil
}

func parseBrowserAction(value map[string]any) (toolbrowser.Action, error) {
	name, err := requiredString(value, "action")
	if err != nil {
		return toolbrowser.Action{}, err
	}
	timeout := func() (time.Duration, error) {
		return durationArg(value, "timeout_ms", 10*time.Second, time.Millisecond, 5*time.Minute)
	}
	waitUntil := func() (toolbrowser.WaitUntil, error) {
		raw, err := optionalString(value, "wait_until", string(toolbrowser.WaitLoad))
		if err != nil {
			return "", err
		}
		parsed := toolbrowser.WaitUntil(strings.ToLower(raw))
		if parsed != toolbrowser.WaitLoad && parsed != toolbrowser.WaitDOMContentLoaded {
			return "", browserInvalid("wait_until must be domcontentloaded or load", map[string]any{"wait_until": raw})
		}
		return parsed, nil
	}
	state := func() (toolbrowser.ElementState, error) {
		raw, err := optionalString(value, "state", string(toolbrowser.StateVisible))
		if err != nil {
			return "", err
		}
		parsed := toolbrowser.ElementState(strings.ToLower(raw))
		switch parsed {
		case toolbrowser.StateVisible, toolbrowser.StateHidden, toolbrowser.StateAttached, toolbrowser.StateDetached:
			return parsed, nil
		default:
			return "", browserInvalid("state must be visible, hidden, attached, or detached", map[string]any{"state": raw})
		}
	}

	switch name {
	case "goto":
		if err := validateBrowserKeys(value, "action", "url", "wait_until", "timeout_ms"); err != nil {
			return toolbrowser.Action{}, err
		}
		urlValue, err := requiredString(value, "url")
		if err != nil {
			return toolbrowser.Action{}, err
		}
		wait, err := waitUntil()
		if err != nil {
			return toolbrowser.Action{}, err
		}
		t, err := timeout()
		if err != nil {
			return toolbrowser.Action{}, err
		}
		return toolbrowser.Action{Kind: name, Goto: &toolbrowser.GotoAction{URL: urlValue, WaitUntil: wait, Timeout: t}}, nil
	case "click":
		if err := validateBrowserKeys(value, "action", "selector"); err != nil {
			return toolbrowser.Action{}, err
		}
		selector, err := requiredCSSSelector(value)
		if err != nil {
			return toolbrowser.Action{}, err
		}
		return toolbrowser.Action{Kind: name, Click: &toolbrowser.ClickAction{Selector: selector}}, nil
	case "fill":
		if err := validateBrowserKeys(value, "action", "selector", "value"); err != nil {
			return toolbrowser.Action{}, err
		}
		selector, err := requiredCSSSelector(value)
		if err != nil {
			return toolbrowser.Action{}, err
		}
		text, err := requiredStringAllowEmpty(value, "value")
		if err != nil {
			return toolbrowser.Action{}, err
		}
		return toolbrowser.Action{Kind: name, Fill: &toolbrowser.FillAction{Selector: selector, Value: text}}, nil
	case "type":
		if err := validateBrowserKeys(value, "action", "selector", "text"); err != nil {
			return toolbrowser.Action{}, err
		}
		selector, err := requiredCSSSelector(value)
		if err != nil {
			return toolbrowser.Action{}, err
		}
		text, err := requiredStringAllowEmpty(value, "text")
		if err != nil {
			return toolbrowser.Action{}, err
		}
		return toolbrowser.Action{Kind: name, Type: &toolbrowser.TypeAction{Selector: selector, Text: text}}, nil
	case "upload":
		if err := validateBrowserKeys(value, "action", "selector", "paths"); err != nil {
			return toolbrowser.Action{}, err
		}
		selector, err := requiredCSSSelector(value)
		if err != nil {
			return toolbrowser.Action{}, err
		}
		paths, err := browserStringArray(value["paths"], "paths", 1, 32)
		if err != nil {
			return toolbrowser.Action{}, err
		}
		return toolbrowser.Action{Kind: name, Upload: &toolbrowser.UploadAction{Selector: selector, Paths: paths}}, nil
	case "download":
		if err := validateBrowserKeys(value, "action", "selector", "timeout_ms"); err != nil {
			return toolbrowser.Action{}, err
		}
		selector, err := requiredCSSSelector(value)
		if err != nil {
			return toolbrowser.Action{}, err
		}
		t, err := timeout()
		if err != nil {
			return toolbrowser.Action{}, err
		}
		return toolbrowser.Action{Kind: name, Download: &toolbrowser.DownloadAction{Selector: selector, Timeout: t}}, nil
	case "tab_new":
		if err := validateBrowserKeys(value, "action", "url"); err != nil {
			return toolbrowser.Action{}, err
		}
		urlValue, err := optionalString(value, "url", "about:blank")
		if err != nil {
			return toolbrowser.Action{}, err
		}
		return toolbrowser.Action{Kind: name, TabNew: &toolbrowser.TabNewAction{URL: urlValue}}, nil
	case "tab_switch":
		if err := validateBrowserKeys(value, "action", "page_id"); err != nil {
			return toolbrowser.Action{}, err
		}
		pageID, err := requiredString(value, "page_id")
		if err != nil {
			return toolbrowser.Action{}, err
		}
		return toolbrowser.Action{Kind: name, TabSwitch: &toolbrowser.TabSwitchAction{PageID: pageID}}, nil
	case "tab_close":
		if err := validateBrowserKeys(value, "action", "page_id"); err != nil {
			return toolbrowser.Action{}, err
		}
		pageID, err := optionalString(value, "page_id", "")
		if err != nil {
			return toolbrowser.Action{}, err
		}
		return toolbrowser.Action{Kind: name, TabClose: &toolbrowser.TabCloseAction{PageID: pageID}}, nil
	case "press":
		if err := validateBrowserKeys(value, "action", "selector", "key"); err != nil {
			return toolbrowser.Action{}, err
		}
		selector, err := optionalString(value, "selector", "")
		if err != nil {
			return toolbrowser.Action{}, err
		}
		if err := rejectNonCSSSelector(selector); err != nil {
			return toolbrowser.Action{}, err
		}
		key, err := requiredString(value, "key")
		if err != nil {
			return toolbrowser.Action{}, err
		}
		return toolbrowser.Action{Kind: name, Press: &toolbrowser.PressAction{Selector: selector, Key: key}}, nil
	case "wait":
		if err := validateBrowserKeys(value, "action", "value"); err != nil {
			return toolbrowser.Action{}, err
		}
		ms, err := intArgRange(value, "value", -1, 0, 300000)
		if err != nil || ms < 0 {
			return toolbrowser.Action{}, browserInvalid("wait requires integer value in milliseconds", nil)
		}
		return toolbrowser.Action{Kind: name, Wait: &toolbrowser.WaitAction{Duration: time.Duration(ms) * time.Millisecond}}, nil
	case "wait_for_selector":
		if err := validateBrowserKeys(value, "action", "selector", "state", "timeout_ms"); err != nil {
			return toolbrowser.Action{}, err
		}
		selector, err := requiredCSSSelector(value)
		if err != nil {
			return toolbrowser.Action{}, err
		}
		st, err := state()
		if err != nil {
			return toolbrowser.Action{}, err
		}
		t, err := timeout()
		if err != nil {
			return toolbrowser.Action{}, err
		}
		return toolbrowser.Action{Kind: name, WaitSelector: &toolbrowser.WaitSelectorAction{Selector: selector, State: st, Timeout: t}}, nil
	case "wait_for_url":
		if err := validateBrowserKeys(value, "action", "url", "timeout_ms"); err != nil {
			return toolbrowser.Action{}, err
		}
		urlValue, err := requiredString(value, "url")
		if err != nil {
			return toolbrowser.Action{}, err
		}
		t, err := timeout()
		if err != nil {
			return toolbrowser.Action{}, err
		}
		return toolbrowser.Action{Kind: name, WaitURL: &toolbrowser.WaitURLAction{URL: urlValue, Timeout: t}}, nil
	case "wait_for_text":
		if err := validateBrowserKeys(value, "action", "text", "exact", "state", "timeout_ms"); err != nil {
			return toolbrowser.Action{}, err
		}
		text, err := requiredString(value, "text")
		if err != nil {
			return toolbrowser.Action{}, err
		}
		exact, err := boolArgStrict(value, "exact", false)
		if err != nil {
			return toolbrowser.Action{}, err
		}
		st, err := state()
		if err != nil {
			return toolbrowser.Action{}, err
		}
		t, err := timeout()
		if err != nil {
			return toolbrowser.Action{}, err
		}
		return toolbrowser.Action{Kind: name, WaitText: &toolbrowser.WaitTextAction{Text: text, Exact: exact, State: st, Timeout: t}}, nil
	case "wait_for_response":
		if err := validateBrowserKeys(value, "action", "url", "url_pattern", "method", "status", "timeout_ms"); err != nil {
			return toolbrowser.Action{}, err
		}
		urlValue, err := optionalString(value, "url", "")
		if err != nil {
			return toolbrowser.Action{}, err
		}
		pattern, err := optionalString(value, "url_pattern", "")
		if err != nil {
			return toolbrowser.Action{}, err
		}
		if urlValue == "" && pattern == "" {
			return toolbrowser.Action{}, browserInvalid("wait_for_response requires url or url_pattern", nil)
		}
		if pattern != "" {
			if _, err := regexp.Compile(pattern); err != nil {
				return toolbrowser.Action{}, browserInvalid("url_pattern must be a valid regular expression", map[string]any{"reason": err.Error()})
			}
		}
		method, err := optionalString(value, "method", "")
		if err != nil {
			return toolbrowser.Action{}, err
		}
		method = strings.ToUpper(method)
		status := 0
		if _, exists := value["status"]; exists {
			status, err = intArgRange(value, "status", 0, 100, 599)
			if err != nil {
				return toolbrowser.Action{}, err
			}
		}
		t, err := timeout()
		if err != nil {
			return toolbrowser.Action{}, err
		}
		return toolbrowser.Action{Kind: name, WaitResponse: &toolbrowser.WaitResponseAction{URL: urlValue, URLPattern: pattern, Method: method, Status: status, Timeout: t}}, nil
	case "select":
		if err := validateBrowserKeys(value, "action", "selector", "value"); err != nil {
			return toolbrowser.Action{}, err
		}
		selector, err := requiredCSSSelector(value)
		if err != nil {
			return toolbrowser.Action{}, err
		}
		selected, err := requiredStringAllowEmpty(value, "value")
		if err != nil {
			return toolbrowser.Action{}, err
		}
		return toolbrowser.Action{Kind: name, Select: &toolbrowser.SelectAction{Selector: selector, Value: selected}}, nil
	case "scroll":
		if err := validateBrowserKeys(value, "action", "delta_x", "delta_y"); err != nil {
			return toolbrowser.Action{}, err
		}
		dx, err := intArgRange(value, "delta_x", 0, -100000, 100000)
		if err != nil {
			return toolbrowser.Action{}, err
		}
		dy, err := intArgRange(value, "delta_y", 0, -100000, 100000)
		if err != nil {
			return toolbrowser.Action{}, err
		}
		if dx == 0 && dy == 0 {
			return toolbrowser.Action{}, browserInvalid("scroll requires a non-zero delta_x or delta_y", nil)
		}
		return toolbrowser.Action{Kind: name, Scroll: &toolbrowser.ScrollAction{DeltaX: int64(dx), DeltaY: int64(dy)}}, nil
	case "reload", "back", "forward":
		if err := validateBrowserKeys(value, "action", "wait_until", "timeout_ms"); err != nil {
			return toolbrowser.Action{}, err
		}
		wait, err := waitUntil()
		if err != nil {
			return toolbrowser.Action{}, err
		}
		t, err := timeout()
		if err != nil {
			return toolbrowser.Action{}, err
		}
		return toolbrowser.Action{Kind: name, Navigation: &toolbrowser.NavigationAction{WaitUntil: wait, Timeout: t}}, nil
	default:
		return toolbrowser.Action{}, browserInvalid("unknown browser action", map[string]any{"action": name})
	}
}

func browserStringArray(raw any, field string, minItems, maxItems int) ([]string, error) {
	values, ok := raw.([]any)
	if !ok || len(values) < minItems || len(values) > maxItems {
		return nil, browserInvalid(fmt.Sprintf("%s must contain between %d and %d strings", field, minItems, maxItems), map[string]any{"field": field})
	}
	result := make([]string, 0, len(values))
	for index, rawValue := range values {
		value, ok := rawValue.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, browserInvalid(field+" values must be non-empty strings", map[string]any{"field": field, "index": index})
		}
		result = append(result, strings.TrimSpace(value))
	}
	return result, nil
}

func validateBrowserKeys(values map[string]any, allowed ...string) error {
	set := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		set[key] = struct{}{}
	}
	for key := range values {
		if _, ok := set[key]; !ok {
			return browserInvalid("unknown browser field", map[string]any{"field": key})
		}
	}
	return nil
}

func requiredCSSSelector(values map[string]any) (string, error) {
	selector, err := requiredString(values, "selector")
	if err != nil {
		return "", err
	}
	if err := rejectNonCSSSelector(selector); err != nil {
		return "", err
	}
	return selector, nil
}

func rejectNonCSSSelector(selector string) error {
	trimmed := strings.TrimSpace(selector)
	if trimmed == "" {
		return nil
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(lower, "xpath=") || strings.HasPrefix(lower, "text=") || strings.HasPrefix(lower, "role=") {
		return browserInvalid("selector must be CSS", map[string]any{"selector": selector})
	}
	return nil
}

func requiredString(values map[string]any, key string) (string, error) {
	value, err := requiredStringAllowEmpty(values, key)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", browserInvalid(key+" is required", map[string]any{"field": key})
	}
	return strings.TrimSpace(value), nil
}

func requiredStringAllowEmpty(values map[string]any, key string) (string, error) {
	raw, ok := values[key]
	if !ok {
		return "", browserInvalid(key+" is required", map[string]any{"field": key})
	}
	value, ok := raw.(string)
	if !ok {
		return "", browserInvalid(key+" must be a string", map[string]any{"field": key})
	}
	return value, nil
}

func optionalString(values map[string]any, key, fallback string) (string, error) {
	raw, ok := values[key]
	if !ok || raw == nil {
		return fallback, nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", browserInvalid(key+" must be a string", map[string]any{"field": key})
	}
	return strings.TrimSpace(value), nil
}

func boolArgStrict(values map[string]any, key string, fallback bool) (bool, error) {
	raw, ok := values[key]
	if !ok || raw == nil {
		return fallback, nil
	}
	value, ok := raw.(bool)
	if !ok {
		return false, browserInvalid(key+" must be a boolean", map[string]any{"field": key})
	}
	return value, nil
}

func intArgRange(values map[string]any, key string, fallback, minimum, maximum int) (int, error) {
	raw, ok := values[key]
	if !ok || raw == nil {
		return fallback, nil
	}
	value, ok := integerValue(raw)
	if !ok || value < int64(minimum) || value > int64(maximum) {
		return 0, browserInvalid(fmt.Sprintf("%s must be an integer between %d and %d", key, minimum, maximum), map[string]any{"field": key})
	}
	return int(value), nil
}

func durationArg(values map[string]any, key string, fallback, unit, maximum time.Duration) (time.Duration, error) {
	fallbackUnits := int64(fallback / unit)
	maxUnits := int(maximum / unit)
	value, err := intArgRange(values, key, int(fallbackUnits), 1, maxUnits)
	if err != nil {
		return 0, err
	}
	return time.Duration(value) * unit, nil
}

func floatArg(values map[string]any, key string, fallback float64) (float64, error) {
	raw, ok := values[key]
	if !ok || raw == nil {
		return fallback, nil
	}
	switch value := raw.(type) {
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return 0, browserInvalid(key+" must be finite", nil)
		}
		return value, nil
	case float32:
		return float64(value), nil
	case int:
		return float64(value), nil
	case int64:
		return float64(value), nil
	case json.Number:
		parsed, err := value.Float64()
		if err != nil {
			return 0, browserInvalid(key+" must be numeric", nil)
		}
		return parsed, nil
	default:
		return 0, browserInvalid(key+" must be numeric", map[string]any{"field": key})
	}
}

func integerValue(raw any) (int64, bool) {
	switch value := raw.(type) {
	case int:
		return int64(value), true
	case int8:
		return int64(value), true
	case int16:
		return int64(value), true
	case int32:
		return int64(value), true
	case int64:
		return value, true
	case uint:
		if uint64(value) <= math.MaxInt64 {
			return int64(value), true
		}
	case uint64:
		if value <= math.MaxInt64 {
			return int64(value), true
		}
	case float64:
		if math.Trunc(value) == value && value >= math.MinInt64 && value <= math.MaxInt64 {
			return int64(value), true
		}
	case json.Number:
		parsed, err := value.Int64()
		return parsed, err == nil
	}
	return 0, false
}

func browserInvalid(message string, details map[string]any) *toolbrowser.Error {
	typed := &toolbrowser.ErrorDetails{}
	hasDetails := false
	if details != nil {
		if value, ok := details["field"].(string); ok {
			typed.Field = value
			hasDetails = true
		}
		if value, ok := details["reason"].(string); ok {
			typed.Reason = value
			hasDetails = true
		}
		if value, ok := details["selector"].(string); ok {
			typed.Selector = value
			hasDetails = true
		}
		if value, ok := details["browser"].(string); ok {
			typed.Browser = toolbrowser.Kind(value)
			hasDetails = true
		}
		if value, ok := details["action"].(string); ok {
			typed.Action = value
			hasDetails = true
		}
		if value, ok := integerValue(details["index"]); ok {
			index := int(value)
			typed.ItemIndex = &index
			hasDetails = true
		}
		if value, ok := integerValue(details["count"]); ok {
			typed.Count = int(value)
			hasDetails = true
		}
	}
	if !hasDetails {
		typed = nil
	}
	return &toolbrowser.Error{Code: toolbrowser.ErrActionInvalid, Message: message, Phase: "validation", Details: typed}
}

func browserIntPointer(value int) *int { return &value }

func browserFailure(err error) Result {
	var browserErr *toolbrowser.Error
	if !errors.As(err, &browserErr) {
		browserErr = &toolbrowser.Error{Code: toolbrowser.ErrCDPFailed, Message: "browser operation failed", Phase: "runtime", Cause: err}
	}
	errorValue := map[string]any{"code": browserErr.Code, "message": browserErr.Message, "phase": browserErr.Phase}
	if browserErr.Details != nil {
		errorValue["details"] = browserErr.Details
	}
	return Result{"browser_ok": false, "code": browserErr.Code, "error": errorValue}
}

func browserResultMap(value any) Result {
	encoded, _ := json.Marshal(value)
	result := Result{}
	_ = json.Unmarshal(encoded, &result)
	return result
}
