package mcpreceipt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/uvwt/agentdock/internal/fs/atomicfile"
)

const (
	Version           = 1
	MaxKeyLength      = 200
	MinKeyLength      = 8
	maxRecordSize     = 64 << 10
	defaultMaxRecords = 2048
)

type Record struct {
	Version              int            `json:"version"`
	ReceiptID            string         `json:"receipt_id"`
	RequestID            string         `json:"request_id,omitempty"`
	Tool                 string         `json:"tool"`
	IdempotencyKeySHA256 string         `json:"idempotency_key_sha256"`
	ArgumentsSHA256      string         `json:"arguments_sha256"`
	Status               string         `json:"status"`
	StartedAt            time.Time      `json:"started_at"`
	FinishedAt           *time.Time     `json:"finished_at,omitempty"`
	OK                   *bool          `json:"ok,omitempty"`
	ErrorSummary         string         `json:"error_summary,omitempty"`
	ResultSHA256         string         `json:"result_sha256,omitempty"`
	Summary              map[string]any `json:"summary,omitempty"`
}

type Store struct {
	dir        string
	maxRecords int
	mu         sync.Mutex
}

func New(home string) *Store {
	home = strings.TrimSpace(home)
	if home == "" {
		return &Store{}
	}
	return &Store{dir: filepath.Join(home, "mcp-receipts"), maxRecords: defaultMaxRecords}
}

func HashBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func HashArguments(arguments map[string]any) (string, error) {
	encoded, err := json.Marshal(arguments)
	if err != nil {
		return "", fmt.Errorf("encode receipt arguments: %w", err)
	}
	return HashBytes(encoded), nil
}

func ReceiptIDForKey(key string) (string, string, error) {
	key = strings.TrimSpace(key)
	if len(key) < MinKeyLength || len(key) > MaxKeyLength {
		return "", "", fmt.Errorf("idempotency_key must contain %d-%d characters", MinKeyLength, MaxKeyLength)
	}
	digest := sha256.Sum256([]byte(key))
	hexDigest := hex.EncodeToString(digest[:])
	return "rcpt_" + hexDigest[:24], "sha256:" + hexDigest, nil
}

func (s *Store) Begin(key, tool, argumentsSHA256, requestID string) (Record, bool, error) {
	if s == nil || s.dir == "" {
		return Record{}, false, errors.New("MCP receipt store is unavailable")
	}
	receiptID, keyHash, err := ReceiptIDForKey(key)
	if err != nil {
		return Record{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, err := s.readUnlocked(receiptID); err == nil {
		return existing, true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Record{}, false, err
	}
	if err := s.makeRoomUnlocked(); err != nil {
		return Record{}, false, err
	}
	record := Record{
		Version:              Version,
		ReceiptID:            receiptID,
		RequestID:            requestID,
		Tool:                 tool,
		IdempotencyKeySHA256: keyHash,
		ArgumentsSHA256:      argumentsSHA256,
		Status:               "started",
		StartedAt:            time.Now().UTC(),
	}
	if err := s.writeUnlocked(record); err != nil {
		return Record{}, false, err
	}
	return record, false, nil
}

func (s *Store) makeRoomUnlocked() error {
	limit := s.maxRecords
	if limit <= 0 {
		limit = defaultMaxRecords
	}
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	type candidate struct {
		id        string
		startedAt time.Time
	}
	count := 0
	finished := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		record, readErr := s.readUnlocked(id)
		if readErr != nil {
			continue
		}
		count++
		if record.Status == "completed" || record.Status == "failed" {
			finished = append(finished, candidate{id: id, startedAt: record.StartedAt})
		}
	}
	if count < limit {
		return nil
	}
	sort.Slice(finished, func(i, j int) bool { return finished[i].startedAt.Before(finished[j].startedAt) })
	for _, item := range finished {
		if count < limit {
			break
		}
		if err := os.Remove(filepath.Join(s.dir, item.id+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		count--
	}
	if count >= limit {
		return fmt.Errorf("MCP receipt store is full with %d unresolved receipts; inspect started receipts before new mutations", count)
	}
	return nil
}

func (s *Store) Finish(record Record, ok bool, errorSummary, resultSHA256 string, summary map[string]any) (Record, error) {
	if s == nil || s.dir == "" {
		return record, errors.New("MCP receipt store is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	record.FinishedAt = &now
	record.OK = new(bool)
	*record.OK = ok
	if ok {
		record.Status = "completed"
	} else {
		record.Status = "failed"
	}
	record.ErrorSummary = truncate(strings.TrimSpace(errorSummary), 512)
	record.ResultSHA256 = resultSHA256
	record.Summary = summary
	if err := s.writeUnlocked(record); err != nil {
		return record, err
	}
	return record, nil
}

func (s *Store) GetByKey(key string) (Record, error) {
	receiptID, _, err := ReceiptIDForKey(key)
	if err != nil {
		return Record{}, err
	}
	return s.GetByID(receiptID)
}

func (s *Store) GetByID(receiptID string) (Record, error) {
	if s == nil || s.dir == "" {
		return Record{}, errors.New("MCP receipt store is unavailable")
	}
	if !validReceiptID(receiptID) {
		return Record{}, errors.New("invalid receipt_id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readUnlocked(receiptID)
}

func (s *Store) Recent(limit int) ([]Record, error) {
	if s == nil || s.dir == "" {
		return nil, errors.New("MCP receipt store is unavailable")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Record{}, nil
	}
	if err != nil {
		return nil, err
	}
	records := make([]Record, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		record, err := s.readUnlocked(strings.TrimSuffix(entry.Name(), ".json"))
		if err == nil {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].StartedAt.After(records[j].StartedAt) })
	if len(records) > limit {
		records = records[:limit]
	}
	return records, nil
}

func (s *Store) readUnlocked(receiptID string) (Record, error) {
	path := filepath.Join(s.dir, receiptID+".json")
	info, err := os.Stat(path)
	if err != nil {
		return Record{}, err
	}
	if info.Size() > maxRecordSize {
		return Record{}, errors.New("MCP receipt exceeds size limit")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Record{}, err
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return Record{}, fmt.Errorf("decode MCP receipt: %w", err)
	}
	if record.Version != Version || record.ReceiptID != receiptID {
		return Record{}, errors.New("invalid MCP receipt record")
	}
	return record, nil
}

func (s *Store) writeUnlocked(record Record) error {
	if !validReceiptID(record.ReceiptID) {
		return errors.New("invalid receipt_id")
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode MCP receipt: %w", err)
	}
	if len(data) > maxRecordSize {
		return errors.New("MCP receipt exceeds size limit")
	}
	return atomicfile.Write(filepath.Join(s.dir, record.ReceiptID+".json"), append(data, '\n'), 0o600)
}

func validReceiptID(value string) bool {
	if len(value) != len("rcpt_")+24 || !strings.HasPrefix(value, "rcpt_") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "rcpt_"))
	return err == nil
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
