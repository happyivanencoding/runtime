package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSemanticFileToolsUseNarrowOperations(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	ctx := context.Background()

	notePath := filepath.Join(root, "note.txt")
	if err := os.WriteFile(notePath, []byte("alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := rt.Call(ctx, "file_replace", map[string]any{
		"path": notePath,
		"old":  "alpha",
		"new":  "beta",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertToolResultMatchesOutputSchema(t, "file_replace", result)
	if data, err := os.ReadFile(notePath); err != nil || string(data) != "beta\n" {
		t.Fatalf("file_replace content=%q err=%v", data, err)
	}

	patch := strings.Join([]string{
		"*** Begin Patch",
		"*** Update File: note.txt",
		"@@",
		"-beta",
		"+gamma",
		"*** End Patch",
	}, "\n")
	result, err = rt.Call(ctx, "file_patch", map[string]any{"workdir": root, "patch": patch})
	if err != nil {
		t.Fatal(err)
	}
	assertToolResultMatchesOutputSchema(t, "file_patch", result)
	if data, err := os.ReadFile(notePath); err != nil || string(data) != "gamma\n" {
		t.Fatalf("file_patch content=%q err=%v", data, err)
	}

	addedPath := filepath.Join(root, "added.txt")
	result, err = rt.Call(ctx, "file_add", map[string]any{"path": addedPath, "content": "new\n", "overwrite": true})
	if err != nil {
		t.Fatal(err)
	}
	assertToolResultMatchesOutputSchema(t, "file_add", result)
	if data, err := os.ReadFile(addedPath); err != nil || string(data) != "new\n" {
		t.Fatalf("file_add content=%q err=%v", data, err)
	}
	if _, err := rt.Call(ctx, "file_add", map[string]any{"path": addedPath, "content": "overwrite\n", "overwrite": true}); err == nil {
		t.Fatal("file_add must not overwrite an existing destination")
	}

	movedPath := filepath.Join(root, "moved.txt")
	result, err = rt.Call(ctx, "file_move", map[string]any{"path": addedPath, "new_path": movedPath})
	if err != nil {
		t.Fatal(err)
	}
	assertToolResultMatchesOutputSchema(t, "file_move", result)
	if _, err := os.Stat(movedPath); err != nil {
		t.Fatalf("file_move destination missing: %v", err)
	}

	result, err = rt.Call(ctx, "file_delete", map[string]any{"path": movedPath})
	if err != nil {
		t.Fatal(err)
	}
	assertToolResultMatchesOutputSchema(t, "file_delete", result)
	if _, err := os.Stat(movedPath); !os.IsNotExist(err) {
		t.Fatalf("file_delete left destination behind: %v", err)
	}
}

func TestFilePatchRejectsCreateDeleteAndMoveOperations(t *testing.T) {
	rt, root := newCodeToolsRuntime(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	patches := []string{
		"*** Begin Patch\n*** Delete File: note.txt\n*** End Patch",
		"*** Begin Patch\n*** Add File: other.txt\n+value\n*** End Patch",
		"*** Begin Patch\n*** Update File: note.txt\n*** Move to: other.txt\n@@\n-alpha\n+beta\n*** End Patch",
	}
	for _, patch := range patches {
		if _, err := rt.Call(ctx, "file_patch", map[string]any{"workdir": root, "patch": patch}); err == nil {
			t.Fatalf("file_patch accepted a non-update operation: %s", patch)
		}
	}
}
