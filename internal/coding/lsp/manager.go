// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/uvwt/agentdock/internal/process"
)

type ServerSpec struct {
	Language              string            `json:"language"`
	Command               string            `json:"command"`
	Args                  []string          `json:"args,omitempty"`
	Env                   map[string]string `json:"env,omitempty"`
	Settings              map[string]any    `json:"settings,omitempty"`
	InitializationOptions map[string]any    `json:"initialization_options,omitempty"`
	Source                string            `json:"source"`
	Available             bool              `json:"available"`
}

type Manager struct {
	home    string
	mu      sync.Mutex
	clients map[string]*client
	closed  bool
}

func New(home string) *Manager { return &Manager{home: home, clients: map[string]*client{}} }

type document struct {
	Text            string
	Version         int
	DiagnosticAfter uint64
}
type diagnostic struct {
	Items    json.RawMessage
	Version  *int
	Sequence uint64
}
type client struct {
	workspace    string
	spec         ServerSpec
	cmd          *exec.Cmd
	control      *process.Controller
	stdin        io.WriteCloser
	done         chan struct{}
	processDone  chan struct{}
	logs         tailLog
	writeMu      sync.Mutex
	mu           sync.Mutex
	next         uint64
	pending      map[uint64]chan message
	opMu         sync.Mutex
	documents    map[string]document
	capabilities map[string]any
	diagMu       sync.Mutex
	diagnostics  map[string]diagnostic
	sequence     uint64
	diagChanged  chan struct{}
	closeOnce    sync.Once
}

func languageName(language string) string {
	switch strings.ToLower(language) {
	case "ts", "js", "javascript", "typescript", "tsx", "jsx":
		return "typescript"
	case "py", "python":
		return "python"
	case "go", "golang":
		return "go"
	case "rust", "rs":
		return "rust"
	default:
		return strings.ToLower(language)
	}
}
func languageFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		return "typescript"
	case ".py", ".pyi":
		return "python"
	case ".rs":
		return "rust"
	default:
		return ""
	}
}
func documentLanguage(path, language string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".tsx":
		return "typescriptreact"
	case ".jsx":
		return "javascriptreact"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	default:
		return language
	}
}

func (m *Manager) Discover(workspace string) ([]ServerSpec, error) {
	specs := map[string]ServerSpec{
		"go":         {Language: "go", Command: "gopls", Args: []string{"serve"}},
		"typescript": {Language: "typescript", Command: "typescript-language-server", Args: []string{"--stdio"}},
		"python":     {Language: "python", Command: "pyright-langserver", Args: []string{"--stdio"}, Settings: map[string]any{"python": map[string]any{"analysis": map[string]any{"diagnosticMode": "openFilesOnly"}}}},
		"rust":       {Language: "rust", Command: "rust-analyzer"},
	}
	// Machine-local configuration is small, explicit and never a parallel project registry.
	data, err := os.ReadFile(filepath.Join(m.home, "lsp-servers.json"))
	if err == nil {
		var overrides map[string]ServerSpec
		if err = json.Unmarshal(data, &overrides); err != nil {
			return nil, fmt.Errorf("lsp-servers.json: %w", err)
		}
		for language, spec := range overrides {
			language = languageName(language)
			spec.Language = language
			spec.Source = "configured"
			specs[language] = spec
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	result := []ServerSpec{}
	for language, spec := range specs {
		if spec.Source != "configured" {
			if language == "go" {
				name := "gopls"
				if runtime.GOOS == "windows" {
					name += ".exe"
				}
				candidate := filepath.Join(m.home, "tools", "go", name)
				if regularFile(candidate) {
					spec.Command = candidate
					spec.Source = "runtime-tools"
				}
			}
			if language == "typescript" || language == "python" {
				pkg, entry := "typescript-language-server", "lib/cli.mjs"
				if language == "python" {
					pkg, entry = "pyright", "langserver.index.js"
				}
				for _, base := range []string{workspace, filepath.Join(m.home, "tools", "node")} {
					candidate := filepath.Join(base, "node_modules", pkg, entry)
					if regularFile(candidate) {
						if node, err := exec.LookPath("node"); err == nil {
							spec.Command = node
							spec.Args = []string{candidate, "--stdio"}
							spec.Source = "node-package"
							break
						}
					}
				}
			}
		}
		if path, err := exec.LookPath(spec.Command); err == nil {
			spec.Command = path
			spec.Available = true
		}
		if spec.Source == "" {
			spec.Source = "PATH"
		}
		result = append(result, spec)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Language < result[j].Language })
	return result, nil
}
func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func clientKey(workspace, language string) string {
	root := filepath.Clean(workspace)
	if runtime.GOOS == "windows" {
		root = strings.ToLower(root)
	}
	return root + "\x00" + language
}
func (m *Manager) get(ctx context.Context, workspace, language string) (*client, error) {
	language = languageName(language)
	if language == "" {
		return nil, errors.New("language is required or must be inferable from path")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errors.New("LSP manager is closed")
	}
	key := clientKey(workspace, language)
	if old := m.clients[key]; old != nil {
		select {
		case <-old.done:
			old.close()
			delete(m.clients, key)
		default:
			return old, nil
		}
	}
	specs, err := m.Discover(workspace)
	if err != nil {
		return nil, err
	}
	for _, spec := range specs {
		if spec.Language != language {
			continue
		}
		if !spec.Available {
			return nil, fmt.Errorf("language server for %s is not installed; inspect lsp_manage discover or configure %s", language, filepath.Join(m.home, "lsp-servers.json"))
		}
		c, err := startClient(ctx, workspace, spec)
		if err != nil {
			return nil, err
		}
		m.clients[key] = c
		return c, nil
	}
	return nil, fmt.Errorf("language %q has no configured server", language)
}

func (m *Manager) Manage(ctx context.Context, workspace, language, action string) (map[string]any, error) {
	language = languageName(language)
	switch action {
	case "discover":
		specs, err := m.Discover(workspace)
		return map[string]any{"workspace": workspace, "servers": specs, "configuration": filepath.Join(m.home, "lsp-servers.json")}, err
	case "start":
		c, err := m.get(ctx, workspace, language)
		if err != nil {
			return nil, err
		}
		return c.info(), nil
	case "status":
		m.mu.Lock()
		defer m.mu.Unlock()
		servers := []any{}
		for _, c := range m.clients {
			if clientKey(c.workspace, "") == clientKey(workspace, "") && (language == "" || c.spec.Language == language) {
				servers = append(servers, c.info())
			}
		}
		return map[string]any{"workspace": workspace, "servers": servers}, nil
	case "stop":
		if language == "" {
			return nil, errors.New("language is required to stop one server")
		}
		m.mu.Lock()
		c := m.clients[clientKey(workspace, language)]
		delete(m.clients, clientKey(workspace, language))
		m.mu.Unlock()
		if c != nil {
			c.close()
		}
		return map[string]any{"workspace": workspace, "language": language, "stopped": c != nil}, nil
	default:
		return nil, errors.New("LSP manage action must be discover, start, status or stop")
	}
}
func (c *client) info() map[string]any {
	status := "running"
	select {
	case <-c.done:
		status = "exited"
	default:
	}
	return map[string]any{"workspace": c.workspace, "language": c.spec.Language, "pid": c.cmd.Process.Pid, "status": status, "command": c.spec.Command, "args": c.spec.Args, "capabilities": c.capabilities, "stderr_tail": c.logs.String()}
}

func startClient(ctx context.Context, workspace string, spec ServerSpec) (*client, error) {
	if strings.EqualFold(filepath.Ext(spec.Command), ".cmd") || strings.EqualFold(filepath.Ext(spec.Command), ".bat") {
		return nil, errors.New("configure the actual node executable and language-server JS entrypoint, not a .cmd/.bat launcher")
	}
	c := &client{workspace: workspace, spec: spec, done: make(chan struct{}), processDone: make(chan struct{}), pending: map[uint64]chan message{}, documents: map[string]document{}, diagnostics: map[string]diagnostic{}, diagChanged: make(chan struct{})}
	cmd := exec.Command(spec.Command, spec.Args...)
	c.cmd = cmd
	cmd.Dir = workspace
	env := os.Environ()
	for key, value := range spec.Env {
		for i := len(env) - 1; i >= 0; i-- {
			name, _, _ := strings.Cut(env[i], "=")
			if strings.EqualFold(name, key) {
				env = append(env[:i], env[i+1:]...)
			}
		}
		env = append(env, key+"="+os.ExpandEnv(value))
	}
	cmd.Env = env
	cmd.Stderr = &c.logs
	var err error
	c.stdin, err = cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = c.stdin.Close()
		return nil, err
	}
	process.Configure(cmd)
	if err = cmd.Start(); err != nil {
		_ = c.stdin.Close()
		_ = stdout.Close()
		return nil, err
	}
	c.control, err = process.Attach(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	go c.readLoop(stdout)
	go func() { _ = cmd.Wait(); close(c.processDone) }()
	init := map[string]any{
		"processId": os.Getpid(), "clientInfo": map[string]any{"name": "runtime-core", "version": "0.2.0"},
		"rootUri": fileURI(workspace), "rootPath": workspace, "workspaceFolders": []any{map[string]any{"uri": fileURI(workspace), "name": filepath.Base(workspace)}},
		"capabilities": map[string]any{
			"general":   map[string]any{"positionEncodings": []string{"utf-16"}},
			"workspace": map[string]any{"configuration": true, "workspaceFolders": true},
			"textDocument": map[string]any{
				"synchronization": map[string]any{"didSave": true}, "publishDiagnostics": map[string]any{"versionSupport": true},
				"hover":          map[string]any{"contentFormat": []string{"markdown", "plaintext"}},
				"documentSymbol": map[string]any{"hierarchicalDocumentSymbolSupport": true}, "callHierarchy": map[string]any{},
			},
		}, "initializationOptions": spec.InitializationOptions,
	}
	result, err := c.call(ctx, "initialize", init)
	if err != nil {
		c.close()
		return nil, fmt.Errorf("initialize %s: %w; %s", spec.Language, err, c.logs.String())
	}
	var response struct {
		Capabilities map[string]any `json:"capabilities"`
	}
	if err = json.Unmarshal(result, &response); err != nil {
		c.close()
		return nil, err
	}
	c.capabilities = response.Capabilities
	if encoding, ok := c.capabilities["positionEncoding"].(string); ok && encoding != "utf-16" {
		c.close()
		return nil, fmt.Errorf("server chose unsupported position encoding %s", encoding)
	}
	if err = c.notify("initialized", map[string]any{}); err != nil {
		c.close()
		return nil, err
	}
	if len(spec.Settings) > 0 {
		_ = c.notify("workspace/didChangeConfiguration", map[string]any{"settings": spec.Settings})
	}
	return c, nil
}
func (c *client) close() {
	c.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		select {
		case <-c.done:
		default:
			_, _ = c.call(ctx, "shutdown", nil)
			_ = c.notify("exit", nil)
		}
		_ = c.stdin.Close()
		select {
		case <-c.processDone:
		case <-time.After(time.Second):
			_ = c.control.Terminate()
			<-c.processDone
		}
		_ = c.control.Close()
	})
}
func (m *Manager) Close() error {
	m.mu.Lock()
	m.closed = true
	clients := m.clients
	m.clients = map[string]*client{}
	m.mu.Unlock()
	for _, c := range clients {
		c.close()
	}
	return nil
}

func fileURI(path string) string {
	path = filepath.ToSlash(path)
	if strings.HasPrefix(path, "//") {
		parts := strings.SplitN(path[2:], "/", 2)
		u := url.URL{Scheme: "file", Host: parts[0]}
		if len(parts) > 1 {
			u.Path = "/" + parts[1]
		}
		return u.String()
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func uriKey(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if runtime.GOOS == "windows" {
		u.Path = strings.ToLower(u.Path)
		u.Host = strings.ToLower(u.Host)
	}
	return u.String()
}
