//go:build windows && amd64

// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package desktop

import (
	"context"
	"errors"
	"strconv"
	"unsafe"
)

var roles = map[int32]string{50000: "button", 50001: "calendar", 50002: "checkbox", 50003: "combobox", 50004: "edit", 50005: "hyperlink", 50006: "image", 50007: "listitem", 50008: "list", 50009: "menu", 50010: "menubar", 50011: "menuitem", 50012: "progressbar", 50013: "radiobutton", 50014: "scrollbar", 50015: "slider", 50016: "spinner", 50017: "statusbar", 50018: "tab", 50019: "tabitem", 50020: "text", 50021: "toolbar", 50022: "tooltip", 50023: "tree", 50024: "treeitem", 50025: "custom", 50026: "group", 50027: "thumb", 50028: "datagrid", 50029: "dataitem", 50030: "document", 50031: "splitbutton", 50032: "window", 50033: "pane", 50034: "header", 50035: "headeritem", 50036: "table", 50037: "titlebar", 50038: "separator"}
var patterns = []struct {
	id   int
	name string
}{{10000, "invoke"}, {10002, "value"}, {10004, "scroll"}, {10010, "selection_item"}, {10015, "toggle"}}

func snapshot(element *com, depth int, parent string) (Node, error) {
	node := Node{Depth: depth, ParentID: parent, Patterns: []string{}}
	var err error
	node.Name, err = element.stringValue(23)
	if err != nil {
		return node, err
	}
	node.RuntimeID, err = element.runtimeID()
	if err != nil {
		return node, err
	}
	node.AutomationID, _ = element.stringValue(29)
	node.ClassName, _ = element.stringValue(30)
	role, _ := element.intValue(21)
	node.ControlType = roles[role]
	if node.ControlType == "" {
		node.ControlType = strconv.Itoa(int(role))
	}
	pid, _ := element.intValue(20)
	node.ProcessID = int(pid)
	enabled, _ := element.intValue(28)
	node.Enabled = enabled != 0
	offscreen, _ := element.intValue(38)
	node.Offscreen = offscreen != 0
	focused, _ := element.intValue(26)
	node.Focused = focused != 0
	password, _ := element.intValue(35)
	node.Password = password != 0
	_ = element.call(43, uintptr(unsafe.Pointer(&node.Bounds)))
	for _, entry := range patterns {
		pattern, err := element.pattern(entry.id)
		if err != nil || pattern == nil {
			continue
		}
		node.Patterns = append(node.Patterns, entry.name)
		switch entry.name {
		case "value":
			if !node.Password {
				if value, err := pattern.stringValue(4); err == nil {
					node.Value = &value
				}
			}
		case "toggle":
			if value, err := pattern.intValue(4); err == nil {
				n := int(value)
				node.ToggleState = &n
			}
		case "selection_item":
			if value, err := pattern.intValue(6); err == nil {
				selected := value != 0
				node.Selected = &selected
			}
		}
		pattern.release()
	}
	return node, nil
}

type walkState struct {
	visited, skipped int
	truncated        bool
}

func (u *uia) walk(ctx context.Context, root *com, req Request, visit func(*com, Node) error) (walkState, error) {
	state := walkState{}
	var walk func(*com, int, string) error
	walk = func(element *com, depth int, parent string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if state.visited >= req.MaxNodes {
			state.truncated = true
			return nil
		}
		node, err := snapshot(element, depth, parent)
		state.visited++
		if err != nil {
			if depth == 0 {
				return err
			}
			state.skipped++
			return nil
		}
		if err = visit(element, node); err != nil {
			return err
		}
		child, err := u.related(element, 4)
		if err != nil {
			state.skipped++
			return nil
		}
		if depth >= req.MaxDepth {
			if child != nil {
				child.release()
				state.truncated = true
			}
			return nil
		}
		for child != nil {
			err = walk(child, depth+1, node.RuntimeID)
			if err != nil {
				child.release()
				return err
			}
			if state.visited >= req.MaxNodes {
				child.release()
				state.truncated = true
				return nil
			}
			next, nextErr := u.related(child, 6)
			child.release()
			if nextErr != nil {
				state.skipped++
				return nil
			}
			child = next
		}
		return nil
	}
	err := walk(root, 0, "")
	return state, err
}

func inspectNative(ctx context.Context, req Request) (map[string]any, error) {
	if req.Action == "windows" || req.Action == "applications" {
		items, err := enumerateWindows(ctx, req.ProcessID)
		if err != nil {
			return nil, err
		}
		if req.Action == "windows" {
			return map[string]any{"backend": "win32+uia", "windows": items}, nil
		}
		apps := []any{}
		byPID := map[uint32][]Window{}
		for _, w := range items {
			byPID[w.ProcessID] = append(byPID[w.ProcessID], w)
		}
		for pid, windows := range byPID {
			apps = append(apps, map[string]any{"process_id": pid, "application": windows[0].Application, "windows": windows})
		}
		return map[string]any{"backend": "win32+uia", "applications": apps}, nil
	}
	if req.Action != "tree" && req.Action != "find" {
		return nil, errors.New("desktop inspect action must be windows, applications, tree or find")
	}
	return withUIA(ctx, func(ctx context.Context, u *uia) (map[string]any, error) {
		root, err := u.element(req.WindowHandle)
		if err != nil {
			return nil, err
		}
		defer root.release()
		nodes := []Node{}
		state, err := u.walk(ctx, root, req, func(_ *com, node Node) error {
			if req.Action == "tree" || req.Selector.matches(node) {
				nodes = append(nodes, node)
			}
			return nil
		})
		return map[string]any{"backend": "windows-uia", "window_handle": req.WindowHandle, "elements": nodes, "visited": state.visited, "skipped_elements": state.skipped, "truncated": state.truncated}, err
	})
}
