package webpush

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHKDFRFC5869 は RFC 5869 のテストベクタ（Test Case 1, SHA-256）で HKDF を検証する。
// 自作HKDFがRFC準拠であることを、往復テストとは独立に確認する。
func TestHKDFRFC5869(t *testing.T) {
	ikm := bytesRepeat(0x0b, 22)
	salt, _ := hex.DecodeString("000102030405060708090a0b0c")
	info, _ := hex.DecodeString("f0f1f2f3f4f5f6f7f8f9")
	want := "3cb25f25faacd57a90434f64d0362f2a2d2d0a90cf1a5a4c5db02d56ecc4c5bf34007208d5b887185865"
	got := hex.EncodeToString(hkdf(salt, ikm, info, 42))
	if got != want {
		t.Fatalf("hkdf mismatch:\n got %s\nwant %s", got, want)
	}
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

// TestSendRoundTrip は Sender が作った暗号化ボディを UA 側の手順で復号し、
// 平文が一致することを確認する（RFC 8291 の実装が正しいことの検証）。
func TestSendRoundTrip(t *testing.T) {
	pub, priv, err := GenerateVAPIDKeys()
	if err != nil {
		t.Fatalf("GenerateVAPIDKeys: %v", err)
	}
	sender, err := NewSender(pub, priv, "mailto:test@example.com")
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}

	// UA（ブラウザ）側の鍵ペアと認証シークレット。
	uaPriv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}

	var gotBody []byte
	var gotAuthHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthHeader = r.Header.Get("Authorization")
		if enc := r.Header.Get("Content-Encoding"); enc != "aes128gcm" {
			t.Errorf("Content-Encoding = %q", enc)
		}
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	sub := Subscription{
		Endpoint: srv.URL,
		P256dh:   b64.EncodeToString(uaPriv.PublicKey().Bytes()),
		Auth:     b64.EncodeToString(auth),
	}
	payload := []byte(`{"title":"hello","body":"世界"}`)
	if err := sender.Send(context.Background(), sub, payload, 3600); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if !strings.HasPrefix(gotAuthHeader, "vapid t=") || !strings.Contains(gotAuthHeader, "k="+pub) {
		t.Fatalf("bad Authorization header: %q", gotAuthHeader)
	}

	got, err := decryptForTest(gotBody, uaPriv, auth)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("payload mismatch: got %q want %q", got, payload)
	}
}

// decryptForTest は UA 側の復号を再現する（テスト専用）。
func decryptForTest(body []byte, uaPriv *ecdh.PrivateKey, auth []byte) ([]byte, error) {
	salt := body[0:16]
	idlen := int(body[20])
	keyid := body[21 : 21+idlen] // as_pub
	ciphertext := body[21+idlen:]
	_ = binary.BigEndian.Uint32(body[16:20])

	asPub, err := ecdh.P256().NewPublicKey(keyid)
	if err != nil {
		return nil, err
	}
	shared, err := uaPriv.ECDH(asPub)
	if err != nil {
		return nil, err
	}
	keyInfo := append([]byte("WebPush: info\x00"), uaPriv.PublicKey().Bytes()...)
	keyInfo = append(keyInfo, keyid...)
	ikm := hkdf(auth, shared, keyInfo, 32)
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
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	// 末尾の区切り 0x02 を除く。
	for len(plaintext) > 0 && plaintext[len(plaintext)-1] == 0x00 {
		plaintext = plaintext[:len(plaintext)-1]
	}
	if len(plaintext) > 0 && plaintext[len(plaintext)-1] == 0x02 {
		plaintext = plaintext[:len(plaintext)-1]
	}
	return plaintext, nil
}
