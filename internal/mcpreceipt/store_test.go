package mcpreceipt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreDeduplicatesAndKeepsRawInputsOutOfReceipts(t *testing.T) {
	home := t.TempDir()
	store := New(home)
	key := "private-idempotency-key-0001"
	arguments := map[string]any{"cmd": "Write-Output super-secret-command", "path": "C:/private/example"}
	argumentsHash, err := HashArguments(arguments)
	if err != nil {
		t.Fatal(err)
	}
	record, existing, err := store.Begin(key, "exec_command", argumentsHash, "request-0001")
	if err != nil || existing {
		t.Fatalf("Begin() existing=%v err=%v", existing, err)
	}
	if record.Status != "started" || record.ReceiptID == "" {
		t.Fatalf("record = %#v", record)
	}
	second, existing, err := store.Begin(key, "exec_command", argumentsHash, "request-0002")
	if err != nil || !existing || second.ReceiptID != record.ReceiptID {
		t.Fatalf("duplicate Begin() record=%#v existing=%v err=%v", second, existing, err)
	}
	finished, err := store.Finish(record, true, "", HashBytes([]byte("result")), map[string]any{"exit_code": 0})
	if err != nil || finished.Status != "completed" || finished.OK == nil || !*finished.OK {
		t.Fatalf("Finish() = %#v err=%v", finished, err)
	}
	loaded, err := store.GetByKey(key)
	if err != nil || loaded.Status != "completed" || loaded.ResultSHA256 == "" {
		t.Fatalf("GetByKey() = %#v err=%v", loaded, err)
	}
	data, err := os.ReadFile(filepath.Join(home, "mcp-receipts", record.ReceiptID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, raw := range []string{key, "super-secret-command", "C:/private/example"} {
		if strings.Contains(text, raw) {
			t.Fatalf("receipt persisted raw sensitive input %q: %s", raw, text)
		}
	}
	for _, want := range []string{record.ReceiptID, argumentsHash, "sha256:"} {
		if !strings.Contains(text, want) {
			t.Fatalf("receipt missing %q: %s", want, text)
		}
	}
}

func TestStoreStaysBoundedWithoutDroppingUnresolvedReceipts(t *testing.T) {
	home := t.TempDir()
	store := New(home)
	store.maxRecords = 3

	for i := 0; i < 3; i++ {
		key := fmt.Sprintf("bounded-key-%04d", i)
		record, existing, err := store.Begin(key, "file_edit", HashBytes([]byte(key)), fmt.Sprintf("request-%04d", i))
		if err != nil || existing {
			t.Fatalf("Begin(%d) existing=%v err=%v", i, existing, err)
		}
		if i < 2 {
			if _, err := store.Finish(record, true, "", HashBytes([]byte("ok")), nil); err != nil {
				t.Fatal(err)
			}
		}
	}

	if _, _, err := store.Begin("bounded-key-0003", "file_edit", HashBytes([]byte("next")), "request-0003"); err != nil {
		t.Fatalf("expected oldest finished receipt to be pruned: %v", err)
	}
	records, err := store.Recent(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("receipt count=%d, want 3", len(records))
	}
	if _, err := store.GetByKey("bounded-key-0002"); err != nil {
		t.Fatalf("unresolved receipt was pruned: %v", err)
	}

	blocked := New(t.TempDir())
	blocked.maxRecords = 2
	for i := 0; i < 2; i++ {
		key := fmt.Sprintf("unresolved-key-%04d", i)
		if _, _, err := blocked.Begin(key, "exec_command", HashBytes([]byte(key)), fmt.Sprintf("request-u-%04d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := blocked.Begin("unresolved-key-0002", "exec_command", HashBytes([]byte("blocked")), "request-u-0002"); err == nil || !strings.Contains(err.Error(), "full with 2 unresolved") {
		t.Fatalf("expected full unresolved store to reject new receipt, got %v", err)
	}
}
