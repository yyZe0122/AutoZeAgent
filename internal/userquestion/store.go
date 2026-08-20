// Package userquestion implements model-facing ask_user questions (ADR-052 R4).
package userquestion

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	StatePending     = "pending"
	StateAnswered    = "answered"
	StateUnavailable = "unavailable"
	StateCancelled   = "cancelled"
)

var (
	ErrNotFound   = errors.New("user question not found")
	ErrNotPending = errors.New("user question is not pending")
	ErrInvalid    = errors.New("invalid user question")
)

type Option struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type Item struct {
	ID          string   `json:"id"`
	Question    string   `json:"question"`
	Header      string   `json:"header,omitempty"`
	Options     []Option `json:"options,omitempty"`
	MultiSelect bool     `json:"multi_select,omitempty"`
}

type Request struct {
	ID         string              `json:"question_id"`
	SessionID  string              `json:"session_id"`
	TaskID     string              `json:"task_id"`
	RunID      string              `json:"run_id"`
	ToolCallID string              `json:"tool_call_id"`
	Questions  []Item              `json:"questions"`
	State      string              `json:"state"`
	Answers    map[string][]string `json:"answers,omitempty"`
	DecidedBy  string              `json:"decided_by,omitempty"`
	CreatedAt  string              `json:"created_at"`
	DecidedAt  string              `json:"decided_at,omitempty"`
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("userquestion store requires database")
	}
	return &Store{db: db}, nil
}

func (s *Store) Insert(ctx context.Context, r Request) error {
	if s == nil || s.db == nil {
		return errors.New("userquestion store is nil")
	}
	if strings.TrimSpace(r.ID) == "" || len(r.Questions) == 0 {
		return fmt.Errorf("%w: question id and questions are required", ErrInvalid)
	}
	if r.State == "" {
		r.State = StatePending
	}
	qJSON, err := json.Marshal(r.Questions)
	if err != nil {
		return err
	}
	created := strings.TrimSpace(r.CreatedAt)
	if created == "" {
		created = time.Now().UTC().Format(time.RFC3339Nano)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO user_questions (
			question_id, session_id, task_id, run_id, tool_call_id,
			questions_json, state, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.SessionID, r.TaskID, r.RunID, r.ToolCallID,
		string(qJSON), r.State, created,
	)
	if err != nil {
		return fmt.Errorf("insert user question: %w", err)
	}
	return nil
}

func (s *Store) Get(ctx context.Context, id string) (Request, error) {
	return s.scanOne(ctx, `
		SELECT question_id, session_id, task_id, run_id, tool_call_id,
			questions_json, state, answers_json, decided_by, created_at, decided_at
		FROM user_questions WHERE question_id = ?`, strings.TrimSpace(id))
}

func (s *Store) ListPending(ctx context.Context, sessionID string, limit int) ([]Request, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	sessionID = strings.TrimSpace(sessionID)
	var rows *sql.Rows
	var err error
	if sessionID == "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT question_id, session_id, task_id, run_id, tool_call_id,
				questions_json, state, answers_json, decided_by, created_at, decided_at
			FROM user_questions WHERE state = ?
			ORDER BY created_at ASC LIMIT ?`, StatePending, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT question_id, session_id, task_id, run_id, tool_call_id,
				questions_json, state, answers_json, decided_by, created_at, decided_at
			FROM user_questions WHERE state = ? AND session_id = ?
			ORDER BY created_at ASC LIMIT ?`, StatePending, sessionID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("list pending user questions: %w", err)
	}
	defer rows.Close()
	var out []Request
	for rows.Next() {
		r, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) MarkDecided(ctx context.Context, id, state, decidedBy, decidedAt string, answers map[string][]string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: question id is required", ErrInvalid)
	}
	aJSON, err := json.Marshal(answers)
	if err != nil {
		return err
	}
	if answers == nil {
		aJSON = []byte("{}")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE user_questions
		SET state = ?, answers_json = ?, decided_by = ?, decided_at = ?
		WHERE question_id = ? AND state = ?`,
		state, string(aJSON), decidedBy, decidedAt, id, StatePending,
	)
	if err != nil {
		return fmt.Errorf("mark user question decided: %w", err)
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ErrNotPending
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Store) scanOne(ctx context.Context, query string, args ...any) (Request, error) {
	row := s.db.QueryRowContext(ctx, query, args...)
	r, err := scanRequest(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Request{}, ErrNotFound
	}
	return r, err
}

func scanRequest(row rowScanner) (Request, error) {
	var r Request
	var qJSON, aJSON string
	var decidedAt sql.NullString
	if err := row.Scan(
		&r.ID, &r.SessionID, &r.TaskID, &r.RunID, &r.ToolCallID,
		&qJSON, &r.State, &aJSON, &r.DecidedBy, &r.CreatedAt, &decidedAt,
	); err != nil {
		return Request{}, err
	}
	if qJSON != "" {
		_ = json.Unmarshal([]byte(qJSON), &r.Questions)
	}
	if aJSON != "" && aJSON != "{}" {
		_ = json.Unmarshal([]byte(aJSON), &r.Answers)
	}
	if decidedAt.Valid {
		r.DecidedAt = decidedAt.String
	}
	return r, nil
}
