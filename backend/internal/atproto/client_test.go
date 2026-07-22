package atproto

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mamemomonga/mamechat/backend/internal/config"
)

func TestDidWebToURL(t *testing.T) {
	cases := []struct {
		did  string
		want string
	}{
		{"did:web:example.com", "https://example.com/.well-known/did.json"},
		{"did:web:example.com:u:alice", "https://example.com/u/alice/did.json"},
		{"did:web:example.com%3A3000", "https://example.com:3000/.well-known/did.json"},
	}
	for _, tc := range cases {
		got, err := didWebToURL(tc.did)
		if err != nil {
			t.Fatalf("didWebToURL(%q) error: %v", tc.did, err)
		}
		if got != tc.want {
			t.Errorf("didWebToURL(%q) = %q, want %q", tc.did, got, tc.want)
		}
	}

	if _, err := didWebToURL("did:web:"); err == nil {
		t.Error("expected error for empty did:web host")
	}
}

// 独自PDSのログイン解決: DID → PDS → 認可サーバ を、モックのPLCディレクトリ兼PDSで検証する。
func TestResolveIdentityCustomPDS(t *testing.T) {
	var serverURL string
	mux := http.NewServeMux()
	// PLCディレクトリ: DID ドキュメントを返す。service で自分自身をPDSとして指す。
	mux.HandleFunc("/did:plc:testcustompds", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "did:plc:testcustompds",
			"service": []map[string]any{
				{
					"id":              "#atproto_pds",
					"type":            "AtprotoPersonalDataServer",
					"serviceEndpoint": serverURL,
				},
			},
		})
	})
	// PDSの保護リソースメタデータ: 認可サーバとして自分自身を委任する。
	mux.HandleFunc("/.well-known/oauth-protected-resource", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"authorization_servers": []string{serverURL},
		})
	})
	// 認可サーバのメタデータ。
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                serverURL,
			"authorization_endpoint":                serverURL + "/authorize",
			"token_endpoint":                        serverURL + "/token",
			"pushed_authorization_request_endpoint": serverURL + "/par",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	serverURL = srv.URL

	c := &Client{
		cfg:  config.Config{AtprotoPLCDirectoryURL: srv.URL},
		http: &http.Client{Timeout: 5 * time.Second},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	did, authIssuer, err := c.resolveIdentity(ctx, "did:plc:testcustompds")
	if err != nil {
		t.Fatalf("resolveIdentity error: %v", err)
	}
	if did != "did:plc:testcustompds" {
		t.Errorf("did = %q, want did:plc:testcustompds", did)
	}
	if authIssuer != normalizeURL(srv.URL) {
		t.Errorf("authIssuer = %q, want %q", authIssuer, normalizeURL(srv.URL))
	}

	// 解決した issuer でメタデータを取得できること（issuer一致チェックを含む）。
	meta, err := c.fetchAuthServerMetadata(ctx, authIssuer)
	if err != nil {
		t.Fatalf("fetchAuthServerMetadata error: %v", err)
	}
	if meta.PushedAuthorizationRequestEndpoint != serverURL+"/par" {
		t.Errorf("PAR endpoint = %q, want %q", meta.PushedAuthorizationRequestEndpoint, serverURL+"/par")
	}
}

// PDSがPDSサービスエンドポイントを持たないDIDドキュメントはエラーになること。
func TestResolvePDSMissingService(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/did:plc:nopds", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "did:plc:nopds", "service": []any{}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &Client{
		cfg:  config.Config{AtprotoPLCDirectoryURL: srv.URL},
		http: &http.Client{Timeout: 5 * time.Second},
	}
	if _, err := c.resolvePDS(context.Background(), "did:plc:nopds"); err == nil {
		t.Error("expected error when DID document has no PDS service endpoint")
	}
}
