// Package events implements the append-only Core Event Store.
package events

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yyZe0122/yunmengze-agent/pkg/eventapi"
)

const MaxListLimit = 1000

var (
	ErrEventIDConflict = errors.New("event ID already exists with different content")
	ErrInvalidEvent    = errors.New("invalid event")
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("event store database is required")
	}
	return &Store{db: db}, nil
}

// Append writes an immutable event and assigns its global sequence. event_id is
// the idempotency key: an identical retry returns the first stored event.
func (s *Store) Append(ctx context.Context, event eventapi.Envelope) (eventapi.Envelope, error) {
	return appendWith(ctx, s.db, event)
}

// AppendTx appends an event inside a caller-owned transaction. This lets Kernel
// state changes and their immutable event commit or roll back atomically.
func (s *Store) AppendTx(ctx context.Context, tx *sql.Tx, event eventapi.Envelope) (eventapi.Envelope, error) {
	if tx == nil {
		return eventapi.Envelope{}, errors.New("event transaction is required")
	}
	return appendWith(ctx, tx, event)
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func appendWith(ctx context.Context, executor queryRower, event eventapi.Envelope) (eventapi.Envelope, error) {
	if ctx == nil {
		return eventapi.Envelope{}, errors.New("append context is required")
	}
	compareOccurredAt := !event.OccurredAt.IsZero()
	normalized, err := normalize(event)
	if err != nil {
		return eventapi.Envelope{}, err
	}

	var sequence int64
	err = executor.QueryRowContext(ctx, `
        INSERT INTO events (
            event_id, event_type, aggregate_type, aggregate_id,
            aggregate_version, occurred_at, producer, schema_version,
            trace_id, causation_id, correlation_id, payload
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        RETURNING sequence`,
		normalized.ID,
		normalized.Type,
		normalized.AggregateType,
		normalized.AggregateID,
		normalized.AggregateVersion,
		normalized.OccurredAt.Format(time.RFC3339Nano),
		normalized.Producer,
		normalized.SchemaVersion,
		normalized.TraceID,
		normalized.CausationID,
		normalized.CorrelationID,
		string(normalized.Payload),
	).Scan(&sequence)
	if err == nil {
		normalized.Sequence = uint64(sequence)
		return normalized, nil
	}

	existing, lookupErr := getByID(ctx, executor, normalized.ID)
	if lookupErr == nil {
		if equivalent(existing, normalized, compareOccurredAt) {
			return existing, nil
		}
		return eventapi.Envelope{}, fmt.Errorf("%w: %s", ErrEventIDConflict, normalized.ID)
	}
	if !errors.Is(lookupErr, sql.ErrNoRows) {
		return eventapi.Envelope{}, fmt.Errorf("resolve append failure: %w", lookupErr)
	}
	return eventapi.Envelope{}, fmt.Errorf("append event: %w", err)
}
func (s *Store) ListAfter(ctx context.Context, sequence uint64, limit int) ([]eventapi.Envelope, error) {
	if ctx == nil {
		return nil, errors.New("list context is required")
	}
	if err := validateLimit(limit); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
        SELECT sequence, event_id, event_type, aggregate_type, aggregate_id,
               aggregate_version, occurred_at, producer, schema_version,
               trace_id, causation_id, correlation_id, payload
        FROM events
        WHERE sequence > ?
        ORDER BY sequence ASC
        LIMIT ?`, sequence, limit)
	if err != nil {
		return nil, fmt.Errorf("list events after sequence: %w", err)
	}
	defer rows.Close()
	return collect(rows)
}

func (s *Store) ListAggregate(
	ctx context.Context,
	aggregateType string,
	aggregateID string,
	afterVersion uint64,
	limit int,
) ([]eventapi.Envelope, error) {
	if ctx == nil {
		return nil, errors.New("list context is required")
	}
	if strings.TrimSpace(aggregateType) == "" || strings.TrimSpace(aggregateID) == "" {
		return nil, fmt.Errorf("%w: aggregate type and ID are required", ErrInvalidEvent)
	}
	if err := validateLimit(limit); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
        SELECT sequence, event_id, event_type, aggregate_type, aggregate_id,
               aggregate_version, occurred_at, producer, schema_version,
               trace_id, causation_id, correlation_id, payload
        FROM events
        WHERE aggregate_type = ? AND aggregate_id = ? AND aggregate_version > ?
        ORDER BY aggregate_version ASC
        LIMIT ?`, aggregateType, aggregateID, afterVersion, limit)
	if err != nil {
		return nil, fmt.Errorf("list aggregate events: %w", err)
	}
	defer rows.Close()
	return collect(rows)
}

func getByID(ctx context.Context, executor queryRower, eventID string) (eventapi.Envelope, error) {
	row := executor.QueryRowContext(ctx, `
        SELECT sequence, event_id, event_type, aggregate_type, aggregate_id,
               aggregate_version, occurred_at, producer, schema_version,
               trace_id, causation_id, correlation_id, payload
        FROM events
        WHERE event_id = ?`, eventID)
	return scan(row)
}
func normalize(event eventapi.Envelope) (eventapi.Envelope, error) {
	if event.Sequence != 0 {
		return eventapi.Envelope{}, fmt.Errorf("%w: sequence is assigned by the store", ErrInvalidEvent)
	}
	required := map[string]string{
		"event_id":       event.ID,
		"event_type":     event.Type,
		"aggregate_type": event.AggregateType,
		"aggregate_id":   event.AggregateID,
		"producer":       event.Producer,
	}
	for name, value := range required {
		if strings.TrimSpace(value) == "" {
			return eventapi.Envelope{}, fmt.Errorf("%w: %s is required", ErrInvalidEvent, name)
		}
	}
	if event.AggregateVersion == 0 {
		return eventapi.Envelope{}, fmt.Errorf("%w: aggregate_version must be positive", ErrInvalidEvent)
	}
	if event.SchemaVersion <= 0 {
		return eventapi.Envelope{}, fmt.Errorf("%w: schema_version must be positive", ErrInvalidEvent)
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	} else {
		event.OccurredAt = event.OccurredAt.UTC()
	}
	if len(event.Payload) == 0 {
		event.Payload = json.RawMessage(`{}`)
	}
	if !json.Valid(event.Payload) {
		return eventapi.Envelope{}, fmt.Errorf("%w: payload must be valid JSON", ErrInvalidEvent)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, event.Payload); err != nil {
		return eventapi.Envelope{}, fmt.Errorf("%w: compact payload: %v", ErrInvalidEvent, err)
	}
	event.Payload = append(json.RawMessage(nil), compact.Bytes()...)
	return event, nil
}

func equivalent(stored, candidate eventapi.Envelope, compareOccurredAt bool) bool {
	if stored.ID != candidate.ID ||
		stored.Type != candidate.Type ||
		stored.AggregateType != candidate.AggregateType ||
		stored.AggregateID != candidate.AggregateID ||
		stored.AggregateVersion != candidate.AggregateVersion ||
		stored.Producer != candidate.Producer ||
		stored.SchemaVersion != candidate.SchemaVersion ||
		stored.TraceID != candidate.TraceID ||
		stored.CausationID != candidate.CausationID ||
		stored.CorrelationID != candidate.CorrelationID ||
		!bytes.Equal(stored.Payload, candidate.Payload) {
		return false
	}
	return !compareOccurredAt || stored.OccurredAt.Equal(candidate.OccurredAt)
}

func validateLimit(limit int) error {
	if limit <= 0 || limit > MaxListLimit {
		return fmt.Errorf("limit must be between 1 and %d", MaxListLimit)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scan(row rowScanner) (eventapi.Envelope, error) {
	var (
		event            eventapi.Envelope
		sequence         int64
		aggregateVersion int64
		occurredAt       string
		payload          string
	)
	if err := row.Scan(
		&sequence,
		&event.ID,
		&event.Type,
		&event.AggregateType,
		&event.AggregateID,
		&aggregateVersion,
		&occurredAt,
		&event.Producer,
		&event.SchemaVersion,
		&event.TraceID,
		&event.CausationID,
		&event.CorrelationID,
		&payload,
	); err != nil {
		return eventapi.Envelope{}, err
	}
	parsedTime, err := time.Parse(time.RFC3339Nano, occurredAt)
	if err != nil {
		return eventapi.Envelope{}, fmt.Errorf("parse event time: %w", err)
	}
	event.Sequence = uint64(sequence)
	event.AggregateVersion = uint64(aggregateVersion)
	event.OccurredAt = parsedTime.UTC()
	event.Payload = json.RawMessage(payload)
	return event, nil
}

func collect(rows *sql.Rows) ([]eventapi.Envelope, error) {
	result := make([]eventapi.Envelope, 0)
	for rows.Next() {
		event, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}
	return result, nil
}
