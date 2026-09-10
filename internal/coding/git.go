// Copyright 2026 Jingxuan Li and runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package coding

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/taskstate"
)

const maxGitOutput = 4 << 20

type limitedOutput struct {
	bytes.Buffer
	truncated bool
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	room := maxGitOutput - b.Len()
	if n > room {
		b.truncated = true
		p = p[:room]
	}
	_, err := b.Buffer.Write(p)
	return n, err
}

func gitRun(ctx context.Context, dir string, args ...string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	argv := append([]string{"--literal-pathspecs", "-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", argv...)
	var out, stderr limitedOutput
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return out.String(), out.truncated, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), out.truncated, nil
}

func gitText(ctx context.Context, dir string, args ...string) (string, error) {
	text, truncated, err := gitRun(ctx, dir, args...)
	if err == nil && truncated {
		err = errors.New("structured Git output exceeds 4 MiB; narrow the request")
	}
	return strings.TrimSpace(text), err
}

func GitStatus(ctx context.Context, dir string) (taskstate.GitState, error) {
	text, truncated, err := gitRun(ctx, dir, "status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all")
	if err != nil {
		return taskstate.GitState{}, err
	}
	if truncated {
		return taskstate.GitState{}, errors.New("Git status exceeds 4 MiB; not returning partial state")
	}
	return parseStatus(text)
}

func parseStatus(text string) (taskstate.GitState, error) {
	state := taskstate.GitState{Files: []taskstate.ChangedFile{}}
	records := strings.Split(text, "\x00")
	for i := 0; i < len(records); i++ {
		rec := records[i]
		if rec == "" {
			continue
		}
		if strings.HasPrefix(rec, "# ") {
			kv := strings.SplitN(rec[2:], " ", 2)
			if len(kv) != 2 {
				continue
			}
			switch kv[0] {
			case "branch.oid":
				if kv[1] != "(initial)" {
					state.Head = kv[1]
				}
			case "branch.head":
				state.Branch = kv[1]
			case "branch.upstream":
				state.Upstream = kv[1]
			case "branch.ab":
				_, err := fmt.Sscanf(kv[1], "+%d -%d", &state.Ahead, &state.Behind)
				if err != nil {
					return state, err
				}
			}
			continue
		}
		f := taskstate.ChangedFile{Source: "working-tree"}
		switch rec[0] {
		case '?':
			f.Path = rec[2:]
			f.Index = "?"
			f.Worktree = "?"
		case '1', '2', 'u':
			n := 9
			if rec[0] == '2' {
				n = 10
			}
			if rec[0] == 'u' {
				n = 11
			}
			parts := strings.SplitN(rec, " ", n)
			if len(parts) != n || len(parts[1]) != 2 {
				return state, errors.New("invalid Git porcelain record")
			}
			f.Path = parts[n-1]
			f.Index = parts[1][:1]
			f.Worktree = parts[1][1:]
			if rec[0] == '2' {
				i++
				if i >= len(records) {
					return state, errors.New("missing rename source")
				}
				f.OriginalPath = records[i]
			}
		default:
			return state, fmt.Errorf("unsupported Git porcelain record: %q", rec)
		}
		state.Files = append(state.Files, f)
	}
	state.Clean = len(state.Files) == 0
	return state, nil
}

type Diff struct {
	Text      string `json:"diff"`
	Truncated bool   `json:"truncated"`
	Scope     string `json:"scope"`
}

func GitDiff(ctx context.Context, dir, scope, base string, paths []string) (Diff, error) {
	args := []string{"diff", "--no-ext-diff", "--no-textconv"}
	switch scope {
	case "unstaged":
	case "staged":
		args = append(args, "--cached")
	case "task":
		if base != "" {
			args = append(args, base)
		}
	default:
		return Diff{}, errors.New("diff scope must be task, staged or unstaged")
	}
	args = append(args, "--")
	args = append(args, paths...)
	text, truncated, err := gitRun(ctx, dir, args...)
	return Diff{Text: text, Truncated: truncated, Scope: scope}, err
}

func GitChangedFiles(ctx context.Context, dir, base string) ([]taskstate.ChangedFile, error) {
	state, err := GitStatus(ctx, dir)
	if err != nil {
		return nil, err
	}
	files := state.Files
	seen := map[string]bool{}
	for _, f := range files {
		seen[f.Path] = true
	}
	if base != "" && state.Head != "" && base != state.Head {
		text, truncated, err := gitRun(ctx, dir, "diff", "--name-only", "-z", base, state.Head, "--")
		if err != nil {
			return nil, err
		}
		if truncated {
			return nil, errors.New("changed file list exceeds 4 MiB")
		}
		for _, path := range strings.Split(text, "\x00") {
			if path != "" && !seen[path] {
				files = append(files, taskstate.ChangedFile{Path: path, Index: ".", Worktree: ".", Source: "committed"})
				seen[path] = true
			}
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

type Commit struct {
	ID      string `json:"id"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Subject string `json:"subject"`
}

func GitCommits(ctx context.Context, dir string, limit int) ([]Commit, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("commit limit must be between 1 and 100")
	}
	state, err := GitStatus(ctx, dir)
	if err != nil {
		return nil, err
	}
	if state.Head == "" {
		return []Commit{}, nil
	}
	text, err := gitText(ctx, dir, "log", "-z", "--format=%H%x00%an%x00%aI%x00%s", "-n", strconv.Itoa(limit), "--")
	if err != nil {
		return nil, err
	}
	fields := strings.Split(strings.TrimSuffix(text, "\x00"), "\x00")
	commits := []Commit{}
	if len(fields)%4 != 0 {
		return nil, errors.New("invalid Git log record")
	}
	for i := 0; i < len(fields); i += 4 {
		commits = append(commits, Commit{fields[i], fields[i+1], fields[i+2], fields[i+3]})
	}
	return commits, nil
}

type Branch struct {
	Name     string `json:"name"`
	Commit   string `json:"commit"`
	Current  bool   `json:"current"`
	Upstream string `json:"upstream"`
}

func GitBranches(ctx context.Context, dir string) ([]Branch, error) {
	text, err := gitText(ctx, dir, "for-each-ref", "--format=%(refname:short)%00%(objectname)%00%(HEAD)%00%(upstream:short)", "refs/heads")
	if err != nil {
		return nil, err
	}
	branches := []Branch{}
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(strings.TrimSuffix(line, "\r"), "\x00")
		if len(f) != 4 {
			return nil, errors.New("invalid Git branch record")
		}
		branches = append(branches, Branch{f[0], f[1], f[2] == "*", f[3]})
	}
	return branches, nil
}

type Worktree struct {
	Path     string `json:"path"`
	Head     string `json:"head"`
	Branch   string `json:"branch"`
	Bare     bool   `json:"bare"`
	Detached bool   `json:"detached"`
	Locked   bool   `json:"locked"`
	Prunable bool   `json:"prunable"`
}

func GitWorktrees(ctx context.Context, dir string) ([]Worktree, error) {
	text, truncated, err := gitRun(ctx, dir, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	if truncated {
		return nil, errors.New("worktree list exceeds 4 MiB")
	}
	trees := []Worktree{}
	var current *Worktree
	for _, record := range strings.Split(text, "\x00") {
		if record == "" {
			continue
		}
		f := strings.SplitN(record, " ", 2)
		value := ""
		if len(f) == 2 {
			value = f[1]
		}
		if f[0] == "worktree" {
			trees = append(trees, Worktree{Path: value})
			current = &trees[len(trees)-1]
			continue
		}
		if current == nil {
			return nil, errors.New("invalid worktree record")
		}
		switch f[0] {
		case "HEAD":
			current.Head = value
		case "branch":
			current.Branch = strings.TrimPrefix(value, "refs/heads/")
		case "bare":
			current.Bare = true
		case "detached":
			current.Detached = true
		case "locked":
			current.Locked = true
		case "prunable":
			current.Prunable = true
		}
	}
	return trees, nil
}

func ensureManagedWorktree(ctx context.Context, c taskstate.CodingState) error {
	trees, err := GitWorktrees(ctx, c.Repository)
	if err != nil {
		return err
	}
	for _, tree := range trees {
		if samePath(tree.Path, c.Workspace) {
			if tree.Branch != c.Branch {
				return errors.New("existing worktree branch does not match saved task; preserved")
			}
			return nil
		}
	}
	if _, err := os.Stat(c.Workspace); err == nil {
		return errors.New("managed workspace destination already exists outside the saved Git worktree; preserved")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.Workspace), 0700); err != nil {
		return err
	}
	_, err = gitText(ctx, c.Repository, "worktree", "add", "-b", c.Branch, c.Workspace, c.BaseCommit)
	return err
}

func GitCommit(ctx context.Context, dir, message string, paths []string) (string, error) {
	if strings.TrimSpace(message) == "" || len(paths) == 0 {
		return "", errors.New("message and explicit file paths are required")
	}
	for _, path := range paths {
		clean := filepath.Clean(path)
		if filepath.IsAbs(path) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "", errors.New("commit paths must name explicit repository-relative files")
		}
	}
	args := append([]string{"add", "--"}, paths...)
	if _, err := gitText(ctx, dir, args...); err != nil {
		return "", err
	}
	args = append([]string{"commit", "--only", "-m", message, "--"}, paths...)
	if _, err := gitText(ctx, dir, args...); err != nil {
		return "", err
	}
	return gitText(ctx, dir, "rev-parse", "HEAD")
}
