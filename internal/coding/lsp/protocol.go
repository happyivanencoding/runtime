// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

const maxMessage = 16 << 20

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("LSP %d: %s", e.Code, e.Message) }

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func readMessage(reader *bufio.Reader) (message, error) {
	length := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return message{}, err
		}
		if len(line) > 8192 {
			return message{}, errors.New("invalid LSP header")
		}
		if line == "\r\n" || line == "\n" {
			break
		}
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && strings.EqualFold(key, "Content-Length") {
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return message{}, err
			}
		}
	}
	if length < 0 || length > maxMessage {
		return message{}, fmt.Errorf("invalid LSP Content-Length %d", length)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(reader, body); err != nil {
		return message{}, err
	}
	var msg message
	err := json.Unmarshal(body, &msg)
	return msg, err
}

func writeMessage(w io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) > maxMessage {
		return errors.New("LSP message exceeds 16 MiB")
	}
	_, err = fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(data))
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

type tailLog struct {
	mu   sync.Mutex
	data []byte
}

func (l *tailLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := len(p)
	l.data = append(l.data, p...)
	if len(l.data) > 16384 {
		l.data = append([]byte(nil), l.data[len(l.data)-16384:]...)
	}
	return n, nil
}
func (l *tailLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.ToValidUTF8(string(l.data), "")
}

func (c *client) send(value any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return writeMessage(c.stdin, value)
}
func (c *client) notify(method string, params any) error {
	return c.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.next++
	id := c.next
	response := make(chan message, 1)
	c.pending[id] = response
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	if err := c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	select {
	case result := <-response:
		if result.Error != nil {
			return nil, result.Error
		}
		return result.Result, nil
	case <-c.done:
		return nil, fmt.Errorf("language server stopped: %s", c.logs.String())
	case <-ctx.Done():
		_ = c.notify("$/cancelRequest", map[string]any{"id": id})
		return nil, ctx.Err()
	}
}

func (c *client) readLoop(stdout io.Reader) {
	defer close(c.done)
	reader := bufio.NewReader(stdout)
	for {
		msg, err := readMessage(reader)
		if err != nil {
			c.logs.Write([]byte("\nLSP transport: " + err.Error()))
			return
		}
		if msg.Method != "" {
			if len(msg.ID) > 0 {
				c.answer(msg)
			} else {
				c.notification(msg)
			}
			continue
		}
		var id uint64
		if json.Unmarshal(msg.ID, &id) != nil {
			continue
		}
		c.mu.Lock()
		ch := c.pending[id]
		c.mu.Unlock()
		if ch != nil {
			select {
			case ch <- msg:
			default:
			}
		}
	}
}

func (c *client) answer(msg message) {
	var result any
	var rpcErr *rpcError
	switch msg.Method {
	case "workspace/configuration":
		var req struct {
			Items []struct {
				Section string `json:"section"`
			} `json:"items"`
		}
		_ = json.Unmarshal(msg.Params, &req)
		values := make([]any, len(req.Items))
		for i, item := range req.Items {
			var value any = c.spec.Settings
			for _, part := range strings.Split(item.Section, ".") {
				if part == "" {
					continue
				}
				m, ok := value.(map[string]any)
				if !ok {
					value = nil
					break
				}
				value = m[part]
			}
			values[i] = value
		}
		result = values
	case "workspace/workspaceFolders":
		result = []any{map[string]any{"uri": fileURI(c.workspace), "name": c.workspace}}
	case "window/workDoneProgress/create", "client/registerCapability", "client/unregisterCapability", "workspace/diagnostic/refresh":
		result = nil
	case "workspace/applyEdit":
		result = map[string]any{"applied": false, "failureReason": "Runtime LSP is read/navigation only; use explicit file editing tools."}
	case "window/showMessageRequest":
		result = nil
	default:
		rpcErr = &rpcError{Code: -32601, Message: "client method not supported: " + msg.Method}
	}
	reply := map[string]any{"jsonrpc": "2.0", "id": msg.ID}
	if rpcErr != nil {
		reply["error"] = rpcErr
	} else {
		reply["result"] = result
	}
	_ = c.send(reply)
}

func (c *client) notification(msg message) {
	if msg.Method != "textDocument/publishDiagnostics" {
		return
	}
	var value struct {
		URI         string          `json:"uri"`
		Version     *int            `json:"version"`
		Diagnostics json.RawMessage `json:"diagnostics"`
	}
	if json.Unmarshal(msg.Params, &value) != nil {
		return
	}
	c.diagMu.Lock()
	c.sequence++
	c.diagnostics[uriKey(value.URI)] = diagnostic{Items: value.Diagnostics, Version: value.Version, Sequence: c.sequence}
	close(c.diagChanged)
	c.diagChanged = make(chan struct{})
	c.diagMu.Unlock()
}
