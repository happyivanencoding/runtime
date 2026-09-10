// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package browserbridge

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"
)

type Client struct{ Home string }

type callRequest struct {
	Token  string          `json:"token,omitempty"`
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type CallResponse struct {
	ID     string          `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

func (c Client) Call(ctx context.Context, method string, params any, result any) error {
	state, err := ReadState(c.Home)
	if err != nil {
		return fmt.Errorf("Runtime Chrome extension bridge is not connected: %w", err)
	}
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", state.Address)
	if err != nil {
		return fmt.Errorf("connect Runtime Chrome extension bridge: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	id := bridgeRequestID()
	var raw json.RawMessage
	if params != nil {
		raw, err = json.Marshal(params)
		if err != nil {
			return err
		}
	}
	request := callRequest{Token: state.Token, ID: id, Method: strings.TrimSpace(method), Params: raw}
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		return err
	}
	var response CallResponse
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&response); err != nil {
		return err
	}
	if response.ID != id {
		return fmt.Errorf("Runtime Chrome extension bridge response id mismatch")
	}
	if !response.OK {
		if response.Error == "" {
			response.Error = "Chrome extension bridge request failed"
		}
		return fmt.Errorf("%s", response.Error)
	}
	if result != nil && len(response.Result) > 0 {
		if err := json.Unmarshal(response.Result, result); err != nil {
			return fmt.Errorf("decode Chrome bridge result: %w", err)
		}
	}
	return nil
}

func (c Client) Status(ctx context.Context) (map[string]any, error) {
	var result map[string]any
	if err := c.Call(ctx, "bridge.status", map[string]any{}, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func bridgeRequestID() string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return "br-" + hex.EncodeToString(raw[:])
	}
	return fmt.Sprintf("br-%d", time.Now().UnixNano())
}
