// Copyright 2026 Jingxuan Li and runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package coding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/fs/atomicfile"
	"github.com/uvwt/agentdock/internal/fs/filelock"
)

type Device struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	Kind string `json:"kind,omitempty"`
}

type Project struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	RepoPath         string            `json:"repo_path"`
	Remote           string            `json:"remote,omitempty"`
	MachineID        string            `json:"machine_id"`
	Stack            []string          `json:"stack,omitempty"`
	Instructions     string            `json:"instructions,omitempty"`
	InstructionFiles []string          `json:"instruction_files,omitempty"`
	Capabilities     []string          `json:"capabilities,omitempty"`
	DefaultBranch    string            `json:"default_branch,omitempty"`
	Devices          []Device          `json:"devices,omitempty"`
	Deployment       map[string]string `json:"deployment,omitempty"`
}

type Machine struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

type Registry struct {
	root    string
	Machine Machine
}

var projectIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

func NewRegistry(home string) (*Registry, error) {
	host, err := os.Hostname()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(home, "projects")
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	return &Registry{root: root, Machine: Machine{ID: strings.ToLower(host), Name: host, OS: runtime.GOOS, Arch: runtime.GOARCH}}, nil
}

func (r *Registry) Get(id string) (Project, error) {
	if !projectIDPattern.MatchString(id) {
		return Project{}, errors.New("invalid project id")
	}
	data, err := os.ReadFile(filepath.Join(r.root, id+".json"))
	if err != nil {
		return Project{}, fmt.Errorf("read project %s: %w", id, err)
	}
	var p Project
	if err := json.Unmarshal(data, &p); err != nil {
		return p, err
	}
	if p.ID != id {
		return p, errors.New("project record id mismatch")
	}
	return p, nil
}

func (r *Registry) List() ([]Project, error) {
	paths, err := filepath.Glob(filepath.Join(r.root, "*.json"))
	if err != nil {
		return nil, err
	}
	projects := make([]Project, 0, len(paths))
	for _, path := range paths {
		p, err := r.Get(strings.TrimSuffix(filepath.Base(path), ".json"))
		if err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].ID < projects[j].ID })
	return projects, nil
}

func (r *Registry) Register(ctx context.Context, p Project) (Project, error) {
	p.ID = strings.ToLower(strings.TrimSpace(p.ID))
	if !projectIDPattern.MatchString(p.ID) {
		return p, errors.New("project id must use letters, digits, dot, dash or underscore")
	}
	if !filepath.IsAbs(p.RepoPath) {
		return p, errors.New("repo_path must be absolute")
	}
	root, err := gitText(ctx, p.RepoPath, "rev-parse", "--show-toplevel")
	if err != nil {
		return p, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return p, err
	}
	p.RepoPath = root
	if p.Name == "" {
		p.Name = p.ID
	}
	if p.MachineID == "" {
		p.MachineID = r.Machine.ID
	}
	// Phase one registers repositories on this machine, not inaccessible remote paths.
	if p.MachineID != r.Machine.ID {
		return p, errors.New("remote project registration requires a connected runner (not implemented)")
	}
	if p.Remote == "" {
		p.Remote, _ = gitText(ctx, root, "remote", "get-url", "origin")
	}
	if p.DefaultBranch == "" {
		branch, _ := gitText(ctx, root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
		p.DefaultBranch = strings.TrimPrefix(branch, "origin/")
		if p.DefaultBranch == "" {
			p.DefaultBranch, _ = gitText(ctx, root, "symbolic-ref", "--short", "HEAD")
		}
	}
	if len(p.Stack) == 0 {
		p.Stack = detectStack(root)
	}
	if len(p.InstructionFiles) == 0 {
		p.InstructionFiles = []string{"AGENTS.md", "CLAUDE.md"}
	}
	lockCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	release, err := filelock.Acquire(lockCtx, filepath.Join(r.root, ".registry.lock"))
	if err != nil {
		return p, err
	}
	defer release()
	existing, err := r.Get(p.ID)
	if err == nil && (!samePath(existing.RepoPath, p.RepoPath) || existing.MachineID != p.MachineID) {
		return p, errors.New("project id already points at another repository; it was not overwritten")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return p, err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return p, err
	}
	return p, atomicfile.Write(filepath.Join(r.root, p.ID+".json"), append(data, '\n'), 0600)
}

func detectStack(root string) []string {
	markers := []struct {
		language string
		paths    []string
	}{
		{"go", []string{"go.mod"}}, {"typescript/javascript", []string{"package.json", "web/package.json", "frontend/package.json"}},
		{"python", []string{"pyproject.toml", "requirements.txt", "backend/pyproject.toml"}}, {"rust", []string{"Cargo.toml", "src-tauri/Cargo.toml"}},
		{"kotlin/java", []string{"build.gradle.kts", "build.gradle", "android/build.gradle.kts", "android/build.gradle", "pom.xml"}},
	}
	found := []string{}
	for _, marker := range markers {
		for _, name := range marker.paths {
			if _, err := os.Stat(filepath.Join(root, name)); err == nil {
				found = append(found, marker.language)
				break
			}
		}
	}
	return found
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
