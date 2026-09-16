package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/uvwt/agentdock/internal/mcpreceipt"
)

func (r *Runtime) requestReceipt(_ context.Context, args map[string]any) (Result, error) {
	store := mcpreceipt.New(r.cfg.AgentDockHome)
	key, _ := args["idempotency_key"].(string)
	receiptID, _ := args["receipt_id"].(string)
	key = strings.TrimSpace(key)
	receiptID = strings.TrimSpace(receiptID)
	if key != "" && receiptID != "" {
		return nil, toolErrorDetails("INVALID_ARGUMENT", "choose idempotency_key or receipt_id, not both", "validation", nil)
	}
	if key != "" {
		record, err := store.GetByKey(key)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, toolErrorDetails("RECEIPT_NOT_FOUND", "mutation receipt not found", "not_found", nil)
			}
			return nil, toolErrorCause("RECEIPT_READ_FAILED", "read mutation receipt", "filesystem", nil, err)
		}
		view, err := receiptView(record)
		if err != nil {
			return nil, toolErrorCause("RECEIPT_ENCODE_FAILED", "encode mutation receipt", "serialization", nil, err)
		}
		return Result{"receipt": view}, nil
	}
	if receiptID != "" {
		record, err := store.GetByID(receiptID)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, toolErrorDetails("RECEIPT_NOT_FOUND", "mutation receipt not found", "not_found", nil)
			}
			return nil, toolErrorCause("RECEIPT_READ_FAILED", "read mutation receipt", "filesystem", nil, err)
		}
		view, err := receiptView(record)
		if err != nil {
			return nil, toolErrorCause("RECEIPT_ENCODE_FAILED", "encode mutation receipt", "serialization", nil, err)
		}
		return Result{"receipt": view}, nil
	}
	limit := 20
	if value, ok := args["limit"].(float64); ok {
		limit = int(value)
	} else if value, ok := args["limit"].(int); ok {
		limit = value
	}
	records, err := store.Recent(limit)
	if err != nil {
		return nil, toolErrorCause("RECEIPT_READ_FAILED", "list mutation receipts", "filesystem", nil, err)
	}
	views := make([]map[string]any, 0, len(records))
	for _, record := range records {
		view, err := receiptView(record)
		if err != nil {
			return nil, toolErrorCause("RECEIPT_ENCODE_FAILED", "encode mutation receipt", "serialization", nil, err)
		}
		views = append(views, view)
	}
	return Result{"receipts": views, "count": len(views)}, nil
}

func receiptView(record mcpreceipt.Record) (map[string]any, error) {
	encoded, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("marshal receipt: %w", err)
	}
	var view map[string]any
	if err := json.Unmarshal(encoded, &view); err != nil {
		return nil, fmt.Errorf("unmarshal receipt view: %w", err)
	}
	return view, nil
}
