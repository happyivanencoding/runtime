// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package desktop

import (
	"context"
	"errors"
	"strings"
	"sync"
)

type Selector struct {
	RuntimeID    string `json:"runtime_id,omitempty"`
	AutomationID string `json:"automation_id,omitempty"`
	Name         string `json:"name,omitempty"`
	NameContains string `json:"name_contains,omitempty"`
	ControlType  string `json:"control_type,omitempty"`
}

func (s Selector) empty() bool {
	return s.RuntimeID == "" && s.AutomationID == "" && s.Name == "" && s.NameContains == "" && s.ControlType == ""
}
func (s Selector) matches(n Node) bool {
	return (s.RuntimeID == "" || s.RuntimeID == n.RuntimeID) && (s.AutomationID == "" || s.AutomationID == n.AutomationID) && (s.Name == "" || s.Name == n.Name) && (s.NameContains == "" || strings.Contains(strings.ToLower(n.Name), strings.ToLower(s.NameContains))) && (s.ControlType == "" || strings.EqualFold(s.ControlType, n.ControlType))
}

type Rect struct {
	Left   int32 `json:"left"`
	Top    int32 `json:"top"`
	Right  int32 `json:"right"`
	Bottom int32 `json:"bottom"`
}
type Node struct {
	RuntimeID    string   `json:"runtime_id"`
	ParentID     string   `json:"parent_id,omitempty"`
	Depth        int      `json:"depth"`
	Name         string   `json:"name"`
	AutomationID string   `json:"automation_id"`
	ControlType  string   `json:"control_type"`
	ClassName    string   `json:"class_name"`
	ProcessID    int      `json:"process_id"`
	Enabled      bool     `json:"enabled"`
	Offscreen    bool     `json:"offscreen"`
	Focused      bool     `json:"focused"`
	Password     bool     `json:"password"`
	Bounds       Rect     `json:"bounds"`
	Patterns     []string `json:"patterns"`
	Value        *string  `json:"value,omitempty"`
	ToggleState  *int     `json:"toggle_state,omitempty"`
	Selected     *bool    `json:"selected,omitempty"`
}
type Window struct {
	Handle      uint64 `json:"window_handle"`
	ProcessID   uint32 `json:"process_id"`
	Application string `json:"application"`
	Title       string `json:"title"`
	ClassName   string `json:"class_name"`
	Bounds      Rect   `json:"bounds"`
	Foreground  bool   `json:"foreground"`
}
type Request struct {
	Action       string   `json:"action"`
	WindowHandle uint64   `json:"window_handle"`
	ProcessID    uint32   `json:"process_id"`
	Selector     Selector `json:"selector"`
	MaxDepth     int      `json:"max_depth"`
	MaxNodes     int      `json:"max_nodes"`
	Text         string   `json:"text"`
	Direction    string   `json:"direction"`
	Amount       string   `json:"amount"`
}
type Service struct{ mu sync.Mutex }

func New() *Service { return &Service{} }
func (s *Service) Inspect(ctx context.Context, req Request) (map[string]any, error) {
	if req.MaxDepth == 0 {
		req.MaxDepth = 8
	}
	if req.MaxNodes == 0 {
		req.MaxNodes = 150
	}
	if req.MaxDepth < 1 || req.MaxDepth > 20 || req.MaxNodes < 1 || req.MaxNodes > 1000 {
		return nil, errors.New("max_depth must be 1..20 and max_nodes 1..1000")
	}
	if req.Action == "find" && req.Selector.empty() {
		return nil, errors.New("find requires a semantic selector")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return inspectNative(ctx, req)
}
func (s *Service) Act(ctx context.Context, req Request) (map[string]any, error) {
	if req.WindowHandle == 0 || req.Selector.empty() {
		return nil, errors.New("desktop action requires window_handle and a semantic selector")
	}
	if len(req.Text) > 65536 {
		return nil, errors.New("text exceeds 64 KiB")
	}
	switch req.Action {
	case "focus", "press", "type", "scroll", "toggle", "select":
	default:
		return nil, errors.New("action must be focus, press, type, scroll, toggle or select")
	}
	req.MaxDepth = 20
	req.MaxNodes = 1000
	s.mu.Lock()
	defer s.mu.Unlock()
	return actNative(ctx, req)
}
func (s *Service) Clipboard(ctx context.Context, action, text string) (map[string]any, error) {
	if action != "read" && action != "write" {
		return nil, errors.New("clipboard action must be read or write")
	}
	if len(text) > 1<<20 {
		return nil, errors.New("clipboard text exceeds 1 MiB")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return clipboardNative(ctx, action, text)
}
func (s *Service) Screen(ctx context.Context, handle uint64) ([]byte, Rect, error) {
	if handle == 0 {
		return nil, Rect{}, errors.New("screen requires an explicit window_handle")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return screenNative(ctx, handle)
}
