// Package access はチャンネルの入室許可制御（ホワイトリスト/ブラックリスト）の
// 突き合わせロジックを提供する。リストの各エントリは登録時にサーバ側で解決され、
// 各SNSの安定した一意ID（Blueskyなら DID、Mastodon/Misskey なら instance:accountID）を
// 持つ。照合は安定IDを主軸に、ハンドル・プロフィールURLをフォールバックとして行う。
package access

import (
	"encoding/json"
	"net/url"
	"strings"
)

// 入室許可モード。
const (
	ModeNone      = "none"      // 制限なし（既定）
	ModeWhitelist = "whitelist" // リスト掲載者のみ入室可
	ModeBlacklist = "blacklist" // リスト掲載者は入室不可
)

// SettingWhitelistEnabled は app_settings 上でホワイトリスト機能のサーバ全体
// 有効/無効を保持するキー。値は "true"/"false"。未設定（行なし）は無効(false)扱い。
// 管理画面のサーバ設定から切り替える。
const SettingWhitelistEnabled = "whitelist_enabled"

// SettingEnabled は app_settings に保存した真偽値文字列を bool に解釈する。"true" のみ真。
func SettingEnabled(value string) bool {
	return value == "true"
}

// EffectiveMode はサーバ全体のホワイトリスト有効フラグを踏まえた実効モードを返す。
// ホワイトリストがサーバ側で無効なときは whitelist を none とみなす（ブラックリストは不変）。
// これにより無効化中は既存のホワイトリスト設定を保持したまま制限だけを外し、
// 再度有効化すれば元の制限が復活する。
func EffectiveMode(mode string, whitelistEnabled bool) string {
	mode = NormalizeMode(mode)
	if mode == ModeWhitelist && !whitelistEnabled {
		return ModeNone
	}
	return mode
}

// Entry は入室許可リストの1エントリ。登録時にサーバが解決した一意情報を持つ。
// Raw はオーナーが入力した元の文字列（解決できなかった場合のフォールバック・表示用）。
type Entry struct {
	Provider    string `json:"provider,omitempty"`
	Subject     string `json:"subject,omitempty"`
	Handle      string `json:"handle,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	ProfileURL  string `json:"profileUrl,omitempty"`
	Raw         string `json:"raw,omitempty"`
}

// NormalizeMode は未知の値を ModeNone に丸める。
func NormalizeMode(mode string) string {
	switch mode {
	case ModeWhitelist, ModeBlacklist:
		return mode
	default:
		return ModeNone
	}
}

// ParseList は access_list（JSONB）を Entry スライスへデコードする。
// 旧形式（文字列の配列）も寛容に受け付け、Raw のみの Entry として扱う。
func ParseList(raw []byte) []Entry {
	if len(raw) == 0 {
		return nil
	}
	var elems []json.RawMessage
	if err := json.Unmarshal(raw, &elems); err != nil {
		return nil
	}
	out := make([]Entry, 0, len(elems))
	for _, el := range elems {
		// 旧形式: 文字列エントリ
		var s string
		if err := json.Unmarshal(el, &s); err == nil {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, Entry{Raw: s})
			}
			continue
		}
		var e Entry
		if err := json.Unmarshal(el, &e); err == nil {
			out = append(out, e)
		}
	}
	return out
}

// MarshalList は Entry スライスを access_list 用のJSONへ変換する。
func MarshalList(entries []Entry) ([]byte, error) {
	if entries == nil {
		entries = []Entry{}
	}
	return json.Marshal(entries)
}

// CleanEntries は保存前にエントリを整える。識別キーを持たないものを除外し、
// 主キー（安定ID優先）で重複排除する。
func CleanEntries(entries []Entry) []Entry {
	out := make([]Entry, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		keys := entryKeys(e)
		if len(keys) == 0 {
			continue
		}
		primary := keys[0]
		if _, dup := seen[primary]; dup {
			continue
		}
		seen[primary] = struct{}{}
		out = append(out, e)
	}
	return out
}

// entryKeys は1エントリの照合キー群を返す（安定ID→ハンドル→URL→生入力の順）。
func entryKeys(e Entry) []string {
	keys := make([]string, 0, 3)
	if e.Provider != "" && e.Subject != "" {
		keys = append(keys, subjectKey(e.Provider, e.Subject))
	}
	if e.Handle != "" {
		keys = append(keys, handleKey(e.Handle))
	}
	if e.ProfileURL != "" {
		if c, ok := canonicalURL(e.ProfileURL); ok {
			keys = append(keys, "url:"+c)
		}
	}
	if len(keys) == 0 && e.Raw != "" {
		if k, ok := rawKey(e.Raw); ok {
			keys = append(keys, k)
		}
	}
	return keys
}

// rawKey はオーナーが入力した生文字列（ハンドルまたはプロフィールURL）を照合キーに変換する。
//   - http:// または https:// で始まる → URLとして正規化
//   - それ以外 → 先頭の単一の "@" を除去したハンドルとして正規化
func rawKey(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		canon, ok := canonicalURL(raw)
		if !ok {
			return "", false
		}
		return "url:" + canon, true
	}
	handle := strings.TrimSpace(strings.TrimPrefix(raw, "@"))
	if handle == "" {
		return "", false
	}
	return handleKey(handle), true
}

func subjectKey(provider, subject string) string {
	return "subject:" + strings.ToLower(strings.TrimSpace(provider)) + ":" + strings.ToLower(strings.TrimSpace(subject))
}

func handleKey(handle string) string {
	return "handle:" + strings.ToLower(strings.TrimSpace(handle))
}

// UserKeys はユーザーを識別する照合キーの集合を返す。安定ID・ハンドル・プロフィールURLの
// いずれからでも照合できるようにする。
func UserKeys(provider, subject, handle, profileURL string) map[string]struct{} {
	keys := make(map[string]struct{}, 3)
	if p, s := strings.TrimSpace(provider), strings.TrimSpace(subject); p != "" && s != "" {
		keys[subjectKey(p, s)] = struct{}{}
	}
	if h := strings.TrimSpace(handle); h != "" {
		keys[handleKey(h)] = struct{}{}
	}
	if profileURL != "" {
		if c, ok := canonicalURL(profileURL); ok {
			keys["url:"+c] = struct{}{}
		}
	}
	return keys
}

// Listed はユーザーがリストのいずれかのエントリに一致するかを返す。
func Listed(list []Entry, userKeys map[string]struct{}) bool {
	if len(userKeys) == 0 {
		return false
	}
	for _, e := range list {
		for _, key := range entryKeys(e) {
			if _, hit := userKeys[key]; hit {
				return true
			}
		}
	}
	return false
}

// Allowed はモードとリストに基づきユーザーの入室可否を返す。
func Allowed(mode string, list []Entry, userKeys map[string]struct{}) bool {
	switch NormalizeMode(mode) {
	case ModeWhitelist:
		return Listed(list, userKeys)
	case ModeBlacklist:
		return !Listed(list, userKeys)
	default:
		return true
	}
}

// canonicalURL はプロフィールURLを照合用に正規化する。スキームとホストを小文字化し、
// 末尾スラッシュを除去、クエリ・フラグメントを落とす。
func canonicalURL(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Host)
	path := strings.TrimRight(u.Path, "/")
	return scheme + "://" + host + path, true
}
