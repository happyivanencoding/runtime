package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uvwt/agentdock/internal/app"
	"github.com/uvwt/agentdock/internal/auth"
	"github.com/uvwt/agentdock/internal/mcp"
)

func transferCall(t *testing.T, handler http.Handler, method, path string, body io.Reader, token string, offset int64) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if method == http.MethodPut {
		req.Header.Set("Upload-Offset", fmt.Sprintf("%d", offset))
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func readTransferResponse(t *testing.T, response *httptest.ResponseRecorder, want int) map[string]any {
	t.Helper()
	if response.Code != want {
		t.Fatalf("HTTP status %d, want %d: %s", response.Code, want, response.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	return result
}

func TestUploadRequiresSeparateAuthorization(t *testing.T) {
	cfg := testConfig(t)
	cfg.AuthToken = "normal-mcp-secret"
	runtime, err := app.NewRuntime(cfg)
	if err != nil {
		t.Fatal(err)
	}
	upload := newTransferHandler(t.TempDir(), "upload-only-secret")
	for _, token := range []string{"", "normal-mcp-secret", "wrong"} {
		response := transferCall(t, upload, http.MethodPost, transferEndpoint,
			strings.NewReader(`{"filename":"bundle.zip","size":42}`), token, 0)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("upload token=%q got status %d, want 401", token, response.Code)
		}
	}
	if response := transferCall(t, upload, http.MethodPost, transferEndpoint,
		strings.NewReader(`{"filename":"bundle.zip","size":42}`), "upload-only-secret", 0); response.Code != http.StatusCreated {
		t.Fatalf("valid upload-only token status %d", response.Code)
	}
	mcpHandler := mcpEndpointHandler(mcp.NewServer(runtime, cfg), cfg, auth.NewOAuthStore())
	request := newMCPRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	request.Header.Set("Authorization", "Bearer upload-only-secret")
	response := httptest.NewRecorder()
	mcpHandler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("upload-only token unexpectedly granted MCP access: status %d", response.Code)
	}
	if response := transferCall(t, newTransferHandler(t.TempDir(), ""), http.MethodPost,
		transferEndpoint, nil, "upload-only-secret", 0); response.Code != http.StatusNotFound {
		t.Fatalf("upload not disabled when unconfigured: status %d", response.Code)
	}
}

func TestChunkedTransferResumesAndPreservesCompletedZIP(t *testing.T) {
	root := t.TempDir()
	handler := newTransferHandler(root, "upload-secret")
	content := bytes.Repeat([]byte("example ZIP content payload "), (6<<20)/28+1)
	content = content[:(5<<20)+17]
	created := readTransferResponse(t, transferCall(t, handler, http.MethodPost, transferEndpoint,
		strings.NewReader(fmt.Sprintf(`{"filename":"仙侠素材.zip","size":%d}`, len(content))), "upload-secret", 0), http.StatusCreated)
	id, ok := created["upload_id"].(string)
	if !ok || !validTransferID(id) {
		t.Fatalf("invalid upload id: %v", created)
	}
	item := transferEndpoint + "/" + id
	status := readTransferResponse(t, transferCall(t, handler, http.MethodPut, item,
		bytes.NewReader(content[:transferChunkSize]), "upload-secret", 0), http.StatusOK)
	if int64(status["offset"].(float64)) != transferChunkSize {
		t.Fatalf("chunk 1 offset %v", status["offset"])
	}
	// A client retrying a previously accepted chunk must resynchronize.
	reminder := readTransferResponse(t, transferCall(t, handler, http.MethodPut, item,
		bytes.NewReader(content[:100]), "upload-secret", 0), http.StatusConflict)
	if int64(reminder["offset"].(float64)) != transferChunkSize {
		t.Fatalf("conflict offset %v", reminder["offset"])
	}
	if response := transferCall(t, handler, http.MethodPost, item+"/complete",
		nil, "upload-secret", 0); response.Code != http.StatusConflict {
		t.Fatalf("incomplete upload finalized: %d", response.Code)
	}
	// Simulate Runtime restart: resume from durable state and the existing .part file.
	handler = newTransferHandler(root, "upload-secret")
	pending := readTransferResponse(t, transferCall(t, handler, http.MethodGet, item,
		nil, "upload-secret", 0), http.StatusOK)
	if int64(pending["offset"].(float64)) != transferChunkSize {
		t.Fatalf("restart offset %v", pending["offset"])
	}
	result := readTransferResponse(t, transferCall(t, handler, http.MethodPut, item,
		bytes.NewReader(content[transferChunkSize:]), "upload-secret", transferChunkSize), http.StatusOK)
	if int(result["offset"].(float64)) != len(content) {
		t.Fatalf("final chunk offset %v", result["offset"])
	}
	completed := readTransferResponse(t, transferCall(t, handler, http.MethodPost, item+"/complete",
		nil, "upload-secret", 0), http.StatusOK)
	if completed["complete"] != true {
		t.Fatalf("completion not recorded: %v", completed)
	}
	target, ok := completed["path"].(string)
	if !ok || filepath.Dir(target) != root || filepath.Base(target) != id+"-仙侠素材.zip" {
		t.Fatalf("unexpected destination %q", target)
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("completed bytes mismatch, error=%v", err)
	}
	again := readTransferResponse(t, transferCall(t, newTransferHandler(root, "upload-secret"),
		http.MethodPost, item+"/complete", nil, "upload-secret", 0), http.StatusOK)
	if again["path"] != target {
		t.Fatalf("retry changed destination %v", again)
	}
	if _, err := os.Stat(filepath.Join(root, ".runtime-uploads", id+".part")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial file remains after completion: %v", err)
	}
}

type interruptedTransferReader struct{}

func (interruptedTransferReader) Read([]byte) (int, error) {
	return 0, errors.New("network disconnected")
}

func TestInterruptedChunkRollsBackAndCanRetry(t *testing.T) {
	handler := newTransferHandler(t.TempDir(), "upload-secret")
	created := readTransferResponse(t, transferCall(t, handler, http.MethodPost, transferEndpoint,
		strings.NewReader(`{"filename":"retry.zip","size":7}`), "upload-secret", 0), http.StatusCreated)
	item := transferEndpoint + "/" + created["upload_id"].(string)
	partiallyReceived := io.MultiReader(bytes.NewReader([]byte("abc")), interruptedTransferReader{})
	if response := transferCall(t, handler, http.MethodPut, item,
		partiallyReceived, "upload-secret", 0); response.Code != http.StatusBadRequest {
		t.Fatalf("interrupted chunk status %d", response.Code)
	}
	status := readTransferResponse(t, transferCall(t, handler, http.MethodGet, item,
		nil, "upload-secret", 0), http.StatusOK)
	if status["offset"].(float64) != 0 {
		t.Fatalf("interrupted chunk committed %v", status["offset"])
	}
	if response := transferCall(t, handler, http.MethodPut, item,
		bytes.NewReader([]byte("1234567")), "upload-secret", 0); response.Code != http.StatusOK {
		t.Fatalf("retry status %d: %s", response.Code, response.Body)
	}
}

func TestTransferRejectsInvalidNamesAndOversizedChunks(t *testing.T) {
	handler := newTransferHandler(t.TempDir(), "upload-secret")
	for _, filename := range []string{"../escape.zip", "bad/name.zip", "CON.zip", "bad.txt", "bad\\name.zip"} {
		body, _ := json.Marshal(map[string]any{"filename": filename, "size": 12})
		response := transferCall(t, handler, http.MethodPost, transferEndpoint,
			bytes.NewReader(body), "upload-secret", 0)
		if response.Code != http.StatusBadRequest {
			t.Errorf("filename %q accepted: %d", filename, response.Code)
		}
	}
	created := readTransferResponse(t, transferCall(t, handler, http.MethodPost, transferEndpoint,
		strings.NewReader(fmt.Sprintf(`{"filename":"large.zip","size":%d}`, transferChunkSize+1)),
		"upload-secret", 0), http.StatusCreated)
	item := transferEndpoint + "/" + created["upload_id"].(string)
	response := transferCall(t, handler, http.MethodPut, item,
		bytes.NewReader(make([]byte, transferChunkSize+1)), "upload-secret", 0)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized chunk HTTP %d", response.Code)
	}
}
