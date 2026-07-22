package atproto

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
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
	providerAtproto       = "atproto"
	oauthStateTTL         = 10 * time.Minute
	profileStaleAfterHour = 24
	profileSyncInterval   = 6 * time.Hour
)

var ErrInvalidOAuthConfig = errors.New("invalid atproto oauth config")

type Client struct {
	cfg  config.Config
	pool *pgxpool.Pool
	q    *db.Queries
	http *http.Client
}

type authServerMetadata struct {
	Issuer                             string `json:"issuer"`
	AuthorizationEndpoint              string `json:"authorization_endpoint"`
	TokenEndpoint                      string `json:"token_endpoint"`
	PushedAuthorizationRequestEndpoint string `json:"pushed_authorization_request_endpoint"`
}

type parResponse struct {
	RequestURI string `json:"request_uri"`
	ExpiresIn  int    `json:"expires_in"`
}

// didDocument は DID ドキュメントのうち、PDS 解決に必要な部分だけを表す。
type didDocument struct {
	ID      string `json:"id"`
	Service []struct {
		ID              string `json:"id"`
		Type            string `json:"type"`
		ServiceEndpoint string `json:"serviceEndpoint"`
	} `json:"service"`
}

// protectedResourceMetadata は PDS の oauth-protected-resource メタデータ。
type protectedResourceMetadata struct {
	AuthorizationServers []string `json:"authorization_servers"`
}

type tokenResponse struct {
	Sub string `json:"sub"`
}

type ActorProfile struct {
	DID         string
	Handle      string
	DisplayName string
	AvatarURL   string
	Raw         []byte
}

type privateJWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	D   string `json:"d"`
}

type publicJWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

func NewClient(cfg config.Config, pool *pgxpool.Pool) *Client {
	return &Client{
		cfg:  cfg,
		pool: pool,
		q:    db.New(pool),
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) ClientMetadata() map[string]any {
	clientID := c.clientID()
	return map[string]any{
		"client_id":                  clientID,
		"application_type":           "web",
		"client_name":                c.cfg.ServiceName,
		"client_uri":                 c.cfg.AtprotoPublicBaseURL,
		"dpop_bound_access_tokens":   true,
		"grant_types":                []string{"authorization_code"},
		"redirect_uris":              []string{c.redirectURI()},
		"response_types":             []string{"code"},
		"scope":                      c.cfg.AtprotoOAuthScope,
		"token_endpoint_auth_method": "none",
	}
}

func (c *Client) StartOAuth(ctx context.Context, identifier string) (string, error) {
	identifier = normalizeIdentifier(identifier)
	if identifier == "" {
		return "", errors.New("identifier is required")
	}
	if err := c.validateConfig(); err != nil {
		return "", err
	}
	if err := c.q.DeleteExpiredAtprotoOAuthStates(ctx); err != nil {
		slog.Warn("delete expired atproto oauth states failed", "error", err)
	}

	// identifier(ハンドル/DID) から DID を解決し、その DID ドキュメントが指す PDS、
	// さらに PDS が委任する認可サーバを辿る。これにより bsky.social に限らず
	// 独自PDSでもログインできる。
	expectedDID, authIssuer, err := c.resolveIdentity(ctx, identifier)
	if err != nil {
		return "", err
	}
	metadata, err := c.fetchAuthServerMetadata(ctx, authIssuer)
	if err != nil {
		return "", err
	}
	state, err := randomToken()
	if err != nil {
		return "", err
	}
	codeVerifier, err := randomToken()
	if err != nil {
		return "", err
	}
	key, jwk, err := generateJWK()
	if err != nil {
		return "", err
	}
	jwkJSON, err := json.Marshal(jwk)
	if err != nil {
		return "", err
	}

	form := url.Values{}
	form.Set("client_id", c.clientID())
	form.Set("redirect_uri", c.redirectURI())
	form.Set("response_type", "code")
	form.Set("scope", c.cfg.AtprotoOAuthScope)
	form.Set("state", state)
	form.Set("code_challenge", codeChallenge(codeVerifier))
	form.Set("code_challenge_method", "S256")
	form.Set("login_hint", identifier)

	body, nonce, status, err := c.postFormDPoP(ctx, metadata.PushedAuthorizationRequestEndpoint, form, key, publicFromPrivate(jwk), "")
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("atproto PAR failed: status=%d body=%s", status, truncate(body, 300))
	}
	var par parResponse
	if err := json.Unmarshal(body, &par); err != nil {
		return "", err
	}
	if par.RequestURI == "" {
		return "", errors.New("atproto PAR response missing request_uri")
	}

	if _, err := c.q.CreateAtprotoOAuthState(ctx, db.CreateAtprotoOAuthStateParams{
		State:                              state,
		Issuer:                             authIssuer,
		AuthServerIssuer:                   metadata.Issuer,
		AuthorizationEndpoint:              metadata.AuthorizationEndpoint,
		TokenEndpoint:                      metadata.TokenEndpoint,
		PushedAuthorizationRequestEndpoint: metadata.PushedAuthorizationRequestEndpoint,
		CodeVerifier:                       codeVerifier,
		DpopPrivateJwk:                     jwkJSON,
		DpopNonce:                          nullableText(nonce),
		LoginHint:                          nullableText(identifier),
		ExpectedDid:                        nullableText(expectedDID),
		ExpiresAt:                          time.Now().Add(oauthStateTTL),
	}); err != nil {
		return "", err
	}

	authURL, err := url.Parse(metadata.AuthorizationEndpoint)
	if err != nil {
		return "", err
	}
	values := authURL.Query()
	values.Set("client_id", c.clientID())
	values.Set("request_uri", par.RequestURI)
	authURL.RawQuery = values.Encode()
	return authURL.String(), nil
}

func (c *Client) CompleteOAuth(ctx context.Context, state, issuer, code string) (db.User, error) {
	if state == "" || code == "" {
		return db.User{}, errors.New("state and code are required")
	}
	saved, err := c.q.GetAtprotoOAuthState(ctx, state)
	if err != nil {
		return db.User{}, err
	}
	defer func() {
		if err := c.q.DeleteAtprotoOAuthState(context.Background(), state); err != nil {
			slog.Warn("delete atproto oauth state failed", "error", err)
		}
	}()
	if time.Now().After(saved.ExpiresAt) {
		return db.User{}, errors.New("atproto oauth state expired")
	}
	if issuer != "" && normalizeURL(issuer) != normalizeURL(saved.AuthServerIssuer) {
		return db.User{}, errors.New("atproto oauth issuer mismatch")
	}

	key, jwk, err := parsePrivateJWK(saved.DpopPrivateJwk)
	if err != nil {
		return db.User{}, err
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", c.clientID())
	form.Set("redirect_uri", c.redirectURI())
	form.Set("code", code)
	form.Set("code_verifier", saved.CodeVerifier)

	body, _, status, err := c.postFormDPoP(ctx, saved.TokenEndpoint, form, key, jwk, textValue(saved.DpopNonce))
	if err != nil {
		return db.User{}, err
	}
	if status < 200 || status >= 300 {
		return db.User{}, fmt.Errorf("atproto token exchange failed: status=%d body=%s", status, truncate(body, 300))
	}
	var token tokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return db.User{}, err
	}
	if !strings.HasPrefix(token.Sub, "did:") {
		return db.User{}, errors.New("atproto token response missing DID subject")
	}
	if saved.ExpectedDid.Valid && token.Sub != saved.ExpectedDid.String {
		return db.User{}, errors.New("atproto token subject does not match requested DID")
	}
	profile, err := c.FetchProfile(ctx, token.Sub)
	if err != nil {
		return db.User{}, err
	}
	if profile.DID != "" && profile.DID != token.Sub {
		return db.User{}, errors.New("atproto profile DID mismatch")
	}
	profile.DID = token.Sub
	return c.upsertProfile(ctx, profile, true)
}

// resolveIdentity は identifier(ハンドル/DID) から、ログインに使う DID と、
// その利用者のPDSが委任する認可サーバのissuerを解決する。
func (c *Client) resolveIdentity(ctx context.Context, identifier string) (did string, authIssuer string, err error) {
	did, err = c.resolveDID(ctx, identifier)
	if err != nil {
		return "", "", err
	}
	pds, err := c.resolvePDS(ctx, did)
	if err != nil {
		return "", "", err
	}
	authIssuer, err = c.resolveAuthServer(ctx, pds)
	if err != nil {
		return "", "", err
	}
	return did, authIssuer, nil
}

// resolveDID はハンドルまたはDIDを DID へ解決する。
// ハンドルは atproto の標準手順（DNS TXT → HTTPS well-known）で解決し、
// いずれも失敗した場合のみ公開Appview（Blueskyにフェデレーション済みのアカウント）へフォールバックする。
func (c *Client) resolveDID(ctx context.Context, identifier string) (string, error) {
	if strings.HasPrefix(identifier, "did:") {
		return identifier, nil
	}
	if did := c.resolveHandleViaDNS(ctx, identifier); did != "" {
		return did, nil
	}
	if did := c.resolveHandleViaHTTP(ctx, identifier); did != "" {
		return did, nil
	}
	profile, err := c.FetchProfile(ctx, identifier)
	if err != nil {
		return "", fmt.Errorf("resolve handle %q failed: %w", identifier, err)
	}
	if profile.DID == "" {
		return "", fmt.Errorf("could not resolve handle %q to a DID", identifier)
	}
	return profile.DID, nil
}

// resolveHandleViaDNS は _atproto.<handle> の TXT レコード（did=...）からDIDを解決する。
func (c *Client) resolveHandleViaDNS(ctx context.Context, handle string) string {
	records, err := net.DefaultResolver.LookupTXT(ctx, "_atproto."+handle)
	if err != nil {
		return ""
	}
	for _, rec := range records {
		rec = strings.TrimSpace(rec)
		if after, ok := strings.CutPrefix(rec, "did="); ok {
			did := strings.TrimSpace(after)
			if strings.HasPrefix(did, "did:") {
				return did
			}
		}
	}
	return ""
}

// resolveHandleViaHTTP は https://<handle>/.well-known/atproto-did からDIDを解決する。
func (c *Client) resolveHandleViaHTTP(ctx context.Context, handle string) string {
	endpoint := "https://" + handle + "/.well-known/atproto-did"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ""
	}
	res, err := c.http.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	if err != nil {
		return ""
	}
	did := strings.TrimSpace(string(body))
	if strings.HasPrefix(did, "did:") {
		return did
	}
	return ""
}

// resolvePDS は DID ドキュメントを取得し、atproto PDS のサービスエンドポイントを返す。
func (c *Client) resolvePDS(ctx context.Context, did string) (string, error) {
	doc, err := c.fetchDIDDocument(ctx, did)
	if err != nil {
		return "", err
	}
	if doc.ID != "" && doc.ID != did {
		return "", fmt.Errorf("DID document id mismatch: got %s want %s", doc.ID, did)
	}
	for _, svc := range doc.Service {
		if strings.HasSuffix(svc.ID, "#atproto_pds") || svc.Type == "AtprotoPersonalDataServer" {
			endpoint := strings.TrimRight(strings.TrimSpace(svc.ServiceEndpoint), "/")
			if endpoint != "" {
				return endpoint, nil
			}
		}
	}
	return "", fmt.Errorf("DID document %q has no atproto PDS service endpoint", did)
}

// fetchDIDDocument は did:plc / did:web の DID ドキュメントを取得する。
func (c *Client) fetchDIDDocument(ctx context.Context, did string) (didDocument, error) {
	var endpoint string
	switch {
	case strings.HasPrefix(did, "did:plc:"):
		endpoint = c.cfg.AtprotoPLCDirectoryURL + "/" + did
	case strings.HasPrefix(did, "did:web:"):
		u, err := didWebToURL(did)
		if err != nil {
			return didDocument{}, err
		}
		endpoint = u
	default:
		return didDocument{}, fmt.Errorf("unsupported DID method: %s", did)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return didDocument{}, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return didDocument{}, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return didDocument{}, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return didDocument{}, fmt.Errorf("atproto DID document fetch failed: status=%d body=%s", res.StatusCode, truncate(body, 300))
	}
	var doc didDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return didDocument{}, err
	}
	return doc, nil
}

// resolveAuthServer は PDS の保護リソースメタデータから、委任先の認可サーバのissuerを返す。
func (c *Client) resolveAuthServer(ctx context.Context, pds string) (string, error) {
	endpoint := pds + "/.well-known/oauth-protected-resource"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("atproto protected resource metadata failed: status=%d body=%s", res.StatusCode, truncate(body, 300))
	}
	var meta protectedResourceMetadata
	if err := json.Unmarshal(body, &meta); err != nil {
		return "", err
	}
	if len(meta.AuthorizationServers) == 0 || strings.TrimSpace(meta.AuthorizationServers[0]) == "" {
		return "", errors.New("atproto PDS protected resource metadata missing authorization_servers")
	}
	return normalizeURL(strings.TrimSpace(meta.AuthorizationServers[0])), nil
}

// didWebToURL は did:web を DID ドキュメントのURLに変換する。
// 例: did:web:example.com            → https://example.com/.well-known/did.json
//
//	did:web:example.com:u:alice    → https://example.com/u/alice/did.json
//	did:web:example.com%3A3000     → https://example.com:3000/.well-known/did.json
func didWebToURL(did string) (string, error) {
	rest := strings.TrimPrefix(did, "did:web:")
	if rest == "" {
		return "", errors.New("invalid did:web")
	}
	parts := strings.Split(rest, ":")
	host, err := url.PathUnescape(parts[0])
	if err != nil {
		return "", fmt.Errorf("invalid did:web host: %w", err)
	}
	if host == "" {
		return "", errors.New("invalid did:web host")
	}
	if len(parts) == 1 {
		return "https://" + host + "/.well-known/did.json", nil
	}
	segs := make([]string, 0, len(parts)-1)
	for _, p := range parts[1:] {
		s, err := url.PathUnescape(p)
		if err != nil {
			return "", fmt.Errorf("invalid did:web path: %w", err)
		}
		segs = append(segs, s)
	}
	return "https://" + host + "/" + strings.Join(segs, "/") + "/did.json", nil
}

func (c *Client) FetchProfile(ctx context.Context, actor string) (ActorProfile, error) {
	endpoint, err := url.Parse(c.cfg.AtprotoPublicAPIURL + "/xrpc/app.bsky.actor.getProfile")
	if err != nil {
		return ActorProfile{}, err
	}
	values := endpoint.Query()
	values.Set("actor", actor)
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
		return ActorProfile{}, fmt.Errorf("atproto profile fetch failed: status=%d body=%s", res.StatusCode, truncate(body, 300))
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return ActorProfile{}, err
	}
	return ActorProfile{
		DID:         stringField(raw, "did"),
		Handle:      stringField(raw, "handle"),
		DisplayName: stringField(raw, "displayName"),
		AvatarURL:   stringField(raw, "avatar"),
		Raw:         body,
	}, nil
}

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
	identities, err := c.q.ListStaleAtprotoIdentities(ctx, db.ListStaleAtprotoIdentitiesParams{
		StaleAfterHours: profileStaleAfterHour,
		Limit:           100,
	})
	if err != nil {
		slog.Warn("list stale atproto identities failed", "error", err)
		return
	}
	for _, identity := range identities {
		profile, err := c.FetchProfile(ctx, identity.Subject)
		if err != nil {
			slog.Warn("sync atproto profile failed", "subject", identity.Subject, "error", err)
			continue
		}
		if profile.DID == "" {
			profile.DID = identity.Subject
		}
		if profile.DID != identity.Subject {
			slog.Warn("skip atproto profile sync due to DID mismatch", "subject", identity.Subject, "profile_did", profile.DID)
			continue
		}
		if _, err := c.upsertProfile(ctx, profile, false); err != nil {
			slog.Warn("update atproto profile failed", "subject", identity.Subject, "error", err)
		}
	}
}

func (c *Client) upsertProfile(ctx context.Context, profile ActorProfile, verified bool) (db.User, error) {
	displayName := strings.TrimSpace(profile.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(profile.Handle)
	}
	if displayName == "" {
		displayName = profile.DID
	}
	profileURL := "https://bsky.app/profile/" + profile.DID
	if profile.Handle != "" {
		profileURL = "https://bsky.app/profile/" + profile.Handle
	}

	var out db.User
	err := store.WithTx(ctx, c.pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		identity, err := q.GetAuthIdentityByProviderSubject(ctx, db.GetAuthIdentityByProviderSubjectParams{
			Provider: providerAtproto,
			Subject:  profile.DID,
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if errors.Is(err, pgx.ErrNoRows) {
			user, err := q.CreateUser(ctx, db.CreateUserParams{
				DisplayName: displayName,
				Handle:      nullableText(profile.Handle),
				AvatarUrl:   nullableText(profile.AvatarURL),
			})
			if err != nil {
				return err
			}
			if _, err := q.CreateAuthIdentity(ctx, db.CreateAuthIdentityParams{
				UserID:      user.ID,
				Provider:    providerAtproto,
				Subject:     profile.DID,
				Handle:      nullableText(profile.Handle),
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
			Handle:      nullableText(profile.Handle),
			AvatarUrl:   nullableText(profile.AvatarURL),
		})
		if err != nil {
			return err
		}
		if _, err := q.UpdateAuthIdentityProfileByProviderSubject(ctx, db.UpdateAuthIdentityProfileByProviderSubjectParams{
			Provider:    providerAtproto,
			Subject:     profile.DID,
			UserID:      user.ID,
			Handle:      nullableText(profile.Handle),
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

func (c *Client) fetchAuthServerMetadata(ctx context.Context, issuer string) (authServerMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, issuer+"/.well-known/oauth-authorization-server", nil)
	if err != nil {
		return authServerMetadata{}, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return authServerMetadata{}, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return authServerMetadata{}, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return authServerMetadata{}, fmt.Errorf("atproto auth metadata failed: status=%d body=%s", res.StatusCode, truncate(body, 300))
	}
	var metadata authServerMetadata
	if err := json.Unmarshal(body, &metadata); err != nil {
		return authServerMetadata{}, err
	}
	if metadata.Issuer == "" || metadata.AuthorizationEndpoint == "" || metadata.TokenEndpoint == "" || metadata.PushedAuthorizationRequestEndpoint == "" {
		return authServerMetadata{}, errors.New("atproto auth metadata missing required endpoints")
	}
	// 認可サーバのメタデータは、要求したissuerと一致していなければならない（RFC 8414）。
	if normalizeURL(metadata.Issuer) != normalizeURL(issuer) {
		return authServerMetadata{}, fmt.Errorf("atproto auth metadata issuer mismatch: got %s want %s", metadata.Issuer, issuer)
	}
	return metadata, nil
}

func (c *Client) postFormDPoP(ctx context.Context, endpoint string, form url.Values, key *ecdsa.PrivateKey, jwk publicJWK, nonce string) ([]byte, string, int, error) {
	body, nextNonce, status, err := c.doPostFormDPoP(ctx, endpoint, form, key, jwk, nonce)
	if err != nil {
		return nil, "", 0, err
	}
	if (status == http.StatusBadRequest || status == http.StatusUnauthorized) && nextNonce != "" && nextNonce != nonce {
		return c.doPostFormDPoP(ctx, endpoint, form, key, jwk, nextNonce)
	}
	return body, nextNonce, status, nil
}

func (c *Client) doPostFormDPoP(ctx context.Context, endpoint string, form url.Values, key *ecdsa.PrivateKey, jwk publicJWK, nonce string) ([]byte, string, int, error) {
	proof, err := signDPoP(key, jwk, http.MethodPost, endpoint, nonce)
	if err != nil {
		return nil, "", 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("DPoP", proof)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, "", 0, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, "", 0, err
	}
	return body, res.Header.Get("DPoP-Nonce"), res.StatusCode, nil
}

func signDPoP(key *ecdsa.PrivateKey, jwk publicJWK, method, htu, nonce string) (string, error) {
	header := map[string]any{
		"typ": "dpop+jwt",
		"alg": "ES256",
		"jwk": jwk,
	}
	jti, err := randomToken()
	if err != nil {
		return "", err
	}
	claims := map[string]any{
		"jti": jti,
		"htm": method,
		"htu": htu,
		"iat": time.Now().Unix(),
	}
	if nonce != "" {
		claims["nonce"] = nonce
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	sum := sha256.Sum256([]byte(unsigned))
	r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
	if err != nil {
		return "", err
	}
	signature := append(pad32(r.Bytes()), pad32(s.Bytes())...)
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func generateJWK() (*ecdsa.PrivateKey, privateJWK, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, privateJWK{}, err
	}
	jwk := privateJWK{
		Kty: "EC",
		Crv: "P-256",
		X:   base64.RawURLEncoding.EncodeToString(pad32(key.X.Bytes())),
		Y:   base64.RawURLEncoding.EncodeToString(pad32(key.Y.Bytes())),
		D:   base64.RawURLEncoding.EncodeToString(pad32(key.D.Bytes())),
	}
	return key, jwk, nil
}

func parsePrivateJWK(data []byte) (*ecdsa.PrivateKey, publicJWK, error) {
	var jwk privateJWK
	if err := json.Unmarshal(data, &jwk); err != nil {
		return nil, publicJWK{}, err
	}
	xb, err := base64.RawURLEncoding.DecodeString(jwk.X)
	if err != nil {
		return nil, publicJWK{}, err
	}
	yb, err := base64.RawURLEncoding.DecodeString(jwk.Y)
	if err != nil {
		return nil, publicJWK{}, err
	}
	db, err := base64.RawURLEncoding.DecodeString(jwk.D)
	if err != nil {
		return nil, publicJWK{}, err
	}
	curve := elliptic.P256()
	x := new(big.Int).SetBytes(xb)
	y := new(big.Int).SetBytes(yb)
	if !curve.IsOnCurve(x, y) {
		return nil, publicJWK{}, errors.New("invalid atproto DPoP key")
	}
	key := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: curve, X: x, Y: y},
		D:         new(big.Int).SetBytes(db),
	}
	return key, publicFromPrivate(jwk), nil
}

func publicFromPrivate(jwk privateJWK) publicJWK {
	return publicJWK{Kty: jwk.Kty, Crv: jwk.Crv, X: jwk.X, Y: jwk.Y}
}

func (c *Client) validateConfig() error {
	if c.cfg.AtprotoPublicBaseURL == "" || c.cfg.AtprotoAuthServerURL == "" || c.cfg.AtprotoPublicAPIURL == "" || c.cfg.AtprotoOAuthScope == "" {
		return ErrInvalidOAuthConfig
	}
	return nil
}

func (c *Client) clientID() string {
	return c.cfg.AtprotoPublicBaseURL + "/oauth/atproto/client-metadata.json"
}

func (c *Client) redirectURI() string {
	return c.cfg.AtprotoPublicBaseURL + "/api/auth/atproto/callback"
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

func normalizeIdentifier(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "@")
	if v != "" && !strings.HasPrefix(v, "did:") && !strings.Contains(v, ".") {
		v = v + ".bsky.social"
	}
	return v
}

func normalizeURL(v string) string {
	return strings.TrimRight(v, "/")
}

func nullableText(v string) pgtype.Text {
	v = strings.TrimSpace(v)
	if v == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: v, Valid: true}
}

func textValue(v pgtype.Text) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

func stringField(raw map[string]any, key string) string {
	v, _ := raw[key].(string)
	return strings.TrimSpace(v)
}

func pad32(in []byte) []byte {
	if len(in) >= 32 {
		return in
	}
	out := make([]byte, 32)
	copy(out[32-len(in):], in)
	return out
}

func truncate(in []byte, max int) string {
	if len(in) <= max {
		return string(in)
	}
	return string(in[:max]) + "..."
}
