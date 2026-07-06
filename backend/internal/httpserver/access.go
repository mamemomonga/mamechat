package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/access"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/auth"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/mastodon"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/misskey"
)

var errUnresolvableEntry = errors.New("入力からユーザーを特定できませんでした")

// resolveAccessEntryHandler はオーナー/管理者が入室許可リストに登録する1件を、
// 対応するSNSへ問い合わせて安定ID（atprotoならDID、fediなら instance:accountID）に
// 解決して返す。
func (s *Server) resolveAccessEntryHandler(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.CurrentSession(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	channel, err := s.q.GetChannelBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		writeError(w, http.StatusNotFound, "channel not found")
		return
	}
	isOwner := channel.OwnerUserID.Valid && channel.OwnerUserID.Int64 == session.User.ID
	if !isOwner && !auth.IsPrivileged(session.User.Role) {
		writeError(w, http.StatusForbidden, "channel owner only")
		return
	}
	var req struct {
		Entry string `json:"entry"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	entry, err := s.resolveAccessEntry(r.Context(), req.Entry)
	if err != nil {
		// 解決失敗はユーザー入力起因が主なので 422 で理由を返す。
		writeError(w, http.StatusUnprocessableEntity, accessResolveErrorMessage(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entry": entry})
}

// accessTarget は生入力を分類した結果（解決先の手がかり）。
type accessTarget struct {
	atprotoActor string // atproto: ハンドルまたはDID
	fediUser     string // fedi: ローカルユーザー名
	fediHost     string // fedi: インスタンスホスト
}

// resolveAccessEntry は生入力を解決して access.Entry を返す。
func (s *Server) resolveAccessEntry(ctx context.Context, raw string) (access.Entry, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return access.Entry{}, errUnresolvableEntry
	}
	target, err := classifyAccessTarget(raw)
	if err != nil {
		return access.Entry{}, err
	}
	if target.atprotoActor != "" {
		return s.resolveAtprotoEntry(ctx, raw, target.atprotoActor)
	}
	return s.resolveFediEntry(ctx, raw, target.fediUser, target.fediHost)
}

func (s *Server) resolveAtprotoEntry(ctx context.Context, raw, actor string) (access.Entry, error) {
	if s.atproto == nil {
		return access.Entry{}, errUnresolvableEntry
	}
	profile, err := s.atproto.FetchProfile(ctx, actor)
	if err != nil || profile.DID == "" {
		return access.Entry{}, errUnresolvableEntry
	}
	return access.Entry{
		Provider:    "atproto",
		Subject:     profile.DID,
		Handle:      profile.Handle,
		DisplayName: profile.DisplayName,
		ProfileURL:  "https://bsky.app/profile/" + profile.DID,
		Raw:         raw,
	}, nil
}

// resolveFediEntry は Mastodon を先に試し、ダメなら Misskey を試す（インスタンス種別は
// 入力からは判別できないため）。
func (s *Server) resolveFediEntry(ctx context.Context, raw, user, host string) (access.Entry, error) {
	if s.mastodon != nil {
		if inst, err := mastodon.NormalizeInstanceURL(host); err == nil {
			if p, err := s.mastodon.LookupAccount(ctx, inst, user); err == nil && p.AccountID != "" {
				return fediEntry("mastodon", raw, user, inst, p.AccountID, p.DisplayName, p.ProfileURL), nil
			}
		}
	}
	if s.misskey != nil {
		if inst, err := misskey.NormalizeInstanceURL(host); err == nil {
			if p, err := s.misskey.LookupUser(ctx, inst, user); err == nil && p.AccountID != "" {
				return fediEntry("misskey", raw, user, inst, p.AccountID, p.DisplayName, p.ProfileURL), nil
			}
		}
	}
	return access.Entry{}, errUnresolvableEntry
}

func fediEntry(provider, raw, user, instanceURL, accountID, displayName, profileURL string) access.Entry {
	host := instanceURL
	if u, err := url.Parse(instanceURL); err == nil && u.Host != "" {
		host = u.Host
	}
	if profileURL == "" {
		profileURL = instanceURL + "/@" + user
	}
	return access.Entry{
		Provider:    provider,
		Subject:     instanceURL + ":" + accountID,
		Handle:      user + "@" + host,
		DisplayName: displayName,
		ProfileURL:  profileURL,
		Raw:         raw,
	}
}

// classifyAccessTarget は生入力を atproto / fedi に分類する。
//   - did:... → atproto（DID）
//   - https://bsky.app/profile/<actor> → atproto
//   - https://<host>/@<user> または /users/<user> → fedi
//   - "@user@host" / "user@host" → fedi
//   - "alice.bsky.social"（ドメイン形式・@なし）→ atproto ハンドル
func classifyAccessTarget(raw string) (accessTarget, error) {
	if strings.HasPrefix(raw, "did:") {
		return accessTarget{atprotoActor: raw}, nil
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return classifyURL(raw)
	}
	handle := strings.TrimSpace(strings.TrimPrefix(raw, "@"))
	if handle == "" {
		return accessTarget{}, errUnresolvableEntry
	}
	if strings.Contains(handle, "@") {
		parts := strings.SplitN(handle, "@", 2)
		if parts[0] == "" || parts[1] == "" {
			return accessTarget{}, errUnresolvableEntry
		}
		return accessTarget{fediUser: parts[0], fediHost: parts[1]}, nil
	}
	// ドメイン形式のハンドルは atproto とみなす。
	return accessTarget{atprotoActor: handle}, nil
}

func classifyURL(raw string) (accessTarget, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return accessTarget{}, errUnresolvableEntry
	}
	path := strings.Trim(u.Path, "/")
	segs := strings.Split(path, "/")
	// bsky.app/profile/<actor> 形式は atproto。
	if len(segs) >= 2 && segs[0] == "profile" {
		actor := segs[1]
		if actor != "" {
			return accessTarget{atprotoActor: actor}, nil
		}
	}
	// fedi のプロフィールURL（/@user または /users/user）。
	last := segs[len(segs)-1]
	user := strings.TrimPrefix(last, "@")
	if user == "" || strings.Contains(user, "@") {
		return accessTarget{}, errUnresolvableEntry
	}
	return accessTarget{fediUser: user, fediHost: u.Host}, nil
}

func accessResolveErrorMessage(err error) string {
	if errors.Is(err, errUnresolvableEntry) {
		return "ユーザーを特定できませんでした。ハンドルまたはプロフィールURLを確認してください。"
	}
	slog.Warn("resolve access entry failed", "error", err)
	return "ユーザーの解決に失敗しました。時間をおいて再度お試しください。"
}
