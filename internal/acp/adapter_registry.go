// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package acp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// AdapterInfo describes one ACP transport adapter without starting it.
type AdapterInfo struct {
	Agent     string   `json:"agent"`
	Available bool     `json:"available"`
	Command   string   `json:"command,omitempty"`
	Args      []string `json:"args,omitempty"`
	Source    string   `json:"source,omitempty"`
}

// AdapterRegistry is the Runtime-facing lazy ACP adapter layer. Constructing it
// never starts an ACP process; a process is created only by Start/Resume flows.
type AdapterRegistry struct {
	home               string
	defaultCWD         string
	maxConcurrentRuns  int
	interactionTimeout time.Duration

	mu       sync.Mutex
	managers map[string]*Manager
	specs    map[string]AgentSpec
	closed   bool
}

func NewAdapterRegistry(home, defaultCWD string, maxConcurrentRuns int, interactionTimeout time.Duration) *AdapterRegistry {
	if maxConcurrentRuns <= 0 {
		maxConcurrentRuns = 2
	}
	if interactionTimeout <= 0 {
		interactionTimeout = 5 * time.Minute
	}
	return &AdapterRegistry{
		home: home, defaultCWD: defaultCWD,
		maxConcurrentRuns: maxConcurrentRuns, interactionTimeout: interactionTimeout,
		managers: make(map[string]*Manager), specs: make(map[string]AgentSpec),
	}
}

func normalizeAdapterAgent(agent string) string {
	agent = strings.ToLower(strings.TrimSpace(agent))
	if agent == "" {
		return "codex"
	}
	return agent
}

func (r *AdapterRegistry) Manager(agent, command string, args []string, envFromEnv map[string]string) (*Manager, AdapterInfo, error) {
	agent = normalizeAdapterAgent(agent)
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, AdapterInfo{}, newError("ACP_MANAGER_CLOSED", "ACP adapter registry is closed", false, nil, nil)
	}
	if existing := r.managers[agent]; existing != nil {
		info := adapterInfoFromSpec(r.specs[agent], "active")
		r.mu.Unlock()
		return existing, info, nil
	}
	r.mu.Unlock()

	info, spec, err := r.resolve(agent, command, args, envFromEnv)
	if err != nil {
		return nil, info, err
	}
	return r.ManagerWithSpec(spec, info.Source)
}

// ManagerWithSpec is used by the legacy configured ACP surface so both the
// legacy tools and Runtime adapter tools share one manager for the same agent.
func (r *AdapterRegistry) ManagerWithSpec(spec AgentSpec, source string) (*Manager, AdapterInfo, error) {
	spec.Name = normalizeAdapterAgent(spec.Name)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, AdapterInfo{}, newError("ACP_MANAGER_CLOSED", "ACP adapter registry is closed", false, nil, nil)
	}
	if existing := r.managers[spec.Name]; existing != nil {
		return existing, adapterInfoFromSpec(r.specs[spec.Name], "active"), nil
	}
	manager, err := NewManager(Options{
		Home: r.home, DefaultCWD: r.defaultCWD, Agent: spec,
		MaxConcurrentRuns: r.maxConcurrentRuns, InteractionTimeout: r.interactionTimeout,
	})
	if err != nil {
		return nil, AdapterInfo{}, err
	}
	r.managers[spec.Name] = manager
	r.specs[spec.Name] = spec
	if strings.TrimSpace(source) == "" {
		source = "configured"
	}
	return manager, adapterInfoFromSpec(spec, source), nil
}

func (r *AdapterRegistry) ExistingManager(agent string) *Manager {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.managers[normalizeAdapterAgent(agent)]
}

func (r *AdapterRegistry) ManagerForSession(sessionID, command string, args []string, envFromEnv map[string]string) (*Manager, AdapterInfo, SessionRecord, error) {
	record, err := r.FindSession(sessionID)
	if err != nil {
		return nil, AdapterInfo{}, SessionRecord{}, err
	}
	manager, info, err := r.Manager(record.Agent, command, args, envFromEnv)
	return manager, info, record, err
}

func (r *AdapterRegistry) FindSession(sessionID string) (SessionRecord, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || filepath.Base(sessionID) != sessionID {
		return SessionRecord{}, newError("ACP_SESSION_NOT_FOUND", "ACP session was not found", false, map[string]any{"session_id": sessionID}, nil)
	}
	root := filepath.Join(r.home, "acp", "sessions")
	directories, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SessionRecord{}, newError("ACP_SESSION_NOT_FOUND", "ACP session was not found", false, map[string]any{"session_id": sessionID}, err)
		}
		return SessionRecord{}, fmt.Errorf("list ACP adapter session roots: %w", err)
	}
	for _, directory := range directories {
		if !directory.IsDir() {
			continue
		}
		path := filepath.Join(root, directory.Name(), sessionID+".json")
		record, found, err := readPersistedAdapterSession(path, sessionID)
		if err != nil {
			return SessionRecord{}, err
		}
		if found {
			return record, nil
		}
	}
	return SessionRecord{}, newError("ACP_SESSION_NOT_FOUND", "ACP session was not found", false, map[string]any{"session_id": sessionID}, nil)
}

func (r *AdapterRegistry) ListSessions() ([]SessionRecord, error) {
	root := filepath.Join(r.home, "acp", "sessions")
	directories, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []SessionRecord{}, nil
		}
		return nil, fmt.Errorf("list ACP adapter session roots: %w", err)
	}
	sessions := []SessionRecord{}
	for _, directory := range directories {
		if !directory.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, directory.Name()))
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
				continue
			}
			id := strings.TrimSuffix(file.Name(), ".json")
			record, found, err := readPersistedAdapterSession(filepath.Join(root, directory.Name(), file.Name()), id)
			if err != nil {
				return nil, err
			}
			if found {
				sessions = append(sessions, record)
			}
		}
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt) })
	return sessions, nil
}

func readPersistedAdapterSession(path, expectedID string) (SessionRecord, bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SessionRecord{}, false, nil
		}
		return SessionRecord{}, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return SessionRecord{}, false, fmt.Errorf("invalid ACP session state file: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return SessionRecord{}, false, err
	}
	var record SessionRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return SessionRecord{}, false, fmt.Errorf("decode ACP adapter session: %w", err)
	}
	if err := validateSessionRecord(expectedID, record); err != nil {
		return SessionRecord{}, false, err
	}
	return record, true, nil
}

func (r *AdapterRegistry) AdapterStatuses() []AdapterInfo {
	agents := []string{"codex", "claude", "grok"}
	result := make([]AdapterInfo, 0, len(agents))
	for _, agent := range agents {
		r.mu.Lock()
		active := r.managers[agent]
		spec := r.specs[agent]
		r.mu.Unlock()
		if active != nil {
			result = append(result, adapterInfoFromSpec(spec, "active"))
			continue
		}
		info, _, err := r.resolve(agent, "", nil, nil)
		if err != nil {
			result = append(result, AdapterInfo{Agent: agent, Available: false})
			continue
		}
		result = append(result, info)
	}
	return result
}

func (r *AdapterRegistry) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	managers := make([]*Manager, 0, len(r.managers))
	for _, manager := range r.managers {
		managers = append(managers, manager)
	}
	r.managers = map[string]*Manager{}
	r.mu.Unlock()
	var errs []error
	for _, manager := range managers {
		if err := manager.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func adapterInfoFromSpec(spec AgentSpec, source string) AdapterInfo {
	return AdapterInfo{Agent: spec.Name, Available: true, Command: spec.Command, Args: append([]string(nil), spec.Args...), Source: source}
}

func (r *AdapterRegistry) resolve(agent, configuredCommand string, configuredArgs []string, envFromEnv map[string]string) (AdapterInfo, AgentSpec, error) {
	environment := map[string]string{}
	for childName, hostName := range envFromEnv {
		childName, hostName = strings.TrimSpace(childName), strings.TrimSpace(hostName)
		if childName == "" || hostName == "" {
			return AdapterInfo{Agent: agent}, AgentSpec{}, newError("ACP_ADAPTER_INVALID", "ACP environment mapping names must not be empty", false, nil, nil)
		}
		value, ok := os.LookupEnv(hostName)
		if !ok {
			return AdapterInfo{Agent: agent}, AgentSpec{}, newError("ACP_ADAPTER_ENV_MISSING", "required ACP host environment variable is missing", false, map[string]any{"environment": hostName}, nil)
		}
		environment[childName] = value
	}
	if strings.TrimSpace(configuredCommand) != "" {
		command, err := validateAdapterExecutable(configuredCommand)
		if err != nil {
			return AdapterInfo{Agent: agent}, AgentSpec{}, err
		}
		spec := AgentSpec{Name: agent, Command: command, Args: append([]string(nil), configuredArgs...), Environment: environment}
		return adapterInfoFromSpec(spec, "explicit"), spec, nil
	}

	names, presetArgs, npmPackage, npmBin := adapterPreset(agent)
	if len(names) == 0 {
		return AdapterInfo{Agent: agent}, AgentSpec{}, newError("ACP_ADAPTER_NOT_CONFIGURED", "custom ACP agent requires an explicit absolute command", false, map[string]any{"agent": agent}, nil)
	}
	for _, name := range names {
		if candidate, err := exec.LookPath(name); err == nil {
			if command, err := validateAdapterExecutable(candidate); err == nil {
				spec := AgentSpec{Name: agent, Command: command, Args: append([]string(nil), presetArgs...), Environment: environment}
				return adapterInfoFromSpec(spec, "path"), spec, nil
			}
		}
	}
	directories := adapterSearchDirectories(r.home)
	for _, directory := range directories {
		for _, name := range names {
			if command, err := validateAdapterExecutable(filepath.Join(directory, name)); err == nil {
				spec := AgentSpec{Name: agent, Command: command, Args: append([]string(nil), presetArgs...), Environment: environment}
				return adapterInfoFromSpec(spec, "filesystem"), spec, nil
			}
		}
	}
	if npmPackage != "" {
		if command, args, ok := resolveNPMAdapter(npmPackage, npmBin, presetArgs, directories); ok {
			spec := AgentSpec{Name: agent, Command: command, Args: args, Environment: environment}
			return adapterInfoFromSpec(spec, "npm"), spec, nil
		}
	}
	return AdapterInfo{Agent: agent}, AgentSpec{}, newError("ACP_ADAPTER_NOT_FOUND", "ACP adapter executable was not found", false, map[string]any{"agent": agent}, nil)
}

func adapterPreset(agent string) (names, args []string, npmPackage, npmBin string) {
	switch agent {
	case "codex":
		return platformAdapterNames("codex-acp"), nil, "@agentclientprotocol/codex-acp", "codex-acp"
	case "claude":
		return platformAdapterNames("claude-agent-acp"), nil, "@agentclientprotocol/claude-agent-acp", "claude-agent-acp"
	case "grok":
		return platformAdapterNames("grok"), []string{"agent", "stdio"}, "", ""
	default:
		return nil, nil, "", ""
	}
}

func platformAdapterNames(base string) []string {
	if runtime.GOOS == "windows" {
		return []string{base + ".exe", base + ".com"}
	}
	return []string{base}
}

func validateAdapterExecutable(candidate string) (string, error) {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return "", errors.New("ACP adapter command is empty")
	}
	absolute, err := filepath.Abs(candidate)
	if err != nil || !filepath.IsAbs(absolute) {
		return "", newError("ACP_ADAPTER_INVALID", "ACP adapter command must be an absolute executable path", false, map[string]any{"command": candidate}, err)
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		return "", newError("ACP_ADAPTER_NOT_FOUND", "ACP adapter executable was not found", false, map[string]any{"command": absolute}, err)
	}
	if runtime.GOOS == "windows" {
		ext := strings.ToLower(filepath.Ext(absolute))
		if ext != ".exe" && ext != ".com" {
			return "", newError("ACP_ADAPTER_INVALID", "Windows ACP adapter must be a directly executable .exe or .com file", false, map[string]any{"command": absolute}, nil)
		}
	} else if info.Mode()&0o111 == 0 {
		return "", newError("ACP_ADAPTER_INVALID", "ACP adapter file is not executable", false, map[string]any{"command": absolute}, nil)
	}
	return filepath.Clean(absolute), nil
}

func adapterSearchDirectories(home string) []string {
	result := []string{}
	if userHome, err := os.UserHomeDir(); err == nil {
		result = append(result, filepath.Join(userHome, ".local", "bin"), filepath.Join(userHome, ".local", "lib", "node_modules"))
	}
	if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
		result = append(result, appData, filepath.Join(appData, "npm"), filepath.Join(appData, "npm", "node_modules"))
	}
	if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
		result = append(result, filepath.Join(localAppData, "npm"), filepath.Join(localAppData, "npm", "node_modules"), filepath.Join(localAppData, "Microsoft", "WinGet", "Links"), filepath.Join(localAppData, "Programs", "Grok"))
	}
	result = append(result, filepath.Join(home, "bin"), "/usr/local/bin", "/usr/local/lib/node_modules", "/usr/bin", "/usr/lib/node_modules")
	result = append(result, filepath.SplitList(os.Getenv("PATH"))...)
	seen := map[string]struct{}{}
	unique := []string{}
	for _, directory := range result {
		directory = strings.TrimSpace(directory)
		if directory == "" {
			continue
		}
		absolute, err := filepath.Abs(directory)
		if err != nil {
			continue
		}
		key := strings.ToLower(filepath.Clean(absolute))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, filepath.Clean(absolute))
	}
	return unique
}

func resolveNPMAdapter(packageName, binName string, presetArgs, directories []string) (string, []string, bool) {
	node, err := exec.LookPath("node")
	if err != nil {
		return "", nil, false
	}
	node, err = validateAdapterExecutable(node)
	if err != nil {
		return "", nil, false
	}
	packageParts := strings.Split(strings.Trim(packageName, "/"), "/")
	roots := append([]string{}, directories...)
	for _, directory := range directories {
		roots = append(roots, filepath.Join(directory, "node_modules"))
	}
	seen := map[string]struct{}{}
	for _, root := range roots {
		packageRoot := root
		for _, part := range packageParts {
			packageRoot = filepath.Join(packageRoot, part)
		}
		packageRoot = filepath.Clean(packageRoot)
		key := strings.ToLower(packageRoot)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		entry, ok := readNPMBin(packageRoot, binName)
		if !ok {
			continue
		}
		args := []string{entry}
		args = append(args, presetArgs...)
		return node, args, true
	}
	return "", nil, false
}

func readNPMBin(packageRoot, binName string) (string, bool) {
	manifest, err := os.ReadFile(filepath.Join(packageRoot, "package.json"))
	if err != nil {
		return "", false
	}
	var raw struct {
		Bin json.RawMessage `json:"bin"`
	}
	if json.Unmarshal(manifest, &raw) != nil || len(raw.Bin) == 0 {
		return "", false
	}
	var entry string
	if json.Unmarshal(raw.Bin, &entry) != nil {
		var bins map[string]string
		if json.Unmarshal(raw.Bin, &bins) != nil {
			return "", false
		}
		entry = bins[binName]
	}
	entry = strings.TrimSpace(entry)
	if entry == "" || filepath.IsAbs(entry) {
		return "", false
	}
	root, err := filepath.Abs(packageRoot)
	if err != nil {
		return "", false
	}
	candidate, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(entry)))
	if err != nil {
		return "", false
	}
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	info, err := os.Stat(candidate)
	return candidate, err == nil && info.Mode().IsRegular()
}
