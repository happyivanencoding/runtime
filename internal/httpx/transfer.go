// Copyright 2026 runtime-core contributors. SPDX-License-Identifier: AGPL-3.0-only
package httpx

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/uvwt/agentdock/internal/auth"
)

const (
	transferEndpoint  = "/transfers/v1/uploads"
	transferChunkSize = int64(4 << 20)
	transferMaxSize   = int64(50) << 30
)

type transferState struct {
	ID       string
	Filename string
	Size     int64
	Offset   int64
	Complete bool
	Path     string
}

type transferReceiver struct {
	root  string
	token string
	mu    sync.Mutex
}

// The upload token grants only access to this receiver, never to Runtime MCP tools.
// Upload targets are selected by the local owner, never by the remote caller.
func newTransferHandler(root, token string) http.Handler {
	t := &transferReceiver{root: root, token: token}
	return http.HandlerFunc(t.serveHTTP)
}

func (t *transferReceiver) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if t.token == "" {
		http.NotFound(w, r)
		return
	}
	provided, ok := auth.ParseBearerToken(r.Header.Get("Authorization"))
	if !ok || subtle.ConstantTimeCompare([]byte(provided), []byte(t.token)) != 1 {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "upload authorization required", http.StatusUnauthorized)
		return
	}
	if r.URL.Path == transferEndpoint {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		t.create(w, r)
		return
	}
	if !strings.HasPrefix(r.URL.Path, transferEndpoint+"/") {
		http.NotFound(w, r)
		return
	}
	relative := strings.TrimPrefix(r.URL.Path, transferEndpoint+"/")
	parts := strings.Split(relative, "/")
	if len(parts) == 0 || !validTransferID(parts[0]) {
		http.NotFound(w, r)
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	state, err := t.readState(parts[0])
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "cannot read upload state", http.StatusInternalServerError)
		return
	}
	if len(parts) == 2 && parts[1] == "complete" && r.Method == http.MethodPost {
		t.complete(w, state)
		return
	}
	if len(parts) != 1 {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, state.response())
	case http.MethodPut:
		t.appendChunk(w, r, state)
	case http.MethodDelete:
		if state.Complete {
			http.Error(w, "completed uploads cannot be deleted remotely", http.StatusConflict)
			return
		}
		if err := os.Remove(t.partPath(state.ID)); err != nil && !errors.Is(err, os.ErrNotExist) {
			http.Error(w, "cannot remove partial upload", http.StatusInternalServerError)
			return
		}
		if err := os.Remove(t.statePath(state.ID)); err != nil {
			http.Error(w, "cannot remove upload state", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func validTransferID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func validZipFilename(name string) bool {
	if name == "" || len(name) > 180 || !utf8.ValidString(name) ||
		strings.ContainsAny(name, "\\/:*?\"<>|\x00") || strings.TrimRight(name, " .") != name ||
		!strings.EqualFold(filepath.Ext(name), ".zip") {
		return false
	}
	stem := strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name)))
	if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" {
		return false
	}
	for _, prefix := range []string{"COM", "LPT"} {
		if len(stem) == 4 && strings.HasPrefix(stem, prefix) && stem[3] >= '1' && stem[3] <= '9' {
			return false
		}
	}
	return true
}

func (t *transferReceiver) stageDir() string {
	return filepath.Join(t.root, ".runtime-uploads")
}

func (t *transferReceiver) statePath(id string) string {
	return filepath.Join(t.stageDir(), id+".json")
}

func (t *transferReceiver) partPath(id string) string {
	return filepath.Join(t.stageDir(), id+".part")
}

func (t *transferReceiver) finalPath(s transferState) string {
	return filepath.Join(t.root, s.ID+"-"+s.Filename)
}

func (s transferState) response() map[string]any {
	result := map[string]any{
		"upload_id": s.ID, "filename": s.Filename, "size": s.Size,
		"offset": s.Offset, "complete": s.Complete, "chunk_size": transferChunkSize,
	}
	if s.Complete {
		result["path"] = s.Path
	}
	return result
}

func (t *transferReceiver) create(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Filename string
		Size     int64
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		http.Error(w, "expected filename and size", http.StatusBadRequest)
		return
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		http.Error(w, "unexpected trailing data", http.StatusBadRequest)
		return
	}
	if !validZipFilename(input.Filename) || input.Size < 1 || input.Size > transferMaxSize {
		http.Error(w, "expected ZIP filename and size between 1 byte and 50 GiB", http.StatusBadRequest)
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if err := os.MkdirAll(t.stageDir(), 0700); err != nil {
		http.Error(w, "cannot create upload directory", http.StatusInternalServerError)
		return
	}
	var idBytes [16]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		http.Error(w, "cannot create upload id", http.StatusInternalServerError)
		return
	}
	state := transferState{ID: hex.EncodeToString(idBytes[:]), Filename: input.Filename, Size: input.Size}
	if err := t.saveState(state); err != nil {
		http.Error(w, "cannot create upload state", http.StatusInternalServerError)
		return
	}
	writeJSONStatus(w, http.StatusCreated, state.response())
}

func (t *transferReceiver) readState(id string) (transferState, error) {
	data, err := os.ReadFile(t.statePath(id))
	if err != nil {
		return transferState{}, err
	}
	var state transferState
	if err := json.Unmarshal(data, &state); err != nil {
		return transferState{}, err
	}
	if state.ID != id || !validZipFilename(state.Filename) || state.Size < 1 ||
		state.Size > transferMaxSize || state.Offset < 0 || state.Offset > state.Size {
		return transferState{}, fmt.Errorf("invalid upload metadata")
	}
	return state, nil
}

func (t *transferReceiver) saveState(state transferState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	tmp := t.statePath(state.ID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, t.statePath(state.ID)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (t *transferReceiver) appendChunk(w http.ResponseWriter, r *http.Request, state transferState) {
	if state.Complete {
		http.Error(w, "upload already complete", http.StatusConflict)
		return
	}
	offset, err := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
	if err != nil || offset < 0 {
		http.Error(w, "Upload-Offset header required", http.StatusBadRequest)
		return
	}
	if offset != state.Offset {
		writeJSONStatus(w, http.StatusConflict, state.response())
		return
	}
	if r.ContentLength > transferChunkSize || r.ContentLength > state.Size-state.Offset {
		http.Error(w, "chunk exceeds remaining size or 4 MiB", http.StatusRequestEntityTooLarge)
		return
	}
	// Override the server's 15-second generic ReadTimeout for slow 4 MiB uploads.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(2 * time.Minute))
	file, err := os.OpenFile(t.partPath(state.ID), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		http.Error(w, "cannot open upload", http.StatusInternalServerError)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		http.Error(w, "cannot inspect partial upload", http.StatusInternalServerError)
		return
	}
	if info.Size() < state.Offset {
		http.Error(w, "partial upload is incomplete on disk", http.StatusConflict)
		return
	}
	if err := file.Truncate(state.Offset); err != nil {
		http.Error(w, "cannot restore upload offset", http.StatusInternalServerError)
		return
	}
	if _, err := file.Seek(state.Offset, io.SeekStart); err != nil {
		http.Error(w, "cannot seek upload", http.StatusInternalServerError)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = file.Truncate(state.Offset)
		}
	}()
	count, err := io.Copy(file, http.MaxBytesReader(w, r.Body, transferChunkSize))
	if err != nil || count == 0 || count > state.Size-state.Offset {
		http.Error(w, "incomplete or oversized chunk; retry from previous offset", http.StatusBadRequest)
		return
	}
	if err := file.Sync(); err != nil {
		http.Error(w, "cannot flush upload", http.StatusInternalServerError)
		return
	}
	state.Offset += count
	if err := t.saveState(state); err != nil {
		http.Error(w, "cannot save upload offset", http.StatusInternalServerError)
		return
	}
	committed = true
	writeJSON(w, state.response())
}

func (t *transferReceiver) complete(w http.ResponseWriter, state transferState) {
	if state.Complete {
		writeJSON(w, state.response())
		return
	}
	if state.Offset != state.Size {
		writeJSONStatus(w, http.StatusConflict, state.response())
		return
	}
	target := t.finalPath(state)
	if info, err := os.Stat(target); err == nil {
		if info.Size() != state.Size {
			http.Error(w, "destination already exists with wrong size", http.StatusConflict)
			return
		}
	} else if errors.Is(err, os.ErrNotExist) {
		info, err := os.Stat(t.partPath(state.ID))
		if err != nil || info.Size() != state.Size {
			http.Error(w, "partial upload missing or incomplete", http.StatusConflict)
			return
		}
		if err := os.Rename(t.partPath(state.ID), target); err != nil {
			http.Error(w, "cannot finalize upload", http.StatusInternalServerError)
			return
		}
	} else {
		http.Error(w, "cannot inspect upload destination", http.StatusInternalServerError)
		return
	}
	state.Complete = true
	state.Path = target
	if err := t.saveState(state); err != nil {
		http.Error(w, "cannot record upload completion", http.StatusInternalServerError)
		return
	}
	writeJSON(w, state.response())
}
