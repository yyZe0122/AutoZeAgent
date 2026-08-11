package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	coresqlite "github.com/yyZe0122/yunmengze-agent/internal/store/sqlite"
	"github.com/yyZe0122/yunmengze-agent/pkg/eventapi"
)

func TestAppendListAndAggregateOrdering(t *testing.T) {
	t.Parallel()

	db, store := openStore(t)
	defer db.Close()

	first := sampleEvent("event-1", 1)
	first.Payload = nil
	storedFirst, err := store.Append(context.Background(), first)
	if err != nil {
		t.Fatalf("Append(first) error = %v", err)
	}
	if storedFirst.Sequence != 1 {
		t.Errorf("first sequence = %d, want 1", storedFirst.Sequence)
	}
	if string(storedFirst.Payload) != `{}` {
		t.Errorf("empty payload stored as %s, want {}", storedFirst.Payload)
	}

	second := sampleEvent("event-2", 2)
	second.Payload = json.RawMessage(`{ "value": 2 }`)
	storedSecond, err := store.Append(context.Background(), second)
	if err != nil {
		t.Fatalf("Append(second) error = %v", err)
	}
	if storedSecond.Sequence != 2 {
		t.Errorf("second sequence = %d, want 2", storedSecond.Sequence)
	}
	if string(storedSecond.Payload) != `{"value":2}` {
		t.Errorf("payload = %s, want compact JSON", storedSecond.Payload)
	}

	afterFirst, err := store.ListAfter(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("ListAfter() error = %v", err)
	}
	if len(afterFirst) != 1 || afterFirst[0].ID != "event-2" {
		t.Fatalf("ListAfter() = %+v, want event-2", afterFirst)
	}

	aggregate, err := store.ListAggregate(context.Background(), "task", "task-1", 0, 10)
	if err != nil {
		t.Fatalf("ListAggregate() error = %v", err)
	}
	if len(aggregate) != 2 {
		t.Fatalf("aggregate length = %d, want 2", len(aggregate))
	}
	if aggregate[0].AggregateVersion != 1 || aggregate[1].AggregateVersion != 2 {
		t.Fatalf("aggregate versions = %d, %d; want 1, 2",
			aggregate[0].AggregateVersion, aggregate[1].AggregateVersion)
	}
}

func TestAppendIsIdempotentByEventID(t *testing.T) {
	t.Parallel()

	db, store := openStore(t)
	defer db.Close()

	input := sampleEvent("event-idempotent", 1)
	first, err := store.Append(context.Background(), input)
	if err != nil {
		t.Fatalf("first Append() error = %v", err)
	}
	second, err := store.Append(context.Background(), input)
	if err != nil {
		t.Fatalf("second Append() error = %v", err)
	}
	if second.Sequence != first.Sequence || !second.OccurredAt.Equal(first.OccurredAt) {
		t.Fatalf("idempotent retry = %+v, want first event %+v", second, first)
	}

	conflict := input
	conflict.Type = "task.changed"
	if _, err := store.Append(context.Background(), conflict); !errors.Is(err, ErrEventIDConflict) {
		t.Fatalf("conflicting Append() error = %v, want ErrEventIDConflict", err)
	}

	var count int
	if err := db.SQL().QueryRow("SELECT COUNT(*) FROM events").Scan(&count); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != 1 {
		t.Fatalf("event count = %d, want 1", count)
	}
}

func TestEventsAreDatabaseEnforcedAppendOnly(t *testing.T) {
	t.Parallel()

	db, store := openStore(t)
	defer db.Close()
	stored, err := store.Append(context.Background(), sampleEvent("event-immutable", 1))
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	if _, err := db.SQL().Exec("UPDATE events SET producer = 'other' WHERE sequence = ?", stored.Sequence); err == nil {
		t.Fatal("UPDATE events error = nil, want append-only trigger rejection")
	}
	if _, err := db.SQL().Exec("DELETE FROM events WHERE sequence = ?", stored.Sequence); err == nil {
		t.Fatal("DELETE events error = nil, want append-only trigger rejection")
	}

	listed, err := store.ListAfter(context.Background(), 0, 10)
	if err != nil {
		t.Fatalf("ListAfter() error = %v", err)
	}
	if len(listed) != 1 || listed[0].Producer != "test" {
		t.Fatalf("stored event changed: %+v", listed)
	}
}

func TestEventPersistsAcrossDatabaseRestart(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "core.db")
	firstDB, err := coresqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("first database Open() error = %v", err)
	}
	firstStore, err := NewStore(firstDB.SQL())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if _, err := firstStore.Append(context.Background(), sampleEvent("event-persistent", 1)); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if err := firstDB.Close(); err != nil {
		t.Fatalf("first database Close() error = %v", err)
	}

	secondDB, err := coresqlite.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("second database Open() error = %v", err)
	}
	defer secondDB.Close()
	secondStore, err := NewStore(secondDB.SQL())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	listed, err := secondStore.ListAfter(context.Background(), 0, 10)
	if err != nil {
		t.Fatalf("ListAfter() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "event-persistent" {
		t.Fatalf("events after restart = %+v, want event-persistent", listed)
	}
}

func TestStoreValidatesInputAndContext(t *testing.T) {
	t.Parallel()

	db, store := openStore(t)
	defer db.Close()

	invalid := sampleEvent("event-invalid", 1)
	invalid.Payload = json.RawMessage(`{"broken"`)
	if _, err := store.Append(context.Background(), invalid); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("invalid JSON error = %v, want ErrInvalidEvent", err)
	}
	if _, err := store.ListAfter(context.Background(), 0, 0); err == nil {
		t.Fatal("ListAfter(limit=0) error = nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Append(ctx, sampleEvent("event-cancelled", 1)); err == nil {
		t.Fatal("Append(cancelled context) error = nil")
	}
	if _, err := store.ListAfter(ctx, 0, 10); err == nil {
		t.Fatal("ListAfter(cancelled context) error = nil")
	}
}

func openStore(t *testing.T) (*coresqlite.DB, *Store) {
	t.Helper()
	db, err := coresqlite.Open(context.Background(), filepath.Join(t.TempDir(), "core.db"))
	if err != nil {
		t.Fatalf("database Open() error = %v", err)
	}
	store, err := NewStore(db.SQL())
	if err != nil {
		db.Close()
		t.Fatalf("NewStore() error = %v", err)
	}
	return db, store
}

func sampleEvent(id string, aggregateVersion uint64) eventapi.Envelope {
	return eventapi.Envelope{
		ID:               id,
		Type:             "task.created",
		AggregateType:    "task",
		AggregateID:      "task-1",
		AggregateVersion: aggregateVersion,
		OccurredAt:       time.Time{},
		Producer:         "test",
		SchemaVersion:    1,
		TraceID:          "trace-1",
		CorrelationID:    "correlation-1",
		Payload:          json.RawMessage(`{"value":1}`),
	}
}

func TestListNormalizesStoredEventTimeToUTC(t *testing.T) {
	t.Parallel()

	db, store := openStore(t)
	defer db.Close()
	ctx := context.Background()
	event := sampleEvent("event-offset-time", 1)
	offsetTime := "2026-07-16T18:00:00.123456789+08:00"
	if _, err := db.SQL().ExecContext(ctx, `
        INSERT INTO events (
            event_id, event_type, aggregate_type, aggregate_id, aggregate_version,
            occurred_at, producer, schema_version, trace_id, causation_id, correlation_id, payload
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.Type, event.AggregateType, event.AggregateID, event.AggregateVersion,
		offsetTime, event.Producer, event.SchemaVersion, event.TraceID, event.CausationID, event.CorrelationID, string(event.Payload),
	); err != nil {
		t.Fatal(err)
	}

	listed, err := store.ListAfter(ctx, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("listed events = %d, want 1", len(listed))
	}
	want := time.Date(2026, time.July, 16, 10, 0, 0, 123456789, time.UTC)
	if listed[0].OccurredAt.Location() != time.UTC || !listed[0].OccurredAt.Equal(want) {
		t.Fatalf("occurred at = %v (%v), want %v UTC", listed[0].OccurredAt, listed[0].OccurredAt.Location(), want)
	}
}

func TestConcurrentAppendsReceiveContiguousGlobalSequences(t *testing.T) {
	t.Parallel()

	db, store := openStore(t)
	defer db.Close()

	const count = 32
	var wait sync.WaitGroup
	errorsSeen := make(chan error, count)
	for index := 0; index < count; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			event := sampleEvent(fmt.Sprintf("concurrent-event-%02d", index), 1)
			event.AggregateID = fmt.Sprintf("task-%02d", index)
			if _, err := store.Append(context.Background(), event); err != nil {
				errorsSeen <- err
			}
		}(index)
	}
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		t.Errorf("concurrent Append() error = %v", err)
	}

	listed, err := store.ListAfter(context.Background(), 0, count)
	if err != nil {
		t.Fatalf("ListAfter() error = %v", err)
	}
	if len(listed) != count {
		t.Fatalf("event count = %d, want %d", len(listed), count)
	}
	for index, event := range listed {
		want := uint64(index + 1)
		if event.Sequence != want {
			t.Fatalf("event[%d].Sequence = %d, want %d", index, event.Sequence, want)
		}
	}
}

func TestAppendTxFollowsCallerCommitAndRollback(t *testing.T) {
	t.Parallel()

	db, store := openStore(t)
	defer db.Close()
	ctx := context.Background()
	event := sampleEvent("transaction-event", 1)

	rolledBack, err := db.SQL().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	if _, err := store.AppendTx(ctx, rolledBack, event); err != nil {
		t.Fatalf("AppendTx() error = %v", err)
	}
	if err := rolledBack.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	listed, err := store.ListAfter(ctx, 0, 10)
	if err != nil {
		t.Fatalf("ListAfter() error = %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("rolled back event persisted: %+v", listed)
	}

	committed, err := db.SQL().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	if _, err := store.AppendTx(ctx, committed, event); err != nil {
		t.Fatalf("AppendTx() error = %v", err)
	}
	if err := committed.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	listed, err = store.ListAfter(ctx, 0, 10)
	if err != nil {
		t.Fatalf("ListAfter() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != event.ID {
		t.Fatalf("committed events = %+v, want transaction-event", listed)
	}
}
