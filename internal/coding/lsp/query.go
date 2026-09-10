// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

type Query struct {
	Action    string `json:"action"`
	Language  string `json:"language"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Character int    `json:"character"`
	Query     string `json:"query"`
	Direction string `json:"direction"`
	Limit     int    `json:"limit"`
}

func resolveFile(workspace, path string) (string, error) {
	if path == "" {
		return "", errors.New("path is required")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, path)
	}
	full, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(root, full)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("LSP file is outside this task workspace")
	}
	return full, nil
}
func readDocument(path string) (string, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !stat.Mode().IsRegular() || stat.Size() > 4<<20 {
		return "", errors.New("LSP expects a regular UTF-8 source file no larger than 4 MiB")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", errors.New("LSP source is not UTF-8")
	}
	return string(data), nil
}
func (c *client) syncDocuments(path string) error {
	paths := map[string]bool{}
	for opened := range c.documents {
		paths[opened] = true
	}
	if path != "" {
		paths[path] = true
	}
	for filename := range paths {
		text, err := readDocument(filename)
		previous, opened := c.documents[filename]
		uri := fileURI(filename)
		if errors.Is(err, os.ErrNotExist) && opened {
			_ = c.notify("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": uri}})
			delete(c.documents, filename)
			continue
		}
		if err != nil {
			return err
		}
		if opened && text == previous.Text {
			continue
		}
		current := document{Text: text, Version: previous.Version + 1}
		c.diagMu.Lock()
		current.DiagnosticAfter = c.sequence
		delete(c.diagnostics, uriKey(uri))
		c.diagMu.Unlock()
		if !opened {
			err = c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": documentLanguage(filename, c.spec.Language), "version": current.Version, "text": text}})
		} else {
			err = c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": current.Version}, "contentChanges": []any{map[string]any{"text": text}}})
			if err == nil {
				err = c.notify("textDocument/didSave", map[string]any{"textDocument": map[string]any{"uri": uri}, "text": text})
			}
		}
		if err != nil {
			return err
		}
		c.documents[filename] = current
	}
	return nil
}

func validatePosition(text string, line, character int) error {
	lines := strings.Split(text, "\n")
	if line < 1 || line > len(lines) || character < 0 {
		return errors.New("line is 1-based and character is a nonnegative UTF-16 offset")
	}
	value := strings.TrimSuffix(lines[line-1], "\r")
	offset := 0
	for _, r := range value {
		if offset == character {
			return nil
		}
		if r > 0xffff {
			offset += 2
		} else {
			offset++
		}
		if offset > character {
			return errors.New("character splits a UTF-16 surrogate pair")
		}
	}
	if character != len(utf16.Encode([]rune(value))) {
		return errors.New("character is beyond end of line")
	}
	return nil
}

func (m *Manager) Query(ctx context.Context, workspace string, q Query) (map[string]any, error) {
	if q.Limit == 0 {
		q.Limit = 100
	}
	if q.Limit < 1 || q.Limit > 500 {
		return nil, errors.New("limit must be between 1 and 500")
	}
	var path string
	var err error
	if q.Action != "workspace_symbols" {
		path, err = resolveFile(workspace, q.Path)
		if err != nil {
			return nil, err
		}
	}
	language := languageName(q.Language)
	if language == "" {
		language = languageFor(path)
	}
	c, err := m.get(ctx, workspace, language)
	if err != nil {
		return nil, err
	}
	c.opMu.Lock()
	defer c.opMu.Unlock()
	if err = c.syncDocuments(path); err != nil {
		return nil, err
	}
	if q.Action == "diagnostics" {
		return c.diagnosticQuery(ctx, path, q.Limit)
	}
	method, capability := "", ""
	params := map[string]any{}
	if path != "" {
		params["textDocument"] = map[string]any{"uri": fileURI(path)}
	}
	switch q.Action {
	case "definition":
		method, capability = "textDocument/definition", "definitionProvider"
	case "references":
		method, capability = "textDocument/references", "referencesProvider"
		params["context"] = map[string]any{"includeDeclaration": true}
	case "hover":
		method, capability = "textDocument/hover", "hoverProvider"
	case "symbols":
		method, capability = "textDocument/documentSymbol", "documentSymbolProvider"
	case "workspace_symbols":
		method, capability = "workspace/symbol", "workspaceSymbolProvider"
		params["query"] = q.Query
	case "call_hierarchy":
		method, capability = "textDocument/prepareCallHierarchy", "callHierarchyProvider"
	default:
		return nil, errors.New("LSP action must be definition, references, hover, symbols, workspace_symbols, diagnostics or call_hierarchy")
	}
	if value, ok := c.capabilities[capability]; !ok || value == false {
		return nil, fmt.Errorf("%s does not advertise %s", language, capability)
	}
	if q.Action != "symbols" && q.Action != "workspace_symbols" {
		if err = validatePosition(c.documents[path].Text, q.Line, q.Character); err != nil {
			return nil, err
		}
		params["position"] = map[string]int{"line": q.Line - 1, "character": q.Character}
	}
	data, err := c.call(ctx, method, params)
	if err != nil {
		return nil, err
	}
	if q.Action == "call_hierarchy" {
		var items []json.RawMessage
		if err = json.Unmarshal(data, &items); err != nil {
			return nil, err
		}
		direction := q.Direction
		if direction == "" {
			direction = "outgoing"
		}
		if direction != "incoming" && direction != "outgoing" {
			return nil, errors.New("direction must be incoming or outgoing")
		}
		entries := []any{}
		truncated := len(items) > q.Limit
		if truncated {
			items = items[:q.Limit]
		}
		for _, item := range items {
			calls, err := c.call(ctx, "callHierarchy/"+direction+"Calls", map[string]any{"item": item})
			if err != nil {
				return nil, err
			}
			limited, cut := limitResult(calls, q.Limit)
			entries = append(entries, map[string]any{"item": item, "calls": limited, "truncated": cut})
		}
		return map[string]any{"language": language, "workspace": workspace, "action": q.Action, "direction": direction, "data": entries, "truncated": truncated, "position_encoding": "utf-16; response lines/characters are zero-based"}, nil
	}
	limited, truncated := limitResult(data, q.Limit)
	return map[string]any{"language": language, "workspace": workspace, "action": q.Action, "method": method, "data": limited, "truncated": truncated, "position_encoding": "utf-16; response lines/characters are zero-based"}, nil
}

func limitResult(data json.RawMessage, limit int) (json.RawMessage, bool) {
	var items []json.RawMessage
	if len(data) > 0 && data[0] == '[' && json.Unmarshal(data, &items) == nil && len(items) > limit {
		limited, _ := json.Marshal(items[:limit])
		return limited, true
	}
	if len(data) == 0 {
		return json.RawMessage("null"), false
	}
	return data, false
}

func (c *client) diagnosticQuery(ctx context.Context, path string, limit int) (map[string]any, error) {
	uri := fileURI(path)
	doc := c.documents[path]
	result := map[string]any{"language": c.spec.Language, "workspace": c.workspace, "action": "diagnostics", "uri": uri, "document_version": doc.Version, "position_encoding": "utf-16; response lines/characters are zero-based"}
	if provider, ok := c.capabilities["diagnosticProvider"]; ok && provider != false {
		data, err := c.call(ctx, "textDocument/diagnostic", map[string]any{"textDocument": map[string]any{"uri": uri}})
		if err != nil {
			return nil, err
		}
		var response struct {
			Items json.RawMessage `json:"items"`
		}
		if err = json.Unmarshal(data, &response); err != nil {
			return nil, err
		}
		result["data"], result["truncated"] = limitResult(response.Items, limit)
		result["status"] = "received"
		result["source"] = "pull"
		return result, nil
	}
	wait, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	for {
		c.diagMu.Lock()
		value, ok := c.diagnostics[uriKey(uri)]
		changed := c.diagChanged
		c.diagMu.Unlock()
		if ok && value.Sequence > doc.DiagnosticAfter && (value.Version == nil || *value.Version == doc.Version) {
			result["data"], result["truncated"] = limitResult(value.Items, limit)
			result["status"] = "received"
			result["source"] = "push"
			result["server_version"] = value.Version
			if value.Version == nil {
				result["freshness_note"] = "Server did not include a document version; this is the latest publication after synchronization, not a version-certified clean result."
			}
			return result, nil
		}
		select {
		case <-changed:
		case <-c.done:
			return nil, errors.New("language server exited while waiting for diagnostics")
		case <-wait.Done():
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			result["status"] = "pending"
			result["data"] = nil
			result["truncated"] = false
			result["note"] = "No matching diagnostics publication yet; this does not mean the file is clean. Query again without restarting the server."
			return result, nil
		}
	}
}
