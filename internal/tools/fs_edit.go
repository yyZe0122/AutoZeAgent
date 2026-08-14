package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
)

const (
	defaultReadLineLimit = 2000
	maxReadLineLimit     = 10_000
	patchContextLines    = 20
	maxUnifiedDiffLines  = 400
)

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func hashMismatchError(want, got string) error {
	return fmt.Errorf("expected_sha256 mismatch: want %s have %s", want, got)
}

// matchPatchOld finds old in content with exact, CRLF-normalized, line-trim, then indent-align.
// Returns replacements count, updated text, or a context snippet when not found.
func matchPatchOld(content, old, neu string, replaceAll bool) (updated string, n int, hint string, err error) {
	if old == "" {
		return "", 0, "", fmt.Errorf("patch old text is required")
	}
	src := content
	if n = strings.Count(src, old); n > 0 {
		return applyReplace(src, old, neu, n, replaceAll)
	}
	// CRLF / lone CR → LF on both sides.
	srcLF := normalizeNewlines(src)
	oldLF := normalizeNewlines(old)
	neuLF := normalizeNewlines(neu)
	if n = strings.Count(srcLF, oldLF); n > 0 {
		return applyReplace(srcLF, oldLF, neuLF, n, replaceAll)
	}
	if aligned, ok := alignIndent(srcLF, oldLF); ok {
		if n = strings.Count(srcLF, aligned); n > 0 {
			return applyReplace(srcLF, aligned, alignNewToOld(aligned, oldLF, neuLF), n, replaceAll)
		}
	}
	if trimmedOld, ok := trimLineMatch(srcLF, oldLF); ok {
		if n = strings.Count(srcLF, trimmedOld); n > 0 {
			return applyReplace(srcLF, trimmedOld, neuLF, n, replaceAll)
		}
	}
	return "", 0, nearbyContext(srcLF, oldLF, patchContextLines), fmt.Errorf("patch old text not found")
}

func applyReplace(src, old, neu string, found int, replaceAll bool) (string, int, string, error) {
	if found > 1 && !replaceAll {
		return "", found, nearbyContext(src, old, patchContextLines),
			fmt.Errorf("patch old text appears %d times; provide more context or set replace_all to true", found)
	}
	n := 1
	if replaceAll {
		n = found
	}
	return strings.Replace(src, old, neu, n), n, "", nil
}

func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

func alignIndent(src, old string) (string, bool) {
	srcLines := strings.Split(src, "\n")
	oldLines := strings.Split(old, "\n")
	if len(oldLines) == 0 {
		return "", false
	}
	oldFirst := strings.TrimLeftFunc(oldLines[0], unicode.IsSpace)
	if oldFirst == "" {
		return "", false
	}
	for i := 0; i <= len(srcLines)-len(oldLines); i++ {
		srcFirst := srcLines[i]
		trimSrc := strings.TrimLeftFunc(srcFirst, unicode.IsSpace)
		if trimSrc != oldFirst {
			continue
		}
		prefix := srcFirst[:len(srcFirst)-len(trimSrc)]
		oldPrefix := oldLines[0][:len(oldLines[0])-len(oldFirst)]
		delta := len([]rune(prefix)) - len([]rune(oldPrefix))
		built := make([]string, len(oldLines))
		ok := true
		for j, line := range oldLines {
			shifted := shiftIndent(line, delta)
			if i+j >= len(srcLines) || srcLines[i+j] != shifted {
				ok = false
				break
			}
			built[j] = shifted
		}
		if ok {
			return strings.Join(built, "\n"), true
		}
	}
	return "", false
}

func shiftIndent(line string, delta int) string {
	if delta == 0 || line == "" {
		return line
	}
	if delta > 0 {
		return strings.Repeat(" ", delta) + line
	}
	runes := []rune(line)
	drop := -delta
	i := 0
	for i < len(runes) && i < drop && unicode.IsSpace(runes[i]) {
		i++
	}
	return string(runes[i:])
}

func alignNewToOld(alignedOld, origOld, neu string) string {
	// Preserve the indent we matched in the file when substituting new.
	if !strings.Contains(origOld, "\n") && !strings.Contains(neu, "\n") {
		oldTrim := strings.TrimLeftFunc(origOld, unicode.IsSpace)
		alignedTrim := strings.TrimLeftFunc(alignedOld, unicode.IsSpace)
		if oldTrim == alignedTrim && len(alignedOld) >= len(alignedTrim) {
			prefix := alignedOld[:len(alignedOld)-len(alignedTrim)]
			return prefix + strings.TrimLeftFunc(neu, unicode.IsSpace)
		}
	}
	return neu
}

func trimLineMatch(src, old string) (string, bool) {
	oldLines := strings.Split(old, "\n")
	srcLines := strings.Split(src, "\n")
	if len(oldLines) == 0 {
		return "", false
	}
	want := make([]string, len(oldLines))
	for i, line := range oldLines {
		want[i] = strings.TrimSpace(line)
	}
	for i := 0; i <= len(srcLines)-len(oldLines); i++ {
		ok := true
		got := make([]string, len(oldLines))
		for j := range oldLines {
			if strings.TrimSpace(srcLines[i+j]) != want[j] {
				ok = false
				break
			}
			got[j] = srcLines[i+j]
		}
		if ok {
			return strings.Join(got, "\n"), true
		}
	}
	return "", false
}

func nearbyContext(src, old string, radius int) string {
	if radius < 1 {
		radius = patchContextLines
	}
	lines := strings.Split(src, "\n")
	needle := strings.TrimSpace(strings.Split(old, "\n")[0])
	idx := -1
	if needle != "" {
		for i, line := range lines {
			if strings.Contains(line, needle) || strings.Contains(strings.TrimSpace(line), needle) {
				idx = i
				break
			}
		}
	}
	if idx < 0 {
		start := 0
		end := minInt(len(lines), radius*2)
		return formatLineWindow(lines, start, end)
	}
	start := idx - radius
	if start < 0 {
		start = 0
	}
	end := idx + radius + 1
	if end > len(lines) {
		end = len(lines)
	}
	return formatLineWindow(lines, start, end)
}

func formatLineWindow(lines []string, start, end int) string {
	var b strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&b, "%6d|%s\n", i+1, lines[i])
	}
	return strings.TrimRight(b.String(), "\n")
}

func numberedSlice(lines []string, startLine, count int) string {
	if startLine < 1 {
		startLine = 1
	}
	end := startLine - 1 + count
	if startLine-1 >= len(lines) {
		return ""
	}
	if end > len(lines) {
		end = len(lines)
	}
	var b strings.Builder
	for i := startLine - 1; i < end; i++ {
		fmt.Fprintf(&b, "%6d|%s\n", i+1, lines[i])
	}
	return strings.TrimRight(b.String(), "\n")
}

func unifiedDiff(path, before, after string) string {
	oldLines := trimTrailingEmpty(strings.Split(normalizeNewlines(before), "\n"))
	newLines := trimTrailingEmpty(strings.Split(normalizeNewlines(after), "\n"))
	pre := 0
	for pre < len(oldLines) && pre < len(newLines) && oldLines[pre] == newLines[pre] {
		pre++
	}
	osuf, nsuf := len(oldLines), len(newLines)
	for osuf > pre && nsuf > pre && oldLines[osuf-1] == newLines[nsuf-1] {
		osuf--
		nsuf--
	}
	ctx := 3
	hs := pre - ctx
	if hs < 0 {
		hs = 0
	}
	he := osuf + ctx
	if he > len(oldLines) {
		he = len(oldLines)
	}
	nhs := pre - (pre - hs)
	nhe := nsuf + (he - osuf)
	if nhe > len(newLines) {
		nhe = len(newLines)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "--- a/%s\n+++ b/%s\n", path, path)
	fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", hs+1, he-hs, nhs+1, nhe-nhs)
	for i := hs; i < pre; i++ {
		fmt.Fprintf(&b, " %s\n", oldLines[i])
	}
	for i := pre; i < osuf; i++ {
		fmt.Fprintf(&b, "-%s\n", oldLines[i])
	}
	for i := pre; i < nsuf; i++ {
		fmt.Fprintf(&b, "+%s\n", newLines[i])
	}
	for i := osuf; i < he; i++ {
		fmt.Fprintf(&b, " %s\n", oldLines[i])
	}
	out := b.String()
	lines := strings.Split(out, "\n")
	if len(lines) > maxUnifiedDiffLines {
		return strings.Join(lines[:maxUnifiedDiffLines], "\n") + "\n...[diff truncated]\n"
	}
	return out
}

func trimTrailingEmpty(lines []string) []string {
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		return lines[:len(lines)-1]
	}
	return lines
}
