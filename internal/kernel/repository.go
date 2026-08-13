package kernel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/yyZe0122/yunmengze-agent/internal/events"
	"github.com/yyZe0122/yunmengze-agent/pkg/eventapi"
)

var (
	ErrAlreadyExists = errors.New("aggregate already exists")
	ErrSessionClosed = errors.New("session is closed")
)

type Repository struct {
	db     *sql.DB
	events *events.Store
}

func NewRepository(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("kernel database is required")
	}
	eventStore, err := events.NewStore(db)
	if err != nil {
		return nil, err
	}
	return &Repository{db: db, events: eventStore}, nil
}

type scanner interface {
	Scan(...any) error
}

func aggregateEvent(
	aggregateType string,
	aggregateID string,
	version uint64,
	eventType string,
	occurredAt time.Time,
	payload any,
) (eventapi.Envelope, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return eventapi.Envelope{}, fmt.Errorf("marshal kernel event payload: %w", err)
	}
	return eventapi.Envelope{
		ID:               fmt.Sprintf("kernel/%s/%s/v/%d", aggregateType, aggregateID, version),
		Type:             eventType,
		AggregateType:    aggregateType,
		AggregateID:      aggregateID,
		AggregateVersion: version,
		OccurredAt:       occurredAt,
		Producer:         "kernel",
		SchemaVersion:    1,
		Payload:          encoded,
	}, nil
}

func versionConflict(aggregate, id string, expected, actual uint64) error {
	return fmt.Errorf("%w: %s %s expected %d, actual %d", ErrVersionConflict, aggregate, id, expected, actual)
}

func requireOneVersionedRow(result sql.Result, aggregate, id string, expected uint64) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read updated row count: %w", err)
	}
	if affected != 1 {
		return versionConflict(aggregate, id, expected, 0)
	}
	return nil
}

func existsByID(ctx context.Context, tx *sql.Tx, table, column, id string) bool {
	allowed := map[string]string{
		"sessions": "session_id",
		"tasks":    "task_id",
	}
	if allowed[table] != column {
		return false
	}
	var count int
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s = ?", table, column)
	return tx.QueryRowContext(ctx, query, id).Scan(&count) == nil && count > 0
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse aggregate time: %w", err)
	}
	return parsed.UTC(), nil
}
