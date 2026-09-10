// Copyright 2026 runtime-core contributors.
// SPDX-License-Identifier: Apache-2.0
package browserbridge

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	maxHostToExtensionMessage = 1 << 20
	maxExtensionToHostMessage = 64 << 20
)

type wireRequest struct {
	Token  string          `json:"token,omitempty"`
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type wireResponse struct {
	ID     string          `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type nativeHost struct {
	ctx    context.Context
	stdout io.Writer
	stderr io.Writer
	token  string

	writeMu sync.Mutex
	mu      sync.Mutex
	pending map[string]chan wireResponse
	closed  bool
}

func RunNativeHost(ctx context.Context, origin string, stdin io.Reader, stdout, stderr io.Writer) error {
	origin = strings.TrimSpace(origin)
	wantOrigin := "chrome-extension://" + ExtensionID + "/"
	if origin != wantOrigin {
		return fmt.Errorf("Runtime Chrome bridge rejected extension origin %q", origin)
	}
	home, err := resolveRuntimeHome()
	if err != nil {
		return err
	}
	root := filepath.Dir(StatePath(home))
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create Chrome bridge state directory: %w", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen for Runtime Chrome bridge: %w", err)
	}
	defer listener.Close()
	token, err := randomToken()
	if err != nil {
		return err
	}
	state := State{Protocol: Protocol, Address: listener.Addr().String(), Token: token, Origin: origin, ExtensionID: ExtensionID, PID: os.Getpid(), StartedAt: time.Now().UTC()}
	if err := writeState(home, state); err != nil {
		return err
	}
	defer removeStateIfCurrent(home, token)

	host := &nativeHost{ctx: ctx, stdout: stdout, stderr: stderr, token: token, pending: map[string]chan wireResponse{}}
	readerDone := make(chan error, 1)
	go func() { readerDone <- host.readExtension(stdin) }()
	acceptDone := make(chan error, 1)
	go func() { acceptDone <- host.accept(listener) }()

	select {
	case <-ctx.Done():
		host.closePending("Runtime Chrome native host stopped")
		return nil
	case err := <-readerDone:
		host.closePending("Chrome extension disconnected")
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil
		}
		return err
	case err := <-acceptDone:
		host.closePending("Runtime Chrome native host listener stopped")
		return err
	}
}

func (h *nativeHost) accept(listener net.Listener) error {
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-h.ctx.Done():
				return nil
			default:
				return err
			}
		}
		go h.serveConn(conn)
	}
}

func (h *nativeHost) serveConn(conn net.Conn) {
	defer conn.Close()
	decoder := json.NewDecoder(bufio.NewReader(io.LimitReader(conn, 8<<20)))
	encoder := json.NewEncoder(conn)
	var request wireRequest
	if err := decoder.Decode(&request); err != nil {
		_ = encoder.Encode(wireResponse{ID: request.ID, OK: false, Error: "invalid bridge request"})
		return
	}
	if request.Token != h.token || strings.TrimSpace(request.ID) == "" || strings.TrimSpace(request.Method) == "" {
		_ = encoder.Encode(wireResponse{ID: request.ID, OK: false, Error: "bridge authentication or request fields are invalid"})
		return
	}
	responseCh := make(chan wireResponse, 1)
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		_ = encoder.Encode(wireResponse{ID: request.ID, OK: false, Error: "bridge is closed"})
		return
	}
	if _, exists := h.pending[request.ID]; exists {
		h.mu.Unlock()
		_ = encoder.Encode(wireResponse{ID: request.ID, OK: false, Error: "duplicate bridge request id"})
		return
	}
	h.pending[request.ID] = responseCh
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.pending, request.ID)
		h.mu.Unlock()
	}()

	forward := wireRequest{ID: request.ID, Method: request.Method, Params: request.Params}
	if err := h.writeExtension(forward); err != nil {
		_ = encoder.Encode(wireResponse{ID: request.ID, OK: false, Error: err.Error()})
		return
	}
	select {
	case response := <-responseCh:
		_ = encoder.Encode(response)
	case <-h.ctx.Done():
		_ = encoder.Encode(wireResponse{ID: request.ID, OK: false, Error: "bridge stopped"})
	case <-time.After(5 * time.Minute):
		_ = encoder.Encode(wireResponse{ID: request.ID, OK: false, Error: "Chrome extension response timed out"})
	}
}

func (h *nativeHost) readExtension(reader io.Reader) error {
	for {
		payload, err := readNativeMessage(reader, maxExtensionToHostMessage)
		if err != nil {
			return err
		}
		var response wireResponse
		if err := json.Unmarshal(payload, &response); err != nil || strings.TrimSpace(response.ID) == "" {
			continue
		}
		h.mu.Lock()
		pending := h.pending[response.ID]
		h.mu.Unlock()
		if pending != nil {
			select {
			case pending <- response:
			default:
			}
		}
	}
}

func (h *nativeHost) writeExtension(message any) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if len(payload) > maxHostToExtensionMessage {
		return errors.New("Runtime Chrome bridge request exceeds native messaging limit")
	}
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], uint32(len(payload)))
	if _, err := h.stdout.Write(header[:]); err != nil {
		return err
	}
	_, err = h.stdout.Write(payload)
	return err
}

func (h *nativeHost) closePending(message string) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	pending := h.pending
	h.pending = map[string]chan wireResponse{}
	h.mu.Unlock()
	for id, ch := range pending {
		select {
		case ch <- wireResponse{ID: id, OK: false, Error: message}:
		default:
		}
	}
}

func readNativeMessage(reader io.Reader, maximum int) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, err
	}
	size := int(binary.LittleEndian.Uint32(header[:]))
	if size <= 0 || size > maximum {
		return nil, fmt.Errorf("invalid native messaging payload size %d", size)
	}
	payload := make([]byte, size)
	_, err := io.ReadFull(reader, payload)
	return payload, err
}

func randomToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func writeState(home string, state State) error {
	path := StatePath(home)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func removeStateIfCurrent(home, token string) {
	state, err := ReadState(home)
	if err == nil && state.Token == token {
		_ = os.Remove(StatePath(home))
	}
}
