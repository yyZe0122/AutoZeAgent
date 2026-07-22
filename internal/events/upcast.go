package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"autozeagent.local/autozeagent/pkg/eventapi"
)

var (
	ErrInvalidSchemaVersion = errors.New("invalid event schema version")
	ErrDuplicateUpcaster    = errors.New("event upcaster already registered")
	ErrMissingUpcaster      = errors.New("event upcaster chain is incomplete")
	ErrFutureSchema         = errors.New("event schema is newer than the configured version")
)

type Upcaster func(context.Context, json.RawMessage) (json.RawMessage, error)

type upcasterKey struct {
	eventType   string
	fromVersion int
}

// UpcasterRegistry converts historical payloads at read time. Stored events
// remain immutable and retain their original schema_version and payload.
type UpcasterRegistry struct {
	mu       sync.RWMutex
	current  map[string]int
	upcaster map[upcasterKey]Upcaster
}

func NewUpcasterRegistry() *UpcasterRegistry {
	return &UpcasterRegistry{
		current:  make(map[string]int),
		upcaster: make(map[upcasterKey]Upcaster),
	}
}

func (r *UpcasterRegistry) SetCurrentVersion(eventType string, version int) error {
	if eventType == "" || version <= 0 {
		return ErrInvalidSchemaVersion
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing := r.current[eventType]; existing > version {
		return fmt.Errorf("%w: %s cannot decrease from %d to %d", ErrInvalidSchemaVersion, eventType, existing, version)
	}
	r.current[eventType] = version
	return nil
}

// Register adds one adjacent migration step: fromVersion -> fromVersion+1.
func (r *UpcasterRegistry) Register(eventType string, fromVersion int, upcaster Upcaster) error {
	if eventType == "" || fromVersion <= 0 || upcaster == nil {
		return ErrInvalidSchemaVersion
	}
	key := upcasterKey{eventType: eventType, fromVersion: fromVersion}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.upcaster[key]; exists {
		return fmt.Errorf("%w: %s v%d", ErrDuplicateUpcaster, eventType, fromVersion)
	}
	r.upcaster[key] = upcaster
	return nil
}

func (r *UpcasterRegistry) Upcast(ctx context.Context, event eventapi.Envelope) (eventapi.Envelope, error) {
	if ctx == nil {
		return eventapi.Envelope{}, errors.New("upcast context is required")
	}
	r.mu.RLock()
	target, configured := r.current[event.Type]
	r.mu.RUnlock()
	if !configured {
		return cloneEnvelope(event), nil
	}
	if event.SchemaVersion <= 0 {
		return eventapi.Envelope{}, ErrInvalidSchemaVersion
	}
	if event.SchemaVersion > target {
		return eventapi.Envelope{}, fmt.Errorf("%w: %s has v%d, configured v%d", ErrFutureSchema, event.Type, event.SchemaVersion, target)
	}

	result := cloneEnvelope(event)
	for result.SchemaVersion < target {
		key := upcasterKey{eventType: result.Type, fromVersion: result.SchemaVersion}
		r.mu.RLock()
		upcaster := r.upcaster[key]
		r.mu.RUnlock()
		if upcaster == nil {
			return eventapi.Envelope{}, fmt.Errorf("%w: %s v%d -> v%d", ErrMissingUpcaster, result.Type, result.SchemaVersion, result.SchemaVersion+1)
		}
		payload, err := upcaster(ctx, append(json.RawMessage(nil), result.Payload...))
		if err != nil {
			return eventapi.Envelope{}, fmt.Errorf("upcast %s v%d: %w", result.Type, result.SchemaVersion, err)
		}
		if !json.Valid(payload) {
			return eventapi.Envelope{}, fmt.Errorf("upcast %s v%d: payload is not valid JSON", result.Type, result.SchemaVersion)
		}
		result.Payload = append(json.RawMessage(nil), payload...)
		result.SchemaVersion++
	}
	return result, nil
}

func cloneEnvelope(event eventapi.Envelope) eventapi.Envelope {
	event.Payload = append(json.RawMessage(nil), event.Payload...)
	return event
}
