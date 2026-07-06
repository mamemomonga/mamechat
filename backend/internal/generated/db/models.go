package db

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

type User struct {
	ID          int64       `json:"id"`
	DisplayName string      `json:"display_name"`
	Handle      pgtype.Text `json:"handle"`
	AvatarUrl   pgtype.Text `json:"avatar_url"`
	Status      string      `json:"status"`
	Role        string      `json:"role"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

type AuthIdentity struct {
	ID                    int64              `json:"id"`
	UserID                int64              `json:"user_id"`
	Provider              string             `json:"provider"`
	Subject               string             `json:"subject"`
	InstanceUrl           pgtype.Text        `json:"instance_url"`
	Handle                pgtype.Text        `json:"handle"`
	DisplayName           pgtype.Text        `json:"display_name"`
	AvatarUrl             pgtype.Text        `json:"avatar_url"`
	ProfileUrl            pgtype.Text        `json:"profile_url"`
	RawProfile            []byte             `json:"raw_profile"`
	LastVerifiedAt        pgtype.Timestamptz `json:"last_verified_at"`
	VerificationExpiresAt pgtype.Timestamptz `json:"verification_expires_at"`
	LastProfileSyncAt     pgtype.Timestamptz `json:"last_profile_sync_at"`
	CreatedAt             time.Time          `json:"created_at"`
	UpdatedAt             time.Time          `json:"updated_at"`
}

type AtprotoOauthState struct {
	State                              string      `json:"state"`
	Issuer                             string      `json:"issuer"`
	AuthServerIssuer                   string      `json:"auth_server_issuer"`
	AuthorizationEndpoint              string      `json:"authorization_endpoint"`
	TokenEndpoint                      string      `json:"token_endpoint"`
	PushedAuthorizationRequestEndpoint string      `json:"pushed_authorization_request_endpoint"`
	CodeVerifier                       string      `json:"code_verifier"`
	DpopPrivateJwk                     []byte      `json:"dpop_private_jwk"`
	DpopNonce                          pgtype.Text `json:"dpop_nonce"`
	LoginHint                          pgtype.Text `json:"login_hint"`
	ExpectedDid                        pgtype.Text `json:"expected_did"`
	CreatedAt                          time.Time   `json:"created_at"`
	ExpiresAt                          time.Time   `json:"expires_at"`
}

type MastodonOauthState struct {
	State        string    `json:"state"`
	InstanceUrl  string    `json:"instance_url"`
	CodeVerifier string    `json:"code_verifier"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
}

type MisskeyMiauthSession struct {
	Session     string    `json:"session"`
	InstanceUrl string    `json:"instance_url"`
	ExpiresAt   time.Time `json:"expires_at"`
	CreatedAt   time.Time `json:"created_at"`
}

type MastodonAppRegistration struct {
	ID           int64     `json:"id"`
	InstanceUrl  string    `json:"instance_url"`
	ClientID     string    `json:"client_id"`
	ClientSecret string    `json:"client_secret"`
	CreatedAt    time.Time `json:"created_at"`
}

type Session struct {
	ID         int64              `json:"id"`
	UserID     int64              `json:"user_id"`
	TokenHash  string             `json:"token_hash"`
	ExpiresAt  time.Time          `json:"expires_at"`
	LastSeenAt pgtype.Timestamptz `json:"last_seen_at"`
	UserAgent  pgtype.Text        `json:"user_agent"`
	IpPrefix   pgtype.Text        `json:"ip_prefix"`
	CreatedAt  time.Time          `json:"created_at"`
	RevokedAt  pgtype.Timestamptz `json:"revoked_at"`
}

type Channel struct {
	ID                    int64              `json:"id"`
	Slug                  string             `json:"slug"`
	Title                 string             `json:"title"`
	Description           pgtype.Text        `json:"description"`
	OwnerUserID           pgtype.Int8        `json:"owner_user_id"`
	SuspendedAt           pgtype.Timestamptz `json:"suspended_at"`
	SuspendRetentionHours pgtype.Int4        `json:"suspend_retention_hours"`
	SuspendGraceSeconds   pgtype.Int4        `json:"suspend_grace_seconds"`
	OperatingDeadline     pgtype.Timestamptz `json:"operating_deadline"`
	OperatingUnlimited    bool               `json:"operating_unlimited"`
	PostTtlHours          int32              `json:"post_ttl_hours"`
	CreatedAt             time.Time          `json:"created_at"`
	UpdatedAt             time.Time          `json:"updated_at"`
	UrlLinkifyEnabled     bool               `json:"url_linkify_enabled"`
	ImageUploadEnabled    bool               `json:"image_upload_enabled"`
	AccessMode            string             `json:"access_mode"`
	AccessList            []byte             `json:"access_list"`
}

type PushSubscription struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Endpoint  string    `json:"endpoint"`
	P256dh    string    `json:"p256dh"`
	Auth      string    `json:"auth"`
	CreatedAt time.Time `json:"created_at"`
}

type ChatMessage struct {
	ID                         int64       `json:"id"`
	ChannelID                  int64       `json:"channel_id"`
	UserID                     int64       `json:"user_id"`
	Body                       string      `json:"body"`
	UserDisplayName            string      `json:"user_display_name"`
	UserHandle                 pgtype.Text `json:"user_handle"`
	UserAvatarUrl              pgtype.Text `json:"user_avatar_url"`
	UserProvider               pgtype.Text `json:"user_provider"`
	UserTtsVoicevoxSpeakerUuid pgtype.Text `json:"user_tts_voicevox_speaker_uuid"`
	UserTtsVoicevoxSpeakerName pgtype.Text `json:"user_tts_voicevox_speaker_name"`
	UserTtsVoicevoxSpeakerUrl  pgtype.Text `json:"user_tts_voicevox_speaker_url"`
	CreatedAt                  time.Time   `json:"created_at"`
	ImagePath                  pgtype.Text `json:"image_path"`
	ImageWidth                 pgtype.Int4 `json:"image_width"`
	ImageHeight                pgtype.Int4 `json:"image_height"`
}

type UserSetting struct {
	UserID                 int64       `json:"user_id"`
	TtsVoicevoxSpeakerUuid pgtype.Text `json:"tts_voicevox_speaker_uuid"`
	CreatedAt              time.Time   `json:"created_at"`
	UpdatedAt              time.Time   `json:"updated_at"`
	GhostMode              bool        `json:"ghost_mode"`
}

type TTSAutoDictionaryEntry struct {
	TermKey            string      `json:"term_key"`
	Term               string      `json:"term"`
	Reading            string      `json:"reading"`
	RegisteredByUserID pgtype.Int8 `json:"registered_by_user_id"`
	RegisteredByHandle pgtype.Text `json:"registered_by_handle"`
	RegisteredAt       time.Time   `json:"registered_at"`
}

type TTSAsset struct {
	ContentHash           string             `json:"content_hash"`
	FilePath              string             `json:"file_path"`
	FileSizeBytes         int64              `json:"file_size_bytes"`
	DurationMs            pgtype.Int4        `json:"duration_ms"`
	TextPreview           pgtype.Text        `json:"text_preview"`
	TextLength            int32              `json:"text_length"`
	SpeakerID             int32              `json:"speaker_id"`
	SpeakerName           string             `json:"speaker_name"`
	SpeakerStyleName      pgtype.Text        `json:"speaker_style_name"`
	SpeedScale            float64            `json:"speed_scale"`
	PitchScale            float64            `json:"pitch_scale"`
	IntonationScale       float64            `json:"intonation_scale"`
	VolumeScale           float64            `json:"volume_scale"`
	PrePhonemeLength      float64            `json:"pre_phoneme_length"`
	PostPhonemeLength     float64            `json:"post_phoneme_length"`
	VoicevoxEngineVersion string             `json:"voicevox_engine_version"`
	Format                string             `json:"format"`
	Codec                 string             `json:"codec"`
	Bitrate               int32              `json:"bitrate"`
	Channels              int32              `json:"channels"`
	NormalizerVersion     string             `json:"normalizer_version"`
	SplitterVersion       string             `json:"splitter_version"`
	UseCount              int64              `json:"use_count"`
	CreatedAt             time.Time          `json:"created_at"`
	LastUsedAt            time.Time          `json:"last_used_at"`
	MarkedForDeleteAt     pgtype.Timestamptz `json:"marked_for_delete_at"`
}
