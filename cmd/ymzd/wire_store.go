package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/yyZe0122/yunmengze-agent/internal/approval"
	"github.com/yyZe0122/yunmengze-agent/internal/artifacts"
	"github.com/yyZe0122/yunmengze-agent/internal/events"
	"github.com/yyZe0122/yunmengze-agent/internal/kernel"
	coresqlite "github.com/yyZe0122/yunmengze-agent/internal/store/sqlite"
)

type coreStores struct {
	database           *coresqlite.DB
	eventStore         *events.Store
	kernelRepository   *kernel.Repository
	approvalRepository *approval.Repository
	artifactStore      *artifacts.Store
}

func openCoreStores(ctx context.Context, dataDir string) (coreStores, error) {
	var out coreStores
	database, err := coresqlite.Open(ctx, filepath.Join(dataDir, "core.db"))
	if err != nil {
		return out, err
	}
	out.database = database
	out.eventStore, err = events.NewStore(database.SQL())
	if err != nil {
		_ = database.Close()
		return coreStores{}, err
	}
	out.kernelRepository, err = kernel.NewRepository(database.SQL())
	if err != nil {
		_ = database.Close()
		return coreStores{}, err
	}
	out.approvalRepository, err = approval.NewRepository(database.SQL())
	if err != nil {
		_ = database.Close()
		return coreStores{}, err
	}
	out.artifactStore, err = artifacts.NewStore(database.SQL(), filepath.Join(dataDir, "artifacts"))
	if err != nil {
		_ = database.Close()
		return coreStores{}, fmt.Errorf("open artifact store: %w", err)
	}
	return out, nil
}
