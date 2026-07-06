package mastodon

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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

	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/config"
	db "tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/generated/db"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/store"
)

const (
	providerMastodon      = "mastodon"
	oauthStateTTL         = 10 * time.Minute
	profileStaleAfterHour = 24
	profileSyncInterval   = 6 * time.Hour
	oauthScope            = "read:accounts"
)

var ErrInvalidInstance = errors.New("invalid mastodon instance url")

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

// NormalizeInstanceURL normalizes a Mastodon instance URL to https://host form.
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

// StartOAuth begins the Mastodon OAuth 2.0 flow and returns the authorization URL.
func (c *Client) StartOAuth(ctx context.Context, rawInstanceURL string) (string, error) {
	instanceURL, err := NormalizeInstanceURL(rawInstanceURL)
	if err != nil {
		return "", err
	}
	if err := c.q.DeleteExpiredMastodonOAuthStates(ctx); err != nil {
		slog.Warn("delete expired mastodon oauth states failed", "error", err)
	}

	app, err := c.ensureAppRegistration(ctx, instanceURL)
	if err != nil {
		return "", fmt.Errorf("mastodon app registration failed for %s: %w", instanceURL, err)
	}

	state, err := randomToken()
	if err != nil {
		return "", err
	}
	codeVerifier, err := randomToken()
	if err != nil {
		return "", err
	}

	if _, err := c.q.CreateMastodonOAuthState(ctx, db.CreateMastodonOAuthStateParams{
		State:        state,
		InstanceUrl:  instanceURL,
		CodeVerifier: codeVerifier,
		ExpiresAt:    time.Now().Add(oauthStateTTL),
	}); err != nil {
		return "", err
	}

	authURL, err := url.Parse(instanceURL + "/oauth/authorize")
	if err != nil {
		return "", err
	}
	q := authURL.Query()
	q.Set("response_type", "code")
	q.Set("client_id", app.ClientID)
	q.Set("redirect_uri", c.redirectURI())
	q.Set("scope", oauthScope)
	q.Set("state", state)
	q.Set("code_challenge", codeChallenge(codeVerifier))
	q.Set("code_challenge_method", "S256")
	authURL.RawQuery = q.Encode()
	return authURL.String(), nil
}

// CompleteOAuth exchanges the authorization code for an access token and upserts the user.
func (c *Client) CompleteOAuth(ctx context.Context, state, code string) (db.User, error) {
	if state == "" || code == "" {
		return db.User{}, errors.New("state and code are required")
	}
	saved, err := c.q.GetMastodonOAuthState(ctx, state)
	if err != nil {
		return db.User{}, err
	}
	defer func() {
		if err := c.q.DeleteMastodonOAuthState(context.Background(), state); err != nil {
			slog.Warn("delete mastodon oauth state failed", "error", err)
		}
	}()
	if time.Now().After(saved.ExpiresAt) {
		return db.User{}, errors.New("mastodon oauth state expired")
	}

	app, err := c.q.GetMastodonAppRegistration(ctx, saved.InstanceUrl)
	if err != nil {
		return db.User{}, fmt.Errorf("mastodon app registration not found for %s: %w", saved.InstanceUrl, err)
	}

	accessToken, err := c.exchangeCode(ctx, saved.InstanceUrl, app.ClientID, app.ClientSecret, code, saved.CodeVerifier)
	if err != nil {
		return db.User{}, err
	}

	profile, err := c.verifyCredentials(ctx, saved.InstanceUrl, accessToken)
	if err != nil {
		return db.User{}, err
	}
	profile.InstanceURL = saved.InstanceUrl
	return c.upsertProfile(ctx, profile, true)
}

func (c *Client) ensureAppRegistration(ctx context.Context, instanceURL string) (db.MastodonAppRegistration, error) {
	app, err := c.q.GetMastodonAppRegistration(ctx, instanceURL)
	if err == nil {
		return app, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.MastodonAppRegistration{}, err
	}
	return c.registerApp(ctx, instanceURL)
}

type appRegistrationResponse struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

func (c *Client) registerApp(ctx context.Context, instanceURL string) (db.MastodonAppRegistration, error) {
	form := url.Values{}
	form.Set("client_name", c.cfg.ServiceName)
	form.Set("redirect_uris", c.redirectURI())
	form.Set("scopes", oauthScope)
	if c.cfg.AtprotoPublicBaseURL != "" {
		form.Set("website", c.cfg.AtprotoPublicBaseURL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, instanceURL+"/api/v1/apps", strings.NewReader(form.Encode()))
	if err != nil {
		return db.MastodonAppRegistration{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := c.http.Do(req)
	if err != nil {
		return db.MastodonAppRegistration{}, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	if err != nil {
		return db.MastodonAppRegistration{}, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return db.MastodonAppRegistration{}, fmt.Errorf("mastodon app registration failed: status=%d body=%s", res.StatusCode, truncate(body, 300))
	}
	var reg appRegistrationResponse
	if err := json.Unmarshal(body, &reg); err != nil {
		return db.MastodonAppRegistration{}, err
	}
	if reg.ClientID == "" || reg.ClientSecret == "" {
		return db.MastodonAppRegistration{}, errors.New("mastodon app registration missing client credentials")
	}
	return c.q.UpsertMastodonAppRegistration(ctx, db.UpsertMastodonAppRegistrationParams{
		InstanceUrl:  instanceURL,
		ClientID:     reg.ClientID,
		ClientSecret: reg.ClientSecret,
	})
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
}

func (c *Client) exchangeCode(ctx context.Context, instanceURL, clientID, clientSecret, code, codeVerifier string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("redirect_uri", c.redirectURI())
	form.Set("code", code)
	form.Set("code_verifier", codeVerifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, instanceURL+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	if err != nil {
		return "", err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("mastodon token exchange failed: status=%d body=%s", res.StatusCode, truncate(body, 300))
	}
	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", err
	}
	if tok.AccessToken == "" {
		return "", errors.New("mastodon token exchange missing access_token")
	}
	return tok.AccessToken, nil
}

func (c *Client) verifyCredentials(ctx context.Context, instanceURL, accessToken string) (ActorProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, instanceURL+"/api/v1/accounts/verify_credentials", nil)
	if err != nil {
		return ActorProfile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

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
		return ActorProfile{}, fmt.Errorf("mastodon verify credentials failed: status=%d body=%s", res.StatusCode, truncate(body, 300))
	}
	return parseAccountJSON(body)
}

// FetchProfile fetches a public Mastodon profile by account ID (no auth required).
func (c *Client) FetchProfile(ctx context.Context, instanceURL, accountID string) (ActorProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, instanceURL+"/api/v1/accounts/"+url.PathEscape(accountID), nil)
	if err != nil {
		return ActorProfile{}, err
	}
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
		return ActorProfile{}, fmt.Errorf("mastodon profile fetch failed: status=%d body=%s", res.StatusCode, truncate(body, 300))
	}
	profile, err := parseAccountJSON(body)
	if err != nil {
		return ActorProfile{}, err
	}
	profile.InstanceURL = instanceURL
	return profile, nil
}

// LookupAccount は home インスタンス上でローカルユーザー名から公開アカウントを解決する
// （認証不要の /api/v1/accounts/lookup）。入室許可リスト登録時の名前解決に使う。
func (c *Client) LookupAccount(ctx context.Context, instanceURL, acct string) (ActorProfile, error) {
	endpoint, err := url.Parse(instanceURL + "/api/v1/accounts/lookup")
	if err != nil {
		return ActorProfile{}, err
	}
	values := endpoint.Query()
	values.Set("acct", acct)
	endpoint.RawQuery = values.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return ActorProfile{}, err
	}
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
		return ActorProfile{}, fmt.Errorf("mastodon account lookup failed: status=%d body=%s", res.StatusCode, truncate(body, 300))
	}
	profile, err := parseAccountJSON(body)
	if err != nil {
		return ActorProfile{}, err
	}
	profile.InstanceURL = instanceURL
	var raw map[string]any
	if json.Unmarshal(body, &raw) == nil {
		profile.ProfileURL = stringField(raw, "url")
	}
	return profile, nil
}

func parseAccountJSON(body []byte) (ActorProfile, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return ActorProfile{}, err
	}
	return ActorProfile{
		AccountID:   stringField(raw, "id"),
		Username:    stringField(raw, "username"),
		DisplayName: stringField(raw, "display_name"),
		AvatarURL:   stringField(raw, "avatar"),
		Raw:         body,
	}, nil
}

// StartProfileSync starts a background goroutine that periodically syncs stale Mastodon profiles.
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
	identities, err := c.q.ListStaleMastodonIdentities(ctx, db.ListStaleMastodonIdentitiesParams{
		StaleAfterHours: profileStaleAfterHour,
		Limit:           100,
	})
	if err != nil {
		slog.Warn("list stale mastodon identities failed", "error", err)
		return
	}
	for _, identity := range identities {
		if !identity.InstanceUrl.Valid || identity.InstanceUrl.String == "" {
			continue
		}
		instanceURL := identity.InstanceUrl.String
		accountID := subjectAccountID(identity.Subject, instanceURL)
		if accountID == "" {
			slog.Warn("skip mastodon profile sync: cannot parse account id", "subject", identity.Subject)
			continue
		}
		profile, err := c.FetchProfile(ctx, instanceURL, accountID)
		if err != nil {
			slog.Warn("sync mastodon profile failed", "subject", identity.Subject, "error", err)
			continue
		}
		if _, err := c.upsertProfile(ctx, profile, false); err != nil {
			slog.Warn("update mastodon profile failed", "subject", identity.Subject, "error", err)
		}
	}
}

// subjectAccountID extracts the account ID from "https://instance.example:account_id".
func subjectAccountID(subject, instanceURL string) string {
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
			Provider: providerMastodon,
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
				Provider:    providerMastodon,
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
			Provider:    providerMastodon,
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

func (c *Client) redirectURI() string {
	return c.cfg.AtprotoPublicBaseURL + "/api/auth/mastodon/callback"
}

// buildHandle builds "user@instance" from username and instance URL.
func buildHandle(username, instanceURL string) string {
	u, err := url.Parse(instanceURL)
	if err != nil || u.Host == "" {
		return username
	}
	return username + "@" + u.Host
}

func codeChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
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
