// Package audit writes durable security and execution decisions to core.db.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Entry struct {
	OccurredAt   time.Time
	Actor        string
	Action       string
	ResourceType string
	ResourceID   string
	Outcome      string
	TraceID      string
	Details      map[string]any
}

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("audit database is required")
	}
	return &Store{db: db}, nil
}

func (s *Store) Record(ctx context.Context, entry Entry) error {
	if s == nil || s.db == nil {
		return errors.New("audit store is unavailable")
	}
	return record(ctx, s.db, entry)
}

// RecordTx writes an audit entry in the caller's transaction so the audited
// state change cannot commit without its security record.
func (s *Store) RecordTx(ctx context.Context, tx *sql.Tx, entry Entry) error {
	if s == nil || s.db == nil {
		return errors.New("audit store is unavailable")
	}
	if tx == nil {
		return errors.New("audit transaction is required")
	}
	return record(ctx, tx, entry)
}

type execContext interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func record(ctx context.Context, executor execContext, entry Entry) error {
	if ctx == nil {
		return errors.New("audit context is required")
	}
	if strings.TrimSpace(entry.Actor) == "" || strings.TrimSpace(entry.Action) == "" ||
		strings.TrimSpace(entry.ResourceType) == "" || strings.TrimSpace(entry.ResourceID) == "" || strings.TrimSpace(entry.Outcome) == "" {
		return errors.New("audit actor, action, resource, and outcome are required")
	}
	if entry.OccurredAt.IsZero() {
		entry.OccurredAt = time.Now().UTC()
	}
	if entry.Details == nil {
		entry.Details = map[string]any{}
	}
	details, err := json.Marshal(entry.Details)
	if err != nil {
		return fmt.Errorf("marshal audit details: %w", err)
	}
	_, err = executor.ExecContext(ctx, `
		INSERT INTO audit_log (
			occurred_at, actor, action, resource_type, resource_id, outcome, trace_id, details
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.OccurredAt.UTC().Format(time.RFC3339Nano), strings.TrimSpace(entry.Actor), strings.TrimSpace(entry.Action),
		strings.TrimSpace(entry.ResourceType), strings.TrimSpace(entry.ResourceID), strings.TrimSpace(entry.Outcome),
		strings.TrimSpace(entry.TraceID), string(details),
	)
	if err != nil {
		return fmt.Errorf("insert audit entry: %w", err)
	}
	return nil
}
