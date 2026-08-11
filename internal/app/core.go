package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/yyZe0122/yunmengze-agent/internal/platform/paths"
)

type State string

const StateHealthy State = "healthy"

type BackgroundRunner interface {
	Run(context.Context)
}

type Status struct {
	Name      string       `json:"name"`
	Version   string       `json:"version"`
	State     State        `json:"state"`
	StartedAt time.Time    `json:"started_at"`
	Runtime   paths.Layout `json:"runtime"`
}

type Config struct {
	Name              string
	Version           string
	Runtime           paths.Layout
	BackgroundRunners []BackgroundRunner
}

type Core struct {
	name              string
	version           string
	runtime           paths.Layout
	backgroundRunners []BackgroundRunner
	runMu             sync.Mutex
	runStarted        bool
	startedAt         time.Time
}

func New(config Config) (*Core, error) {
	if config.Name == "" {
		config.Name = "yunmengze"
	}
	if config.Version == "" {
		config.Version = "unknown"
	}
	if err := config.Runtime.Validate(); err != nil {
		return nil, err
	}
	return &Core{
		name:              config.Name,
		version:           config.Version,
		runtime:           config.Runtime,
		backgroundRunners: append([]BackgroundRunner(nil), config.BackgroundRunners...),
		startedAt:         time.Now().UTC(),
	}, nil
}

func (c *Core) Status() Status {
	return Status{
		Name:      c.name,
		Version:   c.version,
		State:     StateHealthy,
		StartedAt: c.startedAt,
		Runtime:   c.runtime,
	}
}

func (c *Core) AddBackgroundRunner(runner BackgroundRunner) error {
	if runner == nil {
		return errors.New("background runner is required")
	}
	c.runMu.Lock()
	defer c.runMu.Unlock()
	if c.runStarted {
		return errors.New("background runners cannot be added after Core starts")
	}
	c.backgroundRunners = append(c.backgroundRunners, runner)
	return nil
}

func (c *Core) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("run context is required")
	}
	c.runMu.Lock()
	if c.runStarted {
		c.runMu.Unlock()
		return errors.New("Core can only run once")
	}
	c.runStarted = true
	backgroundRunners := append([]BackgroundRunner(nil), c.backgroundRunners...)
	c.runMu.Unlock()

	var runners sync.WaitGroup
	for _, runner := range backgroundRunners {
		if runner == nil {
			continue
		}
		runners.Add(1)
		go func(runner BackgroundRunner) {
			defer runners.Done()
			runner.Run(ctx)
		}(runner)
	}
	<-ctx.Done()
	runners.Wait()
	return nil
}
