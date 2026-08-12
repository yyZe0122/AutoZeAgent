// Package configreload watches the user config directory for provider config changes.
package configreload

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/yyZe0122/yunmengze-agent/internal/providerconfig"
)

// Watched basenames under ConfigDir (provider/model/key only).
var watchedNames = map[string]struct{}{
	providerconfig.Filename:      {},
	providerconfig.LocalFilename: {},
	providerconfig.EnvFilename:   {},
}

// Watcher debounces filesystem events and invokes OnChange.
type Watcher struct {
	configDir string
	debounce  time.Duration
	onChange  func()
	log       *slog.Logger

	mu       sync.Mutex
	timer    *time.Timer
	watcher  *fsnotify.Watcher
	stopOnce sync.Once

	// Coalesce: at most one OnChange in flight; mark dirty if events arrive during run.
	runMu   sync.Mutex
	dirty   bool
	running bool
}

// Options configures Watcher.
type Options struct {
	ConfigDir string
	// Debounce defaults to 500ms.
	Debounce time.Duration
	// OnChange is called after debounce (may run on a background goroutine).
	OnChange func()
	// Logger optional; defaults to slog.Default().
	Logger *slog.Logger
}

// New builds a Watcher. Call Start to begin watching.
func New(opts Options) (*Watcher, error) {
	dir := strings.TrimSpace(opts.ConfigDir)
	if dir == "" {
		return nil, errConfigDirRequired
	}
	if opts.OnChange == nil {
		return nil, errOnChangeRequired
	}
	debounce := opts.Debounce
	if debounce <= 0 {
		debounce = 500 * time.Millisecond
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Watcher{
		configDir: dir,
		debounce:  debounce,
		onChange:  opts.OnChange,
		log:       log,
	}, nil
}

// Start watches configDir until ctx is cancelled. Safe to call once.
func (w *Watcher) Start(ctx context.Context) error {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	w.watcher = fw
	if err := fw.Add(w.configDir); err != nil {
		_ = fw.Close()
		w.watcher = nil
		return err
	}
	go w.loop(ctx)
	return nil
}

// Close stops the watcher (idempotent). Prefer cancelling Start's context.
func (w *Watcher) Close() error {
	var err error
	w.stopOnce.Do(func() {
		w.mu.Lock()
		if w.timer != nil {
			w.timer.Stop()
			w.timer = nil
		}
		w.mu.Unlock()
		if w.watcher != nil {
			err = w.watcher.Close()
		}
	})
	return err
}

func (w *Watcher) loop(ctx context.Context) {
	defer func() { _ = w.Close() }()
	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.log.Warn("config watch error", "component", "configreload", "operation", "watch", "result", "warning", "error", err)
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if !isWatchedEvent(event) {
				continue
			}
			w.schedule()
		}
	}
}

func (w *Watcher) schedule() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timer = time.AfterFunc(w.debounce, func() {
		w.fire()
	})
}

func (w *Watcher) fire() {
	w.runMu.Lock()
	if w.running {
		w.dirty = true
		w.runMu.Unlock()
		return
	}
	w.running = true
	w.dirty = false
	w.runMu.Unlock()

	for {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					w.log.Error("config reload OnChange panic", "component", "configreload", "operation", "reload", "result", "failed", "panic", rec)
				}
			}()
			w.onChange()
		}()
		w.runMu.Lock()
		if !w.dirty {
			w.running = false
			w.runMu.Unlock()
			return
		}
		w.dirty = false
		w.runMu.Unlock()
	}
}

func isWatchedEvent(event fsnotify.Event) bool {
	base := filepath.Base(event.Name)
	if _, ok := watchedNames[base]; ok {
		return event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) || event.Has(fsnotify.Chmod)
	}
	// Editor / atomic save temps: agent.json.tmp, agent.local.json.swp, env~
	for name := range watchedNames {
		if base == name+".tmp" || base == name+"~" || base == "."+name+".swp" || base == name+".swp" {
			return true
		}
		// WriteSelectedModel temp: .yunmengze-model-*.tmp in same dir — ignore (runtime suppresses).
	}
	return false
}
