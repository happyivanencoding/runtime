// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package browser

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type extensionSnapshot struct {
	PageID              string               `json:"page_id"`
	Pages               []PageSummary        `json:"pages"`
	URL                 string               `json:"url"`
	Title               string               `json:"title"`
	Text                string               `json:"text"`
	DOM                 string               `json:"dom"`
	Viewport            Viewport             `json:"viewport"`
	PageSize            Size                 `json:"page_size"`
	FocusedElement      *FocusedElement      `json:"focused_element"`
	InteractiveElements []InteractiveElement `json:"interactive_elements"`
	ConsoleErrors       []ConsoleError       `json:"console_errors"`
	NetworkEvents       []NetworkEvent       `json:"network_events"`
	NetworkErrors       []NetworkError       `json:"network_errors"`
	PageErrors          []PageError          `json:"page_errors"`
	Downloads           []Download           `json:"downloads"`
	PNGBase64           string               `json:"png_base64"`
}

func (s *Service) ExtensionStatus(ctx context.Context) (map[string]any, error) {
	result, err := s.bridge.Status(ctx)
	if err != nil {
		return nil, browserError(ErrNotFound, "Runtime Chrome extension bridge is not connected", "extension", nil, err)
	}
	result["transport"] = "extension"
	return result, nil
}

func (s *Service) startExtension(ctx context.Context, req StartRequest) (StartResult, error) {
	if req.Timeout <= 0 {
		req.Timeout = 30 * time.Second
	}
	params := map[string]any{
		"show_cursor":              req.ShowCursor,
		"max_text_chars":           1,
		"max_dom_chars":            1,
		"max_interactive_elements": 1,
	}
	if req.TabID > 0 {
		params["tab_id"] = req.TabID
	}
	var snapshot extensionSnapshot
	if err := s.bridge.Call(ctx, "browser.snapshot", params, &snapshot); err != nil {
		return StartResult{}, browserError(ErrNotFound, "Runtime Chrome extension bridge could not attach a Chrome tab", "extension", nil, err)
	}
	if strings.TrimSpace(req.URL) != "" && strings.TrimSpace(req.URL) != "about:blank" && snapshot.URL != req.URL {
		params["tab_id"] = parseExtensionTabID(snapshot.PageID)
		params["actions"] = []map[string]any{{"action": "goto", "url": req.URL, "timeout_ms": int(req.Timeout / time.Millisecond)}}
		if err := s.bridge.Call(ctx, "browser.act", params, &snapshot); err != nil {
			return StartResult{}, browserError(ErrActionFailed, "navigate Chrome extension tab during session start", "extension", nil, err)
		}
	}
	tabID := parseExtensionTabID(snapshot.PageID)
	if tabID <= 0 {
		return StartResult{}, browserError(ErrCDPFailed, "Chrome extension returned an invalid tab id", "extension", &ErrorDetails{PageID: snapshot.PageID}, nil)
	}
	now := s.now()
	session := &extensionSession{id: newExtensionSessionID(), tabID: tabID, showCursor: req.ShowCursor, createdAt: now, lastActivity: now}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = s.detachExtensionTab(context.Background(), tabID)
		return StartResult{}, browserError(ErrActionFailed, "browser service is closed", "runtime", nil, nil)
	}
	for _, existing := range s.extensionSessions {
		if existing.tabID == tabID {
			s.mu.Unlock()
			return StartResult{}, browserError(ErrProfileInUse, "Chrome extension tab is already owned by a Runtime browser session", "extension", &ErrorDetails{SessionID: existing.id, PageID: snapshot.PageID}, nil)
		}
	}
	s.extensionSessions[session.id] = session
	s.mu.Unlock()
	return StartResult{
		SessionID:      session.id,
		PageID:         snapshot.PageID,
		Pages:          nonNilPages(snapshot.Pages),
		URL:            snapshot.URL,
		Title:          snapshot.Title,
		ConnectionMode: "extension",
	}, nil
}

func (s *Service) extensionAct(ctx context.Context, session *extensionSession, req ActRequest) (Snapshot, error) {
	session.opMu.Lock()
	defer session.opMu.Unlock()
	s.touchExtensionSession(session.id)
	params := map[string]any{
		"tab_id":                   session.tabID,
		"show_cursor":              session.showCursor,
		"actions":                  extensionActionMaps(req.Actions),
		"full_page":                req.FullPage,
		"max_text_chars":           req.MaxTextChars,
		"max_dom_chars":            req.MaxDOMChars,
		"max_interactive_elements": req.MaxInteractiveElements,
	}
	if strings.TrimSpace(req.PageID) != "" {
		params["tab_id"] = parseExtensionTabID(req.PageID)
	}
	var raw extensionSnapshot
	if err := s.bridge.Call(ctx, "browser.act", params, &raw); err != nil {
		return Snapshot{}, browserError(ErrActionFailed, "Chrome extension browser action failed", "extension", nil, err)
	}
	snapshot, err := decodeExtensionSnapshot(raw, session.id)
	if err != nil {
		return Snapshot{}, err
	}
	if tabID := parseExtensionTabID(raw.PageID); tabID > 0 {
		s.mu.Lock()
		if current := s.extensionSessions[session.id]; current == session {
			current.tabID = tabID
			current.lastActivity = s.now()
		}
		s.mu.Unlock()
	}
	return snapshot, nil
}

func (s *Service) extensionSnapshot(ctx context.Context, session *extensionSession, req SnapshotRequest) (Snapshot, error) {
	session.opMu.Lock()
	defer session.opMu.Unlock()
	s.touchExtensionSession(session.id)
	params := map[string]any{
		"tab_id":                   session.tabID,
		"show_cursor":              session.showCursor,
		"full_page":                req.FullPage,
		"max_text_chars":           req.MaxTextChars,
		"max_dom_chars":            req.MaxDOMChars,
		"max_interactive_elements": req.MaxInteractiveElements,
	}
	if strings.TrimSpace(req.PageID) != "" {
		params["tab_id"] = parseExtensionTabID(req.PageID)
	}
	var raw extensionSnapshot
	if err := s.bridge.Call(ctx, "browser.snapshot", params, &raw); err != nil {
		return Snapshot{}, browserError(ErrCDPFailed, "Chrome extension snapshot failed", "extension", nil, err)
	}
	return decodeExtensionSnapshot(raw, session.id)
}

func decodeExtensionSnapshot(raw extensionSnapshot, sessionID string) (Snapshot, error) {
	png, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw.PNGBase64))
	if err != nil || len(png) == 0 {
		return Snapshot{}, browserError(ErrCDPFailed, "Chrome extension returned an invalid PNG screenshot", "extension", nil, err)
	}
	return Snapshot{
		SessionID:           sessionID,
		PageID:              raw.PageID,
		Pages:               nonNilPages(raw.Pages),
		URL:                 raw.URL,
		Title:               raw.Title,
		Text:                raw.Text,
		DOM:                 raw.DOM,
		Viewport:            raw.Viewport,
		PageSize:            raw.PageSize,
		FocusedElement:      raw.FocusedElement,
		InteractiveElements: nonNilInteractive(raw.InteractiveElements),
		ConsoleErrors:       nonNilConsole(raw.ConsoleErrors),
		NetworkEvents:       nonNilNetworkEvents(raw.NetworkEvents),
		NetworkErrors:       nonNilNetwork(raw.NetworkErrors),
		PageErrors:          nonNilPage(raw.PageErrors),
		Downloads:           nonNilDownloads(raw.Downloads),
		PNG:                 png,
	}, nil
}

func (s *Service) getExtensionSession(id string) (*extensionSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.extensionSessions[strings.TrimSpace(id)]
	return session, session != nil
}

func (s *Service) removeExtensionSession(id string) (*extensionSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id = strings.TrimSpace(id)
	session := s.extensionSessions[id]
	if session == nil {
		return nil, false
	}
	delete(s.extensionSessions, id)
	return session, true
}

func (s *Service) touchExtensionSession(id string) {
	s.mu.Lock()
	if session := s.extensionSessions[id]; session != nil {
		session.lastActivity = s.now()
	}
	s.mu.Unlock()
}

func (s *Service) detachExtensionTab(ctx context.Context, tabID int) error {
	var response map[string]any
	return s.bridge.Call(ctx, "cdp.detach", map[string]any{"tab_id": tabID}, &response)
}

func extensionActionMaps(actions []Action) []map[string]any {
	result := make([]map[string]any, 0, len(actions))
	for _, action := range actions {
		item := map[string]any{"action": action.Kind}
		switch action.Kind {
		case "goto":
			item["url"] = action.Goto.URL
			item["wait_until"] = action.Goto.WaitUntil
			item["timeout_ms"] = durationMilliseconds(action.Goto.Timeout)
		case "click":
			item["selector"] = action.Click.Selector
		case "fill":
			item["selector"] = action.Fill.Selector
			item["value"] = action.Fill.Value
		case "type":
			item["selector"] = action.Type.Selector
			item["text"] = action.Type.Text
		case "upload":
			item["selector"] = action.Upload.Selector
			item["paths"] = append([]string(nil), action.Upload.Paths...)
		case "download":
			item["selector"] = action.Download.Selector
			item["timeout_ms"] = durationMilliseconds(action.Download.Timeout)
		case "tab_new":
			item["url"] = action.TabNew.URL
		case "tab_switch":
			item["page_id"] = action.TabSwitch.PageID
		case "tab_close":
			if strings.TrimSpace(action.TabClose.PageID) != "" {
				item["page_id"] = action.TabClose.PageID
			}
		case "press":
			if action.Press.Selector != "" {
				item["selector"] = action.Press.Selector
			}
			item["key"] = action.Press.Key
		case "wait":
			item["value"] = durationMilliseconds(action.Wait.Duration)
		case "wait_for_selector":
			item["selector"] = action.WaitSelector.Selector
			item["state"] = action.WaitSelector.State
			item["timeout_ms"] = durationMilliseconds(action.WaitSelector.Timeout)
		case "wait_for_url":
			item["url"] = action.WaitURL.URL
			item["timeout_ms"] = durationMilliseconds(action.WaitURL.Timeout)
		case "wait_for_text":
			item["text"] = action.WaitText.Text
			item["exact"] = action.WaitText.Exact
			item["state"] = action.WaitText.State
			item["timeout_ms"] = durationMilliseconds(action.WaitText.Timeout)
		case "wait_for_response":
			if action.WaitResponse.URL != "" {
				item["url"] = action.WaitResponse.URL
			}
			if action.WaitResponse.URLPattern != "" {
				item["url_pattern"] = action.WaitResponse.URLPattern
			}
			if action.WaitResponse.Method != "" {
				item["method"] = action.WaitResponse.Method
			}
			if action.WaitResponse.Status != 0 {
				item["status"] = action.WaitResponse.Status
			}
			item["timeout_ms"] = durationMilliseconds(action.WaitResponse.Timeout)
		case "select":
			item["selector"] = action.Select.Selector
			item["value"] = action.Select.Value
		case "scroll":
			item["delta_x"] = action.Scroll.DeltaX
			item["delta_y"] = action.Scroll.DeltaY
		case "reload", "back", "forward":
			item["wait_until"] = action.Navigation.WaitUntil
			item["timeout_ms"] = durationMilliseconds(action.Navigation.Timeout)
		}
		result = append(result, item)
	}
	return result
}

func durationMilliseconds(value time.Duration) int64 {
	if value <= 0 {
		return 0
	}
	return value.Milliseconds()
}

func parseExtensionTabID(pageID string) int {
	value, _ := strconv.Atoi(strings.TrimSpace(pageID))
	return value
}

func newExtensionSessionID() string {
	return fmt.Sprintf("browser-ext-%d", time.Now().UnixNano())
}

func nonNilPages(values []PageSummary) []PageSummary {
	if values == nil {
		return []PageSummary{}
	}
	return values
}

func nonNilInteractive(values []InteractiveElement) []InteractiveElement {
	if values == nil {
		return []InteractiveElement{}
	}
	return values
}
