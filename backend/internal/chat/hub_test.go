package chat

import (
	"context"
	"sync"
	"testing"
)

// fakePresence は PresenceStore のテスト用スタブ。ClearActive の呼び出しを記録する。
type fakePresence struct {
	mu      sync.Mutex
	cleared []string
}

func (f *fakePresence) MarkActive(context.Context, string, string) error   { return nil }
func (f *fakePresence) RemoveActive(context.Context, string, string) error { return nil }
func (f *fakePresence) ActiveUserIDs(context.Context, string) (map[string]struct{}, error) {
	return map[string]struct{}{}, nil
}
func (f *fakePresence) ClearActive(_ context.Context, channelSlug string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleared = append(f.cleared, channelSlug)
	return nil
}

// fakeBus は RealtimeBus のテスト用スタブ。
type fakeBus struct{}

func (fakeBus) Publish(context.Context, string, ServerMessage) error { return nil }
func (fakeBus) Subscribe(context.Context, string) (<-chan ServerMessage, func() error, error) {
	return make(chan ServerMessage), func() error { return nil }, nil
}

// DropChannel はチャンネル削除時に、このノードのメモリ状態を破棄しアクティブ集合を消す。
// 同じ slug で作り直した際に旧 channel_id が残らないようにする土台の動作を検証する。
func TestDropChannelRemovesStateAndClearsActive(t *testing.T) {
	presence := &fakePresence{}
	h := NewHub(fakeBus{}, presence, nil, 0)

	// runChannel を起動せずに、削除対象のローカルチャンネル状態だけを差し込む。
	_, cancel := context.WithCancel(context.Background())
	h.channels["the-lounge"] = &localChannel{
		slug:        "the-lounge",
		channelID:   10, // 旧 channel_id
		clients:     map[*Client]struct{}{},
		cancel:      cancel,
		unsubscribe: func() error { return nil },
	}

	h.DropChannel("the-lounge")

	if _, ok := h.channels["the-lounge"]; ok {
		t.Fatalf("DropChannel はメモリ上のチャンネル状態を削除するべき")
	}
	presence.mu.Lock()
	defer presence.mu.Unlock()
	if len(presence.cleared) != 1 || presence.cleared[0] != "the-lounge" {
		t.Fatalf("ClearActive(the-lounge) が呼ばれるべき: got %v", presence.cleared)
	}
}

// kickAll は在室者ゼロでも安全に動く（削除通知経路が空チャンネルで panic しないこと）。
func TestKickAllNoClientsIsSafe(t *testing.T) {
	h := NewHub(fakeBus{}, &fakePresence{}, nil, 0)
	h.kickAll("missing-channel") // エントリ無し
	_, cancel := context.WithCancel(context.Background())
	h.channels["empty"] = &localChannel{slug: "empty", clients: map[*Client]struct{}{}, cancel: cancel}
	h.kickAll("empty") // クライアント無し
}
