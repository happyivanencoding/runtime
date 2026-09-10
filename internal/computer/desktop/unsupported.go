//go:build !windows || !amd64

// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package desktop

import (
	"context"
	"errors"
)

var errUnsupported = errors.New("native Desktop Accessibility currently requires Windows amd64; no coordinate fallback is used")

func inspectNative(context.Context, Request) (map[string]any, error) { return nil, errUnsupported }
func actNative(context.Context, Request) (map[string]any, error)     { return nil, errUnsupported }
func clipboardNative(context.Context, string, string) (map[string]any, error) {
	return nil, errUnsupported
}
func screenNative(context.Context, uint64) ([]byte, Rect, error) { return nil, Rect{}, errUnsupported }
