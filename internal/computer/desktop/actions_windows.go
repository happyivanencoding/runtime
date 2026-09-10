//go:build windows && amd64

// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package desktop

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"unsafe"
)

func actNative(ctx context.Context, req Request) (map[string]any, error) {
	return withUIA(ctx, func(ctx context.Context, u *uia) (map[string]any, error) {
		root, err := u.element(req.WindowHandle)
		if err != nil {
			return nil, err
		}
		defer root.release()
		var target *com
		var before Node
		matches := 0
		defer func() {
			if target != nil {
				target.release()
			}
		}()
		state, err := u.walk(ctx, root, req, func(element *com, node Node) error {
			if req.Selector.matches(node) {
				matches++
				if target == nil {
					if err := element.call(1); err != nil {
						return err
					}
					target = element
					before = node
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		if matches != 1 {
			return nil, fmt.Errorf("selector matched %d elements; action not performed (truncated=%t)", matches, state.truncated)
		}
		if state.truncated && req.Selector.RuntimeID == "" {
			return nil, errors.New("tree was truncated; use a returned runtime_id to select one verified element")
		}
		if !before.Enabled {
			return nil, errors.New("target control is disabled")
		}
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		if req.Action == "focus" {
			err = target.call(3)
		} else {
			id := 0
			switch req.Action {
			case "press":
				id = 10000
			case "type":
				id = 10002
			case "scroll":
				id = 10004
			case "toggle":
				id = 10015
			case "select":
				id = 10010
			}
			pattern, e := target.pattern(id)
			if e != nil || pattern == nil {
				return nil, fmt.Errorf("target does not support %s through UI Automation; no coordinate/keyboard fallback was performed", req.Action)
			}
			defer pattern.release()
			switch req.Action {
			case "type":
				readOnly, e := pattern.intValue(5)
				if e != nil {
					return nil, e
				}
				if readOnly != 0 {
					return nil, errors.New("target value is read-only")
				}
				text, e := windows.UTF16PtrFromString(req.Text)
				if e != nil {
					return nil, e
				}
				err = pattern.call(3, uintptr(unsafe.Pointer(text)))
			case "scroll":
				amount := uintptr(4)
				if req.Amount == "large" {
					amount = 1
				} else if req.Amount != "" && req.Amount != "small" {
					return nil, errors.New("scroll amount must be small or large")
				}
				switch req.Direction {
				case "up", "left":
					if amount == 4 {
						amount = 3
					} else {
						amount = 0
					}
				case "down", "right":
				default:
					return nil, errors.New("scroll direction must be up, down, left or right")
				}
				horizontal, vertical := uintptr(2), uintptr(2)
				if req.Direction == "up" || req.Direction == "down" {
					vertical = amount
				} else {
					horizontal = amount
				}
				err = pattern.call(3, horizontal, vertical)
			default:
				err = pattern.call(3)
			}
		}
		if err != nil {
			return nil, err
		}
		after, observeErr := snapshot(target, before.Depth, before.ParentID)
		result := map[string]any{"backend": "windows-uia", "action": req.Action, "window_handle": req.WindowHandle, "performed": true, "before": before}
		if observeErr == nil {
			result["after"] = after
		} else {
			result["observation_note"] = "action returned success; target may have disappeared: " + observeErr.Error()
		}
		return result, nil
	})
}
