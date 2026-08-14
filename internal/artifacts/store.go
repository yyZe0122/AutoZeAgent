// Package artifacts stores large Core outputs outside SQLite while retaining
// content-addressed metadata in core.db.
package artifacts

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yyZe0122/yunmengze-agent/pkg/toolapi"
)

type Store struct {
	db   *sql.DB
	root string
}

func NewStore(db *sql.DB, root string) (*Store, error) {
	if db == nil {
		return nil, errors.New("artifact database is required")
	}
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("artifact root is required")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve artifact root: %w", err)
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("create artifact root: %w", err)
	}
	return &Store{db: db, root: root}, nil
}

func (s *Store) Put(ctx context.Context, mediaType string, content []byte, metadata map[string]any) (toolapi.ArtifactRef, error) {
	if ctx == nil {
		return toolapi.ArtifactRef{}, errors.New("artifact context is required")
	}
	mediaType = strings.TrimSpace(mediaType)
	if mediaType == "" {
		return toolapi.ArtifactRef{}, errors.New("artifact media type is required")
	}
	digest := sha256.Sum256(content)
	hash := hex.EncodeToString(digest[:])
	id := "sha256:" + hash
	directory := filepath.Join(s.root, hash[:2])
	path := filepath.Join(directory, hash)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return toolapi.ArtifactRef{}, fmt.Errorf("create artifact directory: %w", err)
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		temporary, err := os.CreateTemp(directory, hash+"-*.tmp")
		if err != nil {
			return toolapi.ArtifactRef{}, fmt.Errorf("create artifact temporary file: %w", err)
		}
		temporaryPath := temporary.Name()
		defer os.Remove(temporaryPath)
		if err := temporary.Chmod(0o640); err != nil {
			_ = temporary.Close()
			return toolapi.ArtifactRef{}, fmt.Errorf("set artifact permissions: %w", err)
		}
		if _, err := temporary.Write(content); err != nil {
			_ = temporary.Close()
			return toolapi.ArtifactRef{}, fmt.Errorf("write artifact: %w", err)
		}
		if err := temporary.Sync(); err != nil {
			_ = temporary.Close()
			return toolapi.ArtifactRef{}, fmt.Errorf("sync artifact: %w", err)
		}
		if err := temporary.Close(); err != nil {
			return toolapi.ArtifactRef{}, fmt.Errorf("close artifact: %w", err)
		}
		if err := os.Rename(temporaryPath, path); err != nil && !errors.Is(err, os.ErrExist) {
			return toolapi.ArtifactRef{}, fmt.Errorf("publish artifact: %w", err)
		}
	} else if err != nil {
		return toolapi.ArtifactRef{}, fmt.Errorf("stat artifact: %w", err)
	}
	encodedMetadata, err := json.Marshal(metadata)
	if err != nil {
		return toolapi.ArtifactRef{}, fmt.Errorf("marshal artifact metadata: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO artifacts (artifact_id, content_hash, media_type, size_bytes, storage_path, created_at, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(content_hash) DO NOTHING`,
		id, hash, mediaType, len(content), path, time.Now().UTC().Format(time.RFC3339Nano), string(encodedMetadata),
	)
	if err != nil {
		return toolapi.ArtifactRef{}, fmt.Errorf("record artifact: %w", err)
	}
	return toolapi.ArtifactRef{ID: id, ContentHash: hash, MediaType: mediaType, SizeBytes: int64(len(content))}, nil
}

// Get reads artifact bytes by id (sha256:… or bare hash).
func (s *Store) Get(ctx context.Context, id string) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("artifact context is required")
	}
	id = strings.TrimSpace(id)
	hash := strings.TrimPrefix(id, "sha256:")
	if hash == "" {
		return nil, errors.New("artifact id is required")
	}
	var path string
	err := s.db.QueryRowContext(ctx, `SELECT storage_path FROM artifacts WHERE artifact_id = ? OR content_hash = ?`, id, hash).Scan(&path)
	if errors.Is(err, sql.ErrNoRows) {
		prefix := hash
		if len(prefix) > 2 {
			prefix = prefix[:2]
		}
		path = filepath.Join(s.root, prefix, hash)
	} else if err != nil {
		return nil, fmt.Errorf("lookup artifact: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read artifact: %w", err)
	}
	return data, nil
}
