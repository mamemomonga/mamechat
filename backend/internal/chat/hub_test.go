package chat

import (
	"context"
	"sync"
	"testing"
	"time"
)

// 離席タイマー（猶予）が残り営業時間より大きいとき、オーナー不在でも自動閉店で
// 残時間を延ばさず、本来の営業終了予定時刻で準備中へ移行することを検証する。
func TestComputeSuspendDeadlineAwayTimerDoesNotExtend(t *testing.T) {
	h := &Hub{}
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	opDeadline := now.Add(5 * time.Minute) // 残り営業5分
	ch := &localChannel{
		operatingDeadline: opDeadline,
		suspendEnabled:    true,
		grace:             30 * time.Minute, // 離席猶予30分（残りより大きい）
		ownerAbsentSince:  now,
	}
	got := h.computeSuspendDeadlineLocked(ch, false, now)
	if !got.Equal(opDeadline) {
		t.Fatalf("deadline = %v, want operating deadline %v（離席タイマーで延長してはならない）", got, opDeadline)
	}
}

// 離席タイマー（猶予）が残り営業時間より小さいときは、猶予締切（早い方）で準備中へ移行する。
func TestComputeSuspendDeadlineAwayTimerCloserWins(t *testing.T) {
	h := &Hub{}
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	ch := &localChannel{
		operatingDeadline: now.Add(30 * time.Minute), // 残り営業30分
		suspendEnabled:    true,
		grace:             5 * time.Minute, // 離席猶予5分（残りより小さい）
		ownerAbsentSince:  now,
	}
	got := h.computeSuspendDeadlineLocked(ch, false, now)
	want := now.Add(5 * time.Minute)
	if !got.Equal(want) {
		t.Fatalf("deadline = %v, want grace deadline %v", got, want)
	}
}

// 参考: いったん営業を凍結（operatingDeadlineをゼロ化）して猶予に切り替えると、
// 猶予締切まで延びてしまう。だからこそ猶予>残時間のときは凍結しない、という前提を固定する。
func TestComputeSuspendDeadlinePausedUsesGrace(t *testing.T) {
	h := &Hub{}
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	ch := &localChannel{
		operatingDeadline:        time.Time{}, // 凍結中（一時停止）
		operatingPausedRemaining: 5 * time.Minute,
		suspendEnabled:           true,
		grace:                    30 * time.Minute,
		ownerAbsentSince:         now,
	}
	got := h.computeSuspendDeadlineLocked(ch, false, now)
	want := now.Add(30 * time.Minute)
	if !got.Equal(want) {
		t.Fatalf("deadline = %v, want %v（凍結時は猶予締切）", got, want)
	}
}

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
