package app

import (
	"context"
	"fmt"
	"strings"
)

func (r *Runtime) readFile(ctx context.Context, args map[string]any) (Result, error) {
	return r.files.ReadFile(ctx, args)
}
func (r *Runtime) listDir(ctx context.Context, args map[string]any) (Result, error) {
	return r.files.ListDir(ctx, args)
}
func (r *Runtime) searchText(ctx context.Context, args map[string]any) (Result, error) {
	return r.files.SearchText(ctx, args)
}
func (r *Runtime) fileEdit(ctx context.Context, args map[string]any) (Result, error) {
	return r.files.Edit(ctx, args)
}

func (r *Runtime) fileReplace(ctx context.Context, args map[string]any) (Result, error) {
	return r.fileEditAction(ctx, "replace", args)
}

func (r *Runtime) filePatch(ctx context.Context, args map[string]any) (Result, error) {
	patch, _ := args["patch"].(string)
	trimmed := strings.TrimSpace(patch)
	if !strings.HasPrefix(trimmed, "*** Begin Patch") || !strings.HasSuffix(trimmed, "*** End Patch") {
		return nil, fmt.Errorf("file_patch requires the structured *** Begin Patch / *** End Patch envelope")
	}
	if strings.Contains(trimmed, "*** Delete File:") || strings.Contains(trimmed, "*** Move to:") || strings.Contains(trimmed, "*** Add File:") {
		return nil, fmt.Errorf("file_patch only updates an existing file; use file_add, file_delete, or file_move for create/delete/move operations")
	}
	if strings.Count(trimmed, "*** Update File:") != 1 {
		return nil, fmt.Errorf("file_patch requires exactly one *** Update File operation")
	}
	return r.fileEditAction(ctx, "patch", args)
}

func (r *Runtime) fileAdd(ctx context.Context, args map[string]any) (Result, error) {
	cloned := cloneFileArgs(args)
	cloned["overwrite"] = false
	return r.fileEditAction(ctx, "add", cloned)
}

func (r *Runtime) fileDelete(ctx context.Context, args map[string]any) (Result, error) {
	return r.fileEditAction(ctx, "delete", args)
}

func (r *Runtime) fileMove(ctx context.Context, args map[string]any) (Result, error) {
	return r.fileEditAction(ctx, "move", args)
}

func (r *Runtime) fileEditAction(ctx context.Context, action string, args map[string]any) (Result, error) {
	cloned := cloneFileArgs(args)
	cloned["action"] = action
	return r.files.Edit(ctx, cloned)
}

func cloneFileArgs(args map[string]any) map[string]any {
	cloned := make(map[string]any, len(args)+1)
	for key, value := range args {
		if key == "action" {
			continue
		}
		cloned[key] = value
	}
	return cloned
}
