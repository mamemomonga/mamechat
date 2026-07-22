package config

import (
	"errors"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultActivePollSeconds   = 15
	minActivePollSeconds       = 5
	maxActivePollSeconds       = 300
	defaultBeaconSeconds       = 10
	minBeaconSeconds           = 5
	maxBeaconSeconds           = 60
	defaultActiveWindowSeconds = 30
	maxActiveWindowSeconds     = 300
	activeWindowBeaconFactor   = 3
)

type Config struct {
	AppEnv               string
	Version              string
	ServiceName          string
	HTTPAddr             string
	DatabaseURL          string
	RedisURL             string
	SessionCookieName    string
	SessionTTL           time.Duration
	CORSAllowedOrigin    string
	SecureSessionCookie  bool
	OwnerPassword        string
	OwnerDisplayName     string
	OwnerHandle          string
	OwnerAvatarURL       string
	AtprotoPublicBaseURL string
	AtprotoAuthServerURL string
	AtprotoPublicAPIURL  string
	// AtprotoPLCDirectoryURL は did:plc の DID ドキュメントを解決する PLC ディレクトリ。
	// 独自PDSのログインでは DID から PDS・認可サーバを解決するために使う。
	AtprotoPLCDirectoryURL string
	AtprotoOAuthScope      string
	AtprotoRedirectURL     string
	MastodonRedirectURL    string
	MisskeyRedirectURL     string
	MessageMaxLength       int
	// ActivePollSeconds はチャンネル一覧が在室/アクティブ状態を反映するために再取得する間隔（秒）。
	ActivePollSeconds int
	// BeaconSeconds はクライアントが「アクティブ」を申告するビーコンの送信間隔（秒）。
	BeaconSeconds int
	// ActiveWindow はアクティブ判定の有効期間。この期間ビーコンが無いと非アクティブになる。
	ActiveWindow time.Duration

	ChannelSuspendRetention time.Duration // サスペンド後に削除するまでの期間
	ChannelSuspendGrace     time.Duration // オーナー離席からサスペンドするまでの猶予（0=無期限＝離席で自動閉店しない）

	UploadStorageDir      string        // 添付画像・動画の永続化ディレクトリ
	UploadMaxBytes        int64         // アップロード画像の最大バイト数
	UploadMediaMaxBytes   int64         // アップロード動画素材(GIF/APNG/MP4)の最大バイト数
	UploadVideoMaxSeconds int           // アップロードMP4の最大長さ（秒）
	UploadStagingTTL      time.Duration // アップロード後・未投稿の画像を保持する時間

	// Web Push (VAPID)。公開鍵・秘密鍵が両方設定されているときのみ Push 機能を有効化する。
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string

	TTSEnabled               bool
	TTSStorageDir            string
	TTSVoicevoxURLs          []string
	TTSVoicevoxEngineVersion string
	TTSMaxPendingPerChannel  int
	TTSWorkerConcurrency     int
	TTSGCInterval            time.Duration
}

func Load() Config {
	appEnv := getenv("APP_ENV", "development")
	ttlHours := getenvInt("SESSION_TTL_HOURS", 2160)
	secureCookie := appEnv != "development"
	if v := os.Getenv("SESSION_COOKIE_SECURE"); v != "" {
		secureCookie = strings.EqualFold(v, "true") || v == "1"
	}
	messageMaxLength := getenvInt("MESSAGE_MAX_LENGTH", 400)
	if messageMaxLength <= 0 {
		messageMaxLength = 400
	}
	activePollSeconds := clampEnvInt("ACTIVE_POLL_SECONDS", defaultActivePollSeconds, minActivePollSeconds, maxActivePollSeconds)
	beaconSeconds := clampEnvInt("BEACON_SECONDS", defaultBeaconSeconds, minBeaconSeconds, maxBeaconSeconds)
	activeWindowSeconds := clampEnvInt("ACTIVE_WINDOW_SECONDS", defaultActiveWindowSeconds, 1, maxActiveWindowSeconds)
	minActiveWindowSeconds := beaconSeconds * activeWindowBeaconFactor
	if activeWindowSeconds < minActiveWindowSeconds {
		slog.Warn("ACTIVE_WINDOW_SECONDS is too short for BEACON_SECONDS; adjusted", "configured", activeWindowSeconds, "adjusted", minActiveWindowSeconds, "beacon_seconds", beaconSeconds)
		activeWindowSeconds = minActiveWindowSeconds
	}
	ttsVoicevoxURLs := splitURLs(getenv("TTS_VOICEVOX_URLS", "http://voicevox:50021"))
	if len(ttsVoicevoxURLs) == 0 {
		ttsVoicevoxURLs = []string{"http://voicevox:50021"}
	}
	ttsWorkerConcurrency := getenvInt("TTS_WORKER_CONCURRENCY", 1)
	if ttsWorkerConcurrency <= 0 {
		ttsWorkerConcurrency = 1
	}

	return Config{
		AppEnv:                 appEnv,
		Version:                loadVersion(),
		ServiceName:            getenv("SERVICE_NAME", "mamechat"),
		HTTPAddr:               getenv("HTTP_ADDR", ":8080"),
		DatabaseURL:            getenv("DATABASE_URL", "postgres://app:app@localhost:5432/app?sslmode=disable"),
		RedisURL:               getenv("REDIS_URL", "redis://localhost:6379/0"),
		SessionCookieName:      getenv("SESSION_COOKIE_NAME", "app_session"),
		SessionTTL:             time.Duration(ttlHours) * time.Hour,
		CORSAllowedOrigin:      getenv("CORS_ALLOWED_ORIGIN", "http://localhost:5173"),
		SecureSessionCookie:    secureCookie,
		OwnerPassword:          os.Getenv("OWNER_PASSWORD"),
		OwnerDisplayName:       getenv("OWNER_DISPLAY_NAME", "Owner"),
		OwnerHandle:            getenv("OWNER_HANDLE", "owner.local"),
		OwnerAvatarURL:         os.Getenv("OWNER_AVATAR_URL"),
		AtprotoPublicBaseURL:   strings.TrimRight(getenv("ATPROTO_PUBLIC_BASE_URL", "http://localhost:8080"), "/"),
		AtprotoAuthServerURL:   strings.TrimRight(getenv("ATPROTO_AUTH_SERVER_URL", "https://bsky.social"), "/"),
		AtprotoPublicAPIURL:    strings.TrimRight(getenv("ATPROTO_PUBLIC_API_URL", "https://public.api.bsky.app"), "/"),
		AtprotoPLCDirectoryURL: strings.TrimRight(getenv("ATPROTO_PLC_DIRECTORY_URL", "https://plc.directory"), "/"),
		AtprotoOAuthScope:      getenv("ATPROTO_OAUTH_SCOPE", "atproto transition:generic"),
		AtprotoRedirectURL:     strings.TrimRight(getenv("ATPROTO_LOGIN_REDIRECT_URL", getenv("CORS_ALLOWED_ORIGIN", "http://localhost:5173")), "/"),
		MastodonRedirectURL:    strings.TrimRight(getenv("MASTODON_LOGIN_REDIRECT_URL", getenv("CORS_ALLOWED_ORIGIN", "http://localhost:5173")), "/"),
		MisskeyRedirectURL:     strings.TrimRight(getenv("MISSKEY_LOGIN_REDIRECT_URL", getenv("CORS_ALLOWED_ORIGIN", "http://localhost:5173")), "/"),
		MessageMaxLength:       messageMaxLength,
		ActivePollSeconds:      activePollSeconds,
		BeaconSeconds:          beaconSeconds,
		ActiveWindow:           time.Duration(activeWindowSeconds) * time.Second,

		ChannelSuspendRetention: time.Duration(getenvInt("CHANNEL_SUSPEND_RETENTION_HOURS", 24)) * time.Hour,
		ChannelSuspendGrace:     time.Duration(getenvInt("CHANNEL_SUSPEND_GRACE_MINUTES", 0)) * time.Minute,

		UploadStorageDir:      getenv("UPLOAD_STORAGE_DIR", "/storage/uploads"),
		UploadMaxBytes:        int64(getenvInt("UPLOAD_MAX_BYTES", 5*1024*1024)),
		UploadMediaMaxBytes:   int64(getenvInt("UPLOAD_MEDIA_MAX_BYTES", 20*1024*1024)),
		UploadVideoMaxSeconds: getenvInt("UPLOAD_VIDEO_MAX_SECONDS", 30),
		UploadStagingTTL:      time.Duration(getenvInt("UPLOAD_STAGING_TTL_MINUTES", 60)) * time.Minute,

		VAPIDPublicKey:  os.Getenv("VAPID_PUBLIC_KEY"),
		VAPIDPrivateKey: os.Getenv("VAPID_PRIVATE_KEY"),
		VAPIDSubject:    getenv("VAPID_SUBJECT", "mailto:admin@localhost"),

		TTSEnabled:               getenvBool("TTS_ENABLED", true),
		TTSStorageDir:            getenv("TTS_STORAGE_DIR", "/storage/tts"),
		TTSVoicevoxURLs:          ttsVoicevoxURLs,
		TTSVoicevoxEngineVersion: getenv("TTS_VOICEVOX_ENGINE_VERSION", "voicevox-unknown"),
		TTSMaxPendingPerChannel:  getenvInt("TTS_MAX_PENDING_PER_CHANNEL", 5),
		TTSWorkerConcurrency:     ttsWorkerConcurrency,
		TTSGCInterval:            time.Duration(getenvInt("TTS_GC_INTERVAL_MINUTES", 60)) * time.Minute,
	}
}

func (c Config) Validate() error {
	if c.OwnerPassword == "" {
		return errors.New("OWNER_PASSWORD is required")
	}
	return nil
}

// PushEnabled は Web Push が利用可能か（VAPID 公開鍵・秘密鍵の両方が設定済みか）を返す。
func (c Config) PushEnabled() bool {
	return c.VAPIDPublicKey != "" && c.VAPIDPrivateKey != ""
}

// loadVersion はアプリのバージョンを取得する。APP_VERSION 環境変数を優先し、
// 無ければ commitHash付きの version.full（make versionが生成）を、それも無ければ
// ベースの version（X.X.X）を読む。
func loadVersion() string {
	if v := strings.TrimSpace(os.Getenv("APP_VERSION")); v != "" {
		return v
	}
	// 実行ディレクトリ（コンテナは /app/backend、ローカルは backend/）からの相対候補。
	candidates := []string{
		"version.full", "../version.full", "/app/version.full",
		"version", "../version", "/app/version",
	}
	for _, p := range candidates {
		if b, err := os.ReadFile(p); err == nil {
			if s := strings.TrimSpace(string(b)); s != "" {
				return s
			}
		}
	}
	return "dev"
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func clampEnvInt(key string, fallback, minValue, maxValue int) int {
	n := getenvInt(key, fallback)
	if n < minValue {
		slog.Warn("environment value is too small; adjusted", "key", key, "configured", n, "adjusted", minValue)
		return minValue
	}
	if n > maxValue {
		slog.Warn("environment value is too large; adjusted", "key", key, "configured", n, "adjusted", maxValue)
		return maxValue
	}
	return n
}

func getenvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return strings.EqualFold(v, "true") || v == "1" || strings.EqualFold(v, "yes")
}

func splitURLs(value string) []string {
	values := strings.Split(value, ",")
	urls := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimRight(strings.TrimSpace(value), "/")
		if value == "" {
			continue
		}
		urls = append(urls, value)
	}
	return urls
}
