package misskey

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mamemomonga/mamechat/backend/internal/config"
	db "github.com/mamemomonga/mamechat/backend/internal/generated/db"
	"github.com/mamemomonga/mamechat/backend/internal/store"
)

const (
	providerMisskey       = "misskey"
	sessionTTL            = 10 * time.Minute
	profileStaleAfterHour = 24
	profileSyncInterval   = 6 * time.Hour
	miAuthPermission      = "read:account"
)

var ErrInvalidInstance = errors.New("invalid misskey instance url")

type Client struct {
	cfg  config.Config
	pool *pgxpool.Pool
	q    *db.Queries
	http *http.Client
}

type ActorProfile struct {
	AccountID   string
	Username    string
	DisplayName string
	AvatarURL   string
	InstanceURL string
	ProfileURL  string
	Raw         []byte
}

func NewClient(cfg config.Config, pool *pgxpool.Pool) *Client {
	return &Client{
		cfg:  cfg,
		pool: pool,
		q:    db.New(pool),
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

// NormalizeInstanceURL normalizes a Misskey instance URL to https://host form.
func NormalizeInstanceURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrInvalidInstance
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", ErrInvalidInstance
	}
	return "https://" + u.Host, nil
}

// StartMiAuth begins the MiAuth flow and returns the authorization URL.
func (c *Client) StartMiAuth(ctx context.Context, rawInstanceURL string) (string, error) {
	instanceURL, err := NormalizeInstanceURL(rawInstanceURL)
	if err != nil {
		return "", err
	}
	if err := c.q.DeleteExpiredMisskeyMiAuthSessions(ctx); err != nil {
		slog.Warn("delete expired misskey miauth sessions failed", "error", err)
	}

	session, err := newSessionID()
	if err != nil {
		return "", err
	}
	if _, err := c.q.CreateMisskeyMiAuthSession(ctx, db.CreateMisskeyMiAuthSessionParams{
		Session:     session,
		InstanceUrl: instanceURL,
		ExpiresAt:   time.Now().Add(sessionTTL),
	}); err != nil {
		return "", err
	}

	authURL, err := url.Parse(instanceURL + "/miauth/" + url.PathEscape(session))
	if err != nil {
		return "", err
	}
	q := authURL.Query()
	q.Set("name", c.cfg.ServiceName)
	q.Set("callback", c.callbackURL())
	q.Set("permission", miAuthPermission)
	authURL.RawQuery = q.Encode()
	return authURL.String(), nil
}

// CompleteMiAuth verifies the MiAuth session and upserts the user.
func (c *Client) CompleteMiAuth(ctx context.Context, session string) (db.User, error) {
	if session == "" {
		return db.User{}, errors.New("session is required")
	}
	saved, err := c.q.GetMisskeyMiAuthSession(ctx, session)
	if err != nil {
		return db.User{}, err
	}
	defer func() {
		if err := c.q.DeleteMisskeyMiAuthSession(context.Background(), session); err != nil {
			slog.Warn("delete misskey miauth session failed", "error", err)
		}
	}()
	if time.Now().After(saved.ExpiresAt) {
		return db.User{}, errors.New("misskey miauth session expired")
	}

	profile, err := c.checkMiAuth(ctx, saved.InstanceUrl, session)
	if err != nil {
		return db.User{}, err
	}
	profile.InstanceURL = saved.InstanceUrl
	return c.upsertProfile(ctx, profile, true)
}

type checkResponse struct {
	OK   bool           `json:"ok"`
	User misskeyUserRaw `json:"user"`
}

type misskeyUserRaw struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl"`
}

func (c *Client) checkMiAuth(ctx context.Context, instanceURL, session string) (ActorProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		instanceURL+"/api/miauth/"+url.PathEscape(session)+"/check",
		bytes.NewReader([]byte("{}")))
	if err != nil {
		return ActorProfile{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.http.Do(req)
	if err != nil {
		return ActorProfile{}, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return ActorProfile{}, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return ActorProfile{}, fmt.Errorf("misskey miauth check failed: status=%d body=%s", res.StatusCode, truncate(body, 300))
	}
	var resp checkResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return ActorProfile{}, err
	}
	if !resp.OK {
		return ActorProfile{}, errors.New("misskey miauth check returned ok=false (user may have denied)")
	}
	if resp.User.ID == "" {
		return ActorProfile{}, errors.New("misskey miauth check missing user id")
	}
	return ActorProfile{
		AccountID:   resp.User.ID,
		Username:    resp.User.Username,
		DisplayName: resp.User.Name,
		AvatarURL:   resp.User.AvatarURL,
		Raw:         body,
	}, nil
}

// FetchProfile fetches a public Misskey profile by user ID via the users/show API.
func (c *Client) FetchProfile(ctx context.Context, instanceURL, userID string) (ActorProfile, error) {
	payload, err := json.Marshal(map[string]string{"userId": userID})
	if err != nil {
		return ActorProfile{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		instanceURL+"/api/users/show", bytes.NewReader(payload))
	if err != nil {
		return ActorProfile{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.http.Do(req)
	if err != nil {
		return ActorProfile{}, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return ActorProfile{}, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return ActorProfile{}, fmt.Errorf("misskey profile fetch failed: status=%d body=%s", res.StatusCode, truncate(body, 300))
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return ActorProfile{}, err
	}
	return ActorProfile{
		AccountID:   stringField(raw, "id"),
		Username:    stringField(raw, "username"),
		DisplayName: stringField(raw, "name"),
		AvatarURL:   stringField(raw, "avatarUrl"),
		InstanceURL: instanceURL,
		Raw:         body,
	}, nil
}

// LookupUser は home インスタンス上でローカルユーザー名から公開ユーザーを解決する
// （users/show に username + host=null を渡す）。入室許可リスト登録時の名前解決に使う。
func (c *Client) LookupUser(ctx context.Context, instanceURL, username string) (ActorProfile, error) {
	payload, err := json.Marshal(map[string]any{"username": username, "host": nil})
	if err != nil {
		return ActorProfile{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		instanceURL+"/api/users/show", bytes.NewReader(payload))
	if err != nil {
		return ActorProfile{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.http.Do(req)
	if err != nil {
		return ActorProfile{}, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return ActorProfile{}, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return ActorProfile{}, fmt.Errorf("misskey user lookup failed: status=%d body=%s", res.StatusCode, truncate(body, 300))
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return ActorProfile{}, err
	}
	resolvedName := stringField(raw, "username")
	if resolvedName == "" {
		resolvedName = username
	}
	return ActorProfile{
		AccountID:   stringField(raw, "id"),
		Username:    resolvedName,
		DisplayName: stringField(raw, "name"),
		AvatarURL:   stringField(raw, "avatarUrl"),
		InstanceURL: instanceURL,
		ProfileURL:  instanceURL + "/@" + resolvedName,
		Raw:         body,
	}, nil
}

// StartProfileSync starts a background goroutine that periodically syncs stale Misskey profiles.
func (c *Client) StartProfileSync(ctx context.Context) {
	go func() {
		initial := time.NewTimer(2 * time.Minute)
		ticker := time.NewTicker(profileSyncInterval)
		defer initial.Stop()
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-initial.C:
				c.syncStaleProfiles(ctx)
			case <-ticker.C:
				c.syncStaleProfiles(ctx)
			}
		}
	}()
}

func (c *Client) syncStaleProfiles(ctx context.Context) {
	identities, err := c.q.ListStaleMisskeyIdentities(ctx, db.ListStaleMisskeyIdentitiesParams{
		StaleAfterHours: profileStaleAfterHour,
		Limit:           100,
	})
	if err != nil {
		slog.Warn("list stale misskey identities failed", "error", err)
		return
	}
	for _, identity := range identities {
		if !identity.InstanceUrl.Valid || identity.InstanceUrl.String == "" {
			continue
		}
		instanceURL := identity.InstanceUrl.String
		userID := subjectUserID(identity.Subject, instanceURL)
		if userID == "" {
			slog.Warn("skip misskey profile sync: cannot parse user id", "subject", identity.Subject)
			continue
		}
		profile, err := c.FetchProfile(ctx, instanceURL, userID)
		if err != nil {
			slog.Warn("sync misskey profile failed", "subject", identity.Subject, "error", err)
			continue
		}
		if _, err := c.upsertProfile(ctx, profile, false); err != nil {
			slog.Warn("update misskey profile failed", "subject", identity.Subject, "error", err)
		}
	}
}

// subjectUserID extracts the user ID from "https://instance.example:user_id".
func subjectUserID(subject, instanceURL string) string {
	prefix := instanceURL + ":"
	if !strings.HasPrefix(subject, prefix) {
		return ""
	}
	return subject[len(prefix):]
}

func (c *Client) upsertProfile(ctx context.Context, profile ActorProfile, verified bool) (db.User, error) {
	instanceURL := profile.InstanceURL
	subject := instanceURL + ":" + profile.AccountID
	handle := buildHandle(profile.Username, instanceURL)

	displayName := strings.TrimSpace(profile.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(profile.Username)
	}
	if displayName == "" {
		displayName = handle
	}

	profileURL := instanceURL + "/@" + profile.Username

	var out db.User
	err := store.WithTx(ctx, c.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		identity, err := q.GetAuthIdentityByProviderSubject(ctx, db.GetAuthIdentityByProviderSubjectParams{
			Provider: providerMisskey,
			Subject:  subject,
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if errors.Is(err, pgx.ErrNoRows) {
			user, err := q.CreateUser(ctx, db.CreateUserParams{
				DisplayName: displayName,
				Handle:      nullableText(handle),
				AvatarUrl:   nullableText(profile.AvatarURL),
			})
			if err != nil {
				return err
			}
			if _, err := q.CreateAuthIdentity(ctx, db.CreateAuthIdentityParams{
				UserID:      user.ID,
				Provider:    providerMisskey,
				Subject:     subject,
				InstanceUrl: nullableText(instanceURL),
				Handle:      nullableText(handle),
				DisplayName: nullableText(displayName),
				AvatarUrl:   nullableText(profile.AvatarURL),
				ProfileUrl:  nullableText(profileURL),
				RawProfile:  profile.Raw,
			}); err != nil {
				return err
			}
			out = user
			return nil
		}

		user, err := q.UpdateUserProfile(ctx, db.UpdateUserProfileParams{
			ID:          identity.UserID,
			DisplayName: displayName,
			Handle:      nullableText(handle),
			AvatarUrl:   nullableText(profile.AvatarURL),
		})
		if err != nil {
			return err
		}
		if _, err := q.UpdateAuthIdentityProfileByProviderSubject(ctx, db.UpdateAuthIdentityProfileByProviderSubjectParams{
			Provider:    providerMisskey,
			Subject:     subject,
			UserID:      user.ID,
			Handle:      nullableText(handle),
			DisplayName: nullableText(displayName),
			AvatarUrl:   nullableText(profile.AvatarURL),
			ProfileUrl:  nullableText(profileURL),
			RawProfile:  profile.Raw,
			Verified:    verified,
		}); err != nil {
			return err
		}
		out = user
		return nil
	})
	return out, err
}

func (c *Client) callbackURL() string {
	return c.cfg.AtprotoPublicBaseURL + "/api/auth/misskey/callback"
}

// buildHandle builds "user@instance" from username and instance URL.
func buildHandle(username, instanceURL string) string {
	u, err := url.Parse(instanceURL)
	if err != nil || u.Host == "" {
		return username
	}
	return username + "@" + u.Host
}

// newSessionID generates a random UUID v4 string.
func newSessionID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func nullableText(v string) pgtype.Text {
	v = strings.TrimSpace(v)
	if v == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: v, Valid: true}
}

func stringField(raw map[string]any, key string) string {
	v, _ := raw[key].(string)
	return strings.TrimSpace(v)
}

func truncate(in []byte, max int) string {
	if len(in) <= max {
		return string(in)
	}
	return string(in[:max]) + "..."
}
