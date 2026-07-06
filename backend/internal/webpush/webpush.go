// Package webpush は Web Push (RFC 8291 aes128gcm) の送信と VAPID (RFC 8292) 署名を
// 標準ライブラリのみで実装する。外部依存を増やさないための最小実装。
package webpush

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var b64 = base64.RawURLEncoding

// Subscription はブラウザの PushManager が返す購読情報。
type Subscription struct {
	Endpoint string
	P256dh   string // UAの公開鍵（base64url, 非圧縮65バイト）
	Auth     string // 認証シークレット（base64url, 16バイト）
}

// GenerateVAPIDKeys は新しい VAPID 鍵ペアを生成し、base64url 文字列で返す。
// 公開鍵は非圧縮の楕円点（65バイト）、秘密鍵はスカラー（32バイト）。
func GenerateVAPIDKeys() (publicKey, privateKey string, err error) {
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return b64.EncodeToString(priv.PublicKey().Bytes()), b64.EncodeToString(priv.Bytes()), nil
}

// Sender は VAPID 鍵を保持し、購読へ通知を送る。
type Sender struct {
	subject    string
	publicB64  string
	privateKey *ecdsa.PrivateKey
	client     *http.Client
}

// NewSender は VAPID 公開鍵・秘密鍵（base64url）と subject（mailto: など）から Sender を作る。
func NewSender(publicB64, privateB64, subject string) (*Sender, error) {
	pubBytes, err := b64.DecodeString(strings.TrimSpace(publicB64))
	if err != nil || len(pubBytes) != 65 || pubBytes[0] != 0x04 {
		return nil, fmt.Errorf("invalid VAPID public key")
	}
	dBytes, err := b64.DecodeString(strings.TrimSpace(privateB64))
	if err != nil || len(dBytes) == 0 {
		return nil, fmt.Errorf("invalid VAPID private key")
	}
	priv := new(ecdsa.PrivateKey)
	priv.PublicKey.Curve = elliptic.P256()
	priv.D = new(big.Int).SetBytes(dBytes)
	priv.PublicKey.X = new(big.Int).SetBytes(pubBytes[1:33])
	priv.PublicKey.Y = new(big.Int).SetBytes(pubBytes[33:65])
	return &Sender{
		subject:    subject,
		publicB64:  strings.TrimSpace(publicB64),
		privateKey: priv,
		client:     &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// ErrSubscriptionGone は購読が失効（404/410）したことを表す。呼び出し側はDBから削除する。
type ErrSubscriptionGone struct{ Status int }

func (e *ErrSubscriptionGone) Error() string {
	return fmt.Sprintf("push subscription gone: status %d", e.Status)
}

// Send は payload（通常はJSON）を1件の購読へ暗号化して送信する。
func (s *Sender) Send(ctx context.Context, sub Subscription, payload []byte, ttlSeconds int) error {
	body, err := encrypt(sub, payload)
	if err != nil {
		return err
	}
	jwt, err := s.vapidToken(sub.Endpoint)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", fmt.Sprintf("%d", ttlSeconds))
	req.Header.Set("Urgency", "normal")
	req.Header.Set("Authorization", fmt.Sprintf("vapid t=%s, k=%s", jwt, s.publicB64))

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return &ErrSubscriptionGone{Status: resp.StatusCode}
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("push send failed: status %d", resp.StatusCode)
}

// vapidToken は endpoint のオリジンを aud として ES256 の JWT を作る。
func (s *Sender) vapidToken(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	aud := u.Scheme + "://" + u.Host
	header := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims := map[string]any{
		"aud": aud,
		"exp": time.Now().Add(12 * time.Hour).Unix(),
		"sub": s.subject,
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := b64.EncodeToString(claimsJSON)
	signingInput := header + "." + payload
	digest := sha256.Sum256([]byte(signingInput))
	r, ss, err := ecdsa.Sign(rand.Reader, s.privateKey, digest[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[0:32])
	ss.FillBytes(sig[32:64])
	return signingInput + "." + b64.EncodeToString(sig), nil
}

// encrypt は RFC 8291 (aes128gcm) に従い payload を暗号化し、送信ボディを返す。
func encrypt(sub Subscription, payload []byte) ([]byte, error) {
	uaPubBytes, err := b64.DecodeString(sub.P256dh)
	if err != nil {
		return nil, fmt.Errorf("invalid p256dh: %w", err)
	}
	authSecret, err := b64.DecodeString(sub.Auth)
	if err != nil {
		return nil, fmt.Errorf("invalid auth: %w", err)
	}

	curve := ecdh.P256()
	uaPub, err := curve.NewPublicKey(uaPubBytes)
	if err != nil {
		return nil, fmt.Errorf("invalid ua public key: %w", err)
	}
	asPriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	asPub := asPriv.PublicKey().Bytes() // 非圧縮65バイト
	shared, err := asPriv.ECDH(uaPub)
	if err != nil {
		return nil, err
	}

	// IKM = HKDF(salt=auth_secret, ikm=shared, info="WebPush: info\0"||ua_pub||as_pub, 32)
	keyInfo := append([]byte("WebPush: info\x00"), uaPubBytes...)
	keyInfo = append(keyInfo, asPub...)
	ikm := hkdf(authSecret, shared, keyInfo, 32)

	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	cek := hkdf(salt, ikm, []byte("Content-Encoding: aes128gcm\x00"), 16)
	nonce := hkdf(salt, ikm, []byte("Content-Encoding: nonce\x00"), 12)

	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// 単一レコード：末尾に区切り 0x02 を付ける。
	plaintext := append(append([]byte{}, payload...), 0x02)
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	// ヘッダ: salt(16) || rs(4) || idlen(1) || keyid(as_pub, 65)
	var header bytes.Buffer
	header.Write(salt)
	header.Write(binary.BigEndian.AppendUint32(nil, 4096))
	header.WriteByte(byte(len(asPub)))
	header.Write(asPub)
	return append(header.Bytes(), ciphertext...), nil
}

// hkdf は HKDF-SHA256（Extract + Expand）。標準ライブラリに crypto/hkdf が無い版のため自前実装。
func hkdf(salt, ikm, info []byte, length int) []byte {
	extract := hmac.New(sha256.New, salt)
	extract.Write(ikm)
	prk := extract.Sum(nil)

	var out, prev []byte
	for i := byte(1); len(out) < length; i++ {
		h := hmac.New(sha256.New, prk)
		h.Write(prev)
		h.Write(info)
		h.Write([]byte{i})
		prev = h.Sum(nil)
		out = append(out, prev...)
	}
	return out[:length]
}
