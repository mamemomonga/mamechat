package tts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/mamemomonga/mamechat/backend/internal/ttsautodict"
	"github.com/mamemomonga/mamechat/backend/internal/voicevox"
)

var (
	urlPattern       = regexp.MustCompile(`https?://\S+`)
	spacePattern     = regexp.MustCompile(`\s+`)
	symbolRunPattern = regexp.MustCompile(`([!！?？ｗw笑。、,.・ー\-_=*#]){6,}`)
)

func NormalizeText(text string) (string, bool) {
	return NormalizeTextWithDictionary(text, nil)
}

func NormalizeTextWithDictionary(text string, dictionary []RuntimeDictionaryEntry) (string, bool) {
	text = strings.TrimSpace(text)
	text = urlPattern.ReplaceAllString(text, "ゆーあーるえる")
	text = ttsautodict.ReplaceDirectives(text)
	text = applyDictionary(text, compileRuntimeDictionary(dictionary))
	text = applyDictionary(text, dictionaryEntries)
	text = symbolRunPattern.ReplaceAllString(text, "$1$1$1")
	text = spacePattern.ReplaceAllString(text, " ")
	hasReadable := false
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			hasReadable = true
			break
		}
	}
	return strings.TrimSpace(text), hasReadable
}

func SplitText(text string) []string {
	var parts []string
	var current []rune
	flush := func() {
		s := strings.TrimSpace(string(current))
		if s != "" {
			parts = append(parts, splitLongPart(s)...)
		}
		current = current[:0]
	}
	for _, r := range text {
		current = append(current, r)
		if strings.ContainsRune("。！？!?\n", r) {
			flush()
		}
	}
	flush()
	return parts
}

func SpeedScaleForMessage(settings Settings, totalRunes int) float64 {
	if totalRunes <= speedRampStartRunes {
		return settings.SpeedScale
	}
	maxRunes := settings.MessageMaxRunes
	if maxRunes < totalRunes {
		maxRunes = totalRunes
	}
	if maxRunes <= speedRampStartRunes {
		return maxSpeedScale
	}
	length := totalRunes
	if length > maxRunes {
		length = maxRunes
	}
	ratio := float64(length-speedRampStartRunes) / float64(maxRunes-speedRampStartRunes)
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	return settings.SpeedScale + (maxSpeedScale-settings.SpeedScale)*ratio
}

func splitLongPart(text string) []string {
	runes := []rune(text)
	if len(runes) <= maxPartRunes {
		return []string{text}
	}
	var parts []string
	start := 0
	lastBreak := -1
	for i, r := range runes {
		if strings.ContainsRune("、，,・;:：；", r) {
			lastBreak = i + 1
		}
		if i-start+1 >= maxPartRunes {
			end := i + 1
			if lastBreak > start {
				end = lastBreak
			}
			parts = append(parts, strings.TrimSpace(string(runes[start:end])))
			start = end
			lastBreak = -1
		}
	}
	if start < len(runes) {
		parts = append(parts, strings.TrimSpace(string(runes[start:])))
	}
	return parts
}

func ContentHash(text string, s Settings, speaker voicevox.ResolvedSpeaker) string {
	var b strings.Builder
	b.WriteString(hashVersion)
	b.WriteString("\n")
	b.WriteString(text)
	b.WriteString("\n")
	b.WriteString(strconv.FormatInt(int64(speaker.StyleID), 10))
	b.WriteString("\n")
	b.WriteString(speaker.Character.Name)
	b.WriteString("\n")
	b.WriteString(speaker.Character.UUID)
	b.WriteString("\n")
	b.WriteString(speaker.StyleName)
	b.WriteString("\n")
	b.WriteString(formatFloat(s.SpeedScale))
	b.WriteString("\n")
	b.WriteString(formatFloat(s.PitchScale))
	b.WriteString("\n")
	b.WriteString(formatFloat(s.IntonationScale))
	b.WriteString("\n")
	b.WriteString(formatFloat(s.VolumeScale))
	b.WriteString("\n")
	b.WriteString(formatFloat(s.PrePhonemeLength))
	b.WriteString("\n")
	b.WriteString(formatFloat(s.PostPhonemeLength))
	b.WriteString("\n")
	b.WriteString(s.VoicevoxEngineVersion)
	b.WriteString("\n")
	b.WriteString(normalizerVersion)
	b.WriteString("\n")
	b.WriteString(splitterVersion)
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("%s:%s:%d:mono", outputFormat, outputCodec, outputBitrate))
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', 3, 64)
}
