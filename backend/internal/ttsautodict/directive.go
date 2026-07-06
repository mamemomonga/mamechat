package ttsautodict

import (
	"regexp"
	"strings"
)

type Directive struct {
	Term    string
	Reading string
}

// [単語](カタカナ読み) 形式の明示的な読み登録指示を検出する。
//   - 単語側は ] と改行以外を許可（スペースや絵文字を含められる）。
//   - 読み側はカタカナ（長音符・中黒を含む）のみ。
//
// 例: [Claude](クロード) / [Nothing Phone](ナッシングフォン) / [🐙](タコ)
// 角括弧・丸括弧は半角/全角どちらも受け付ける。
var directivePattern = regexp.MustCompile(
	`[\[［]([^\[\]［］\n]{1,64})[\]］][（(]([\p{Katakana}ー・]{1,64})[）)]`,
)

func ExtractDirectives(text string) []Directive {
	matches := directivePattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	directives := make([]Directive, 0, len(matches))
	for _, match := range matches {
		term := strings.TrimSpace(match[1])
		reading := strings.TrimSpace(match[2])
		if term == "" || reading == "" {
			continue
		}
		key := TermKey(term)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		directives = append(directives, Directive{
			Term:    term,
			Reading: reading,
		})
	}
	return directives
}

// ReplaceDirectives は読み上げ正規化用に、指示マークアップを読み（カタカナ）へ置換する。
func ReplaceDirectives(text string) string {
	return directivePattern.ReplaceAllString(text, "$2")
}

func TermKey(term string) string {
	return strings.ToLower(strings.TrimSpace(term))
}
