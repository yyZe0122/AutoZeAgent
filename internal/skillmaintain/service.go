package skillmaintain

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/yyZe0122/yunmengze-agent/internal/skillcatalog"
)

// Service applies/rejects drafts and records usage (ADR-050).
type Service struct {
	store     *Store
	catalog   *skillcatalog.Catalog
	unusedTTL time.Duration
	now       func() time.Time
}

type Config struct {
	Store     *Store
	Catalog   *skillcatalog.Catalog
	UnusedTTL time.Duration
	Now       func() time.Time
}

func New(cfg Config) (*Service, error) {
	if cfg.Store == nil {
		return nil, errors.New("skillmaintain store is required")
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: cfg.Store, catalog: cfg.Catalog, unusedTTL: cfg.UnusedTTL, now: cfg.Now}, nil
}

// Maintain soft-archives unused skills that have a last_used_at older than unused_ttl.
func (s *Service) Maintain(ctx context.Context) {
	if s == nil || s.unusedTTL <= 0 {
		return
	}
	now := s.now().UTC()
	nowRFC := now.Format(time.RFC3339Nano)
	cutoff := now.Add(-s.unusedTTL).Format(time.RFC3339Nano)
	ids, err := s.store.ArchiveExpired(ctx, cutoff, nowRFC)
	if err != nil {
		slog.Warn("skill maintain archive failed", "component", "skillmaintain", "operation", "maintain", "result", "failed", "error", err)
		return
	}
	for _, id := range ids {
		if err := s.store.InsertEvent(ctx, Event{
			SkillID: id, Action: ActionArchive, Actor: "system", CreatedAt: nowRFC,
		}); err != nil {
			slog.Warn("skill archive event failed", "component", "skillmaintain", "operation", "maintain", "result", "failed", "skill_id", id, "error", err)
		}
	}
	if n := len(ids); n > 0 {
		slog.Info("skill unused archived", "component", "skillmaintain", "operation", "maintain", "result", "succeeded", "count", n)
	}
}

// RecordUsed upserts last_used and writes a used event. Also unarchives.
func (s *Service) RecordUsed(ctx context.Context, skillIDs []string, actor string) error {
	if s == nil {
		return nil
	}
	now := s.now().UTC().Format(time.RFC3339Nano)
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "user"
	}
	for _, id := range skillIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if err := s.store.RecordUsed(ctx, id, now); err != nil {
			return err
		}
		if err := s.store.InsertEvent(ctx, Event{
			SkillID: id, Action: ActionUsed, Actor: actor, CreatedAt: now,
		}); err != nil {
			return err
		}
	}
	s.Maintain(ctx)
	return nil
}

// RecordDraft logs a draft/discard after the catalog file write.
func (s *Service) RecordDraft(ctx context.Context, skillID, actor, path, hash string, discarded bool) error {
	if s == nil {
		return nil
	}
	action := ActionDraft
	if discarded {
		action = ActionReject
	}
	return s.store.InsertEvent(ctx, Event{
		SkillID: skillID, Action: action, Actor: actor, Path: displayPath(path),
		ContentHash: hash, CreatedAt: s.now().UTC().Format(time.RFC3339Nano),
	})
}

// ApplyDraft promotes SKILL.md.draft → SKILL.md with backup.
func (s *Service) ApplyDraft(ctx context.Context, skillID, actor string) (skillcatalog.Skill, error) {
	if s == nil || s.catalog == nil {
		return skillcatalog.Skill{}, errors.New("skill catalog is unavailable")
	}
	sk, backup, hash, err := s.catalog.ApplyDraft(skillID, s.now())
	if err != nil {
		return skillcatalog.Skill{}, err
	}
	_ = s.store.InsertEvent(ctx, Event{
		SkillID: sk.ID, Action: ActionApply, Actor: actorOrUser(actor),
		Path: displayPath(backup), ContentHash: hash,
		CreatedAt: s.now().UTC().Format(time.RFC3339Nano),
	})
	return sk, nil
}

// RejectDraft discards SKILL.md.draft.
func (s *Service) RejectDraft(ctx context.Context, skillID, actor string) error {
	if s == nil || s.catalog == nil {
		return errors.New("skill catalog is unavailable")
	}
	sk, err := s.catalog.DiscardDraft(skillID)
	if err != nil {
		return err
	}
	return s.store.InsertEvent(ctx, Event{
		SkillID: sk.ID, Action: ActionReject, Actor: actorOrUser(actor),
		Path: displayPath(sk.DraftPath()), CreatedAt: s.now().UTC().Format(time.RFC3339Nano),
	})
}

func (s *Service) UsageMap(ctx context.Context) (map[string]Usage, error) {
	if s == nil {
		return map[string]Usage{}, nil
	}
	list, err := s.store.ListUsage(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]Usage, len(list))
	for _, u := range list {
		out[u.SkillID] = u
	}
	return out, nil
}

func actorOrUser(actor string) string {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return "user"
	}
	return actor
}

func displayPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	return filepath.Base(filepath.Dir(p)) + "/" + filepath.Base(p)
}

// ArchivedSkillIDs returns skill ids with a non-empty archived_at.
func (s *Service) ArchivedSkillIDs(ctx context.Context) map[string]struct{} {
	out := map[string]struct{}{}
	if s == nil {
		return out
	}
	list, err := s.store.ListUsage(ctx)
	if err != nil {
		return out
	}
	for _, u := range list {
		if strings.TrimSpace(u.ArchivedAt) != "" {
			out[u.SkillID] = struct{}{}
		}
	}
	return out
}

func (s *Service) Store() *Store {
	if s == nil {
		return nil
	}
	return s.store
}
