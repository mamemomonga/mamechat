package tts

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"log/slog"
	"regexp"
	"sort"
	"strings"
)

//go:embed dictionary.tsv
var dictionaryFS embed.FS

type dictionaryEntry struct {
	replacement string
	re          *regexp.Regexp
}

type RuntimeDictionaryEntry struct {
	Term    string
	Reading string
}

var dictionaryEntries []dictionaryEntry

func init() {
	dictionaryEntries = loadDictionary()
	normalizerVersion = "normalizer-v4-" + dictionaryDigest()
}

func applyDictionary(text string, entries []dictionaryEntry) string {
	for _, entry := range entries {
		text = entry.re.ReplaceAllString(text, entry.replacement)
	}
	return text
}

func compileRuntimeDictionary(entries []RuntimeDictionaryEntry) []dictionaryEntry {
	if len(entries) == 0 {
		return nil
	}
	sorted := append([]RuntimeDictionaryEntry(nil), entries...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return len([]rune(sorted[i].Term)) > len([]rune(sorted[j].Term))
	})
	compiled := make([]dictionaryEntry, 0, len(sorted))
	for _, entry := range sorted {
		term := strings.TrimSpace(entry.Term)
		reading := strings.TrimSpace(entry.Reading)
		if term == "" || reading == "" {
			continue
		}
		pattern := "(?i)" + regexp.QuoteMeta(term)
		re, err := regexp.Compile(pattern)
		if err != nil {
			slog.Warn("skip invalid runtime tts dictionary entry", "term", term, "error", err)
			continue
		}
		compiled = append(compiled, dictionaryEntry{
			replacement: reading,
			re:          re,
		})
	}
	return compiled
}

func loadDictionary() []dictionaryEntry {
	b, err := dictionaryFS.ReadFile("dictionary.tsv")
	if err != nil {
		slog.Warn("read tts dictionary failed", "error", err)
		return nil
	}
	var entries []dictionaryEntry
	for lineNo, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cols := strings.SplitN(line, "\t", 2)
		if len(cols) != 2 {
			slog.Warn("skip invalid tts dictionary row", "line", lineNo+1)
			continue
		}
		pattern := strings.TrimSpace(cols[0])
		replacement := strings.TrimSpace(cols[1])
		if pattern == "" || replacement == "" {
			slog.Warn("skip empty tts dictionary row", "line", lineNo+1)
			continue
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			slog.Warn("skip invalid tts dictionary pattern", "line", lineNo+1, "pattern", pattern, "error", err)
			continue
		}
		entries = append(entries, dictionaryEntry{
			replacement: replacement,
			re:          re,
		})
	}
	return entries
}

func dictionaryDigest() string {
	b, err := dictionaryFS.ReadFile("dictionary.tsv")
	if err != nil {
		return "dictionary-missing"
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:12]
}
