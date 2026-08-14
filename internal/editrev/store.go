// Package editrev records agent file-edit checkpoints for human rewind (ADR-051 / QG).
package editrev

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/yyZe0122/yunmengze-agent/internal/artifacts"
	"github.com/yyZe0122/yunmengze-agent/internal/runmeta"
)

// Revision is one agent write checkpoint.
type Revision struct {
	ID         string
	SessionID  string
	Path       string
	SHABefore  string
	SHAAfter   string
	ArtifactID string
	RunID      string
	CreatedAt  string
}

// Store snapshots bytes to the existing artifact store and rows in edit_revisions.
type Store struct {
	db        *sql.DB
	artifacts *artifacts.Store
	now       func() time.Time
}

func NewStore(db *sql.DB, arts *artifacts.Store) (*Store, error) {
	if db == nil || arts == nil {
		return nil, errors.New("editrev requires database and artifact store")
	}
	return &Store{db: db, artifacts: arts, now: func() time.Time { return time.Now().UTC() }}, nil
}

// SnapshotBeforeWrite stores old bytes (fail-closed). Skips symlinks. Empty/new file is ok (no artifact).
func (s *Store) SnapshotBeforeWrite(ctx context.Context, path string, before []byte, shaAfter string) error {
	if s == nil {
		return errors.New("editrev store is nil")
	}
	info, err := os.Lstat(path)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing to snapshot symlink")
	}
	sessionID, runID := "", ""
	if meta, ok := runmeta.From(ctx); ok {
		sessionID = strings.TrimSpace(meta.SessionID)
		runID = strings.TrimSpace(meta.RunID)
	}
	if sessionID == "" {
		return errors.New("session id is required for edit checkpoint")
	}
	shaBefore := ""
	artifactID := ""
	if len(before) > 0 {
		sum := sha256.Sum256(before)
		shaBefore = hex.EncodeToString(sum[:])
		ref, putErr := s.artifacts.Put(ctx, "application/octet-stream", before, map[string]any{
			"kind": "edit_revision", "path": path, "session_id": sessionID,
		})
		if putErr != nil {
			return fmt.Errorf("snapshot artifact: %w", putErr)
		}
		artifactID = ref.ID
	}
	id := "erev-" + sha256hex(sessionID, path, shaBefore, shaAfter, s.now().UTC().Format(time.RFC3339Nano))[:32]
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO edit_revisions (revision_id, session_id, path, sha_before, sha_after, artifact_id, run_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, sessionID, path, shaBefore, shaAfter, artifactID, runID, s.now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("insert edit revision: %w", err)
	}
	return nil
}

func (s *Store) Latest(ctx context.Context, sessionID string) (Revision, error) {
	return s.get(ctx, sessionID, "")
}

func (s *Store) Get(ctx context.Context, sessionID, revisionID string) (Revision, error) {
	return s.get(ctx, sessionID, revisionID)
}

func (s *Store) get(ctx context.Context, sessionID, revisionID string) (Revision, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return Revision{}, errors.New("session id is required")
	}
	var row Revision
	var err error
	if id := strings.TrimSpace(revisionID); id != "" {
		err = s.db.QueryRowContext(ctx, `
			SELECT revision_id, session_id, path, sha_before, sha_after, artifact_id, run_id, created_at
			FROM edit_revisions WHERE session_id = ? AND revision_id = ?`, sessionID, id).
			Scan(&row.ID, &row.SessionID, &row.Path, &row.SHABefore, &row.SHAAfter, &row.ArtifactID, &row.RunID, &row.CreatedAt)
	} else {
		err = s.db.QueryRowContext(ctx, `
			SELECT revision_id, session_id, path, sha_before, sha_after, artifact_id, run_id, created_at
			FROM edit_revisions WHERE session_id = ? ORDER BY created_at DESC LIMIT 1`, sessionID).
			Scan(&row.ID, &row.SessionID, &row.Path, &row.SHABefore, &row.SHAAfter, &row.ArtifactID, &row.RunID, &row.CreatedAt)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return Revision{}, fmt.Errorf("no edit revision")
	}
	if err != nil {
		return Revision{}, err
	}
	return row, nil
}

// Rewind restores the file to sha_before. Refuses if current disk hash != sha_after.
func (s *Store) Rewind(ctx context.Context, sessionID, revisionID string) (Revision, error) {
	rev, err := s.get(ctx, sessionID, revisionID)
	if err != nil {
		return Revision{}, err
	}
	current, err := os.ReadFile(rev.Path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Revision{}, err
	}
	if len(current) > 0 {
		sum := sha256.Sum256(current)
		if hex.EncodeToString(sum[:]) != rev.SHAAfter && rev.SHAAfter != "" {
			return Revision{}, fmt.Errorf("file changed since revision; refuse rewind")
		}
	}
	var old []byte
	if rev.ArtifactID != "" {
		old, err = s.artifacts.Get(ctx, rev.ArtifactID)
		if err != nil {
			return Revision{}, err
		}
	}
	if err := os.WriteFile(rev.Path, old, 0o640); err != nil {
		return Revision{}, err
	}
	return rev, nil
}

func sha256hex(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
