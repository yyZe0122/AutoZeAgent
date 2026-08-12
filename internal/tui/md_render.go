package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
)

const (
	mdMaxRunes   = 12000
	mdCacheLimit = 64
)

type mdCacheEntry struct {
	key string
	out string
}

var (
	mdMu    sync.Mutex
	mdCache []mdCacheEntry
)

// renderMarkdown renders completed assistant reply markdown for the terminal.
// Live/streaming content must not call this (too expensive / flickery).
// Plain short replies without markdown markers stay plain (no glamour padding).
func renderMarkdown(src string, width int, theme ThemeName) string {
	src = strings.TrimRight(src, "\n")
	if src == "" {
		return ""
	}
	if width < 20 {
		width = 40
	}
	// Soft cap: huge dumps stay plain.
	if len([]rune(src)) > mdMaxRunes {
		return src
	}
	if !looksLikeMarkdown(src) {
		return src
	}
	styleName := styles.DarkStyle
	if theme == ThemeDay {
		styleName = styles.LightStyle
	}
	key := mdCacheKey(src, width, string(theme))
	if out, ok := mdCacheGet(key); ok {
		return out
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(styleName),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return src
	}
	out, err := r.Render(src)
	if err != nil {
		return src
	}
	out = strings.TrimRight(out, "\n")
	mdCachePut(key, out)
	return out
}

func looksLikeMarkdown(s string) bool {
	if strings.Contains(s, "```") || strings.Contains(s, "`") {
		return true
	}
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "#") || strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") ||
			strings.HasPrefix(t, "> ") || strings.HasPrefix(t, "1. ") ||
			strings.HasPrefix(t, "[") && strings.Contains(t, "](") {
			return true
		}
		if strings.Contains(t, "**") || strings.Contains(t, "__") {
			return true
		}
	}
	return false
}

func mdCacheKey(src string, width int, theme string) string {
	h := sha256.Sum256([]byte(src))
	return hex.EncodeToString(h[:8]) + "|" + itoa(width) + "|" + theme
}

func mdCacheGet(key string) (string, bool) {
	mdMu.Lock()
	defer mdMu.Unlock()
	for _, e := range mdCache {
		if e.key == key {
			return e.out, true
		}
	}
	return "", false
}

func mdCachePut(key, out string) {
	mdMu.Lock()
	defer mdMu.Unlock()
	for i := range mdCache {
		if mdCache[i].key == key {
			mdCache[i].out = out
			return
		}
	}
	if len(mdCache) >= mdCacheLimit {
		mdCache = mdCache[1:]
	}
	mdCache = append(mdCache, mdCacheEntry{key: key, out: out})
}
