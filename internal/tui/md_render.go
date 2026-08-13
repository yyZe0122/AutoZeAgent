package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
)

const (
	mdMaxRunes     = 12000
	mdCacheLimit   = 64
	liveMDMinRunes = 80
	liveMDMinWait  = 200 * time.Millisecond
)

type mdCacheEntry struct {
	key string
	out string
}

var (
	mdMu    sync.Mutex
	mdCache []mdCacheEntry

	liveMDMu    sync.Mutex
	liveMDCache []liveMDEntry
)

type liveMDEntry struct {
	key   string
	src   string
	out   string
	at    time.Time
	runes int
	width int
	theme ThemeName
}

// renderMarkdown renders assistant reply markdown for the terminal.
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

func unclosedFence(s string) bool {
	return strings.Count(s, "```")%2 == 1
}

// renderLiveMarkdown throttles glamour during streaming (T8).
// Unclosed fences and recent tiny deltas stay plain / last snapshot.
// key isolates concurrent streams (typically contentBlock.Key).
func renderLiveMarkdown(src string, width int, theme ThemeName, key string) string {
	src = strings.TrimRight(src, "\n")
	if src == "" || unclosedFence(src) || !looksLikeMarkdown(src) {
		return src
	}
	if utf8.RuneCountInString(src) > mdMaxRunes {
		return src
	}
	if strings.TrimSpace(key) == "" {
		key = "_"
	}
	now := time.Now()
	n := utf8.RuneCountInString(src)
	liveMDMu.Lock()
	defer liveMDMu.Unlock()
	if e, ok := liveMDLookup(key); ok {
		if src == e.src && width == e.width && theme == e.theme && e.out != "" {
			return e.out
		}
		if e.out != "" && width == e.width && theme == e.theme {
			if now.Sub(e.at) < liveMDMinWait && n-e.runes < liveMDMinRunes {
				return e.out
			}
		}
	}
	out := renderMarkdown(src, width, theme)
	liveMDPut(liveMDEntry{
		key: key, src: src, out: out, at: now, runes: n, width: width, theme: theme,
	})
	return out
}

func liveMDLookup(key string) (liveMDEntry, bool) {
	for _, e := range liveMDCache {
		if e.key == key {
			return e, true
		}
	}
	return liveMDEntry{}, false
}

func liveMDPut(e liveMDEntry) {
	for i := range liveMDCache {
		if liveMDCache[i].key == e.key {
			liveMDCache[i] = e
			return
		}
	}
	if len(liveMDCache) >= mdCacheLimit {
		liveMDCache = liveMDCache[1:]
	}
	liveMDCache = append(liveMDCache, e)
}
