package tts

import (
	"context"
	"strings"
	"testing"
)

func TestNormalizeTextAppliesDictionary(t *testing.T) {
	got, ok := NormalizeText("Bluesky mastodon MISSKEY")
	if !ok {
		t.Fatal("NormalizeText returned unreadable")
	}
	want := "ブルースカイ マストドン ミスキー"
	if got != want {
		t.Fatalf("NormalizeText() = %q, want %q", got, want)
	}
}

func TestNormalizeTextWithRuntimeDictionary(t *testing.T) {
	got, ok := NormalizeTextWithDictionary("CloudFlare と cloudflare", []RuntimeDictionaryEntry{
		{Term: "CloudFlare", Reading: "クラウドフレア"},
	})
	if !ok {
		t.Fatal("NormalizeTextWithDictionary returned unreadable")
	}
	want := "クラウドフレア と クラウドフレア"
	if got != want {
		t.Fatalf("NormalizeTextWithDictionary() = %q, want %q", got, want)
	}
}

func TestNormalizeTextReplacesURL(t *testing.T) {
	got, ok := NormalizeText("見て https://example.com/path?x=1")
	if !ok {
		t.Fatal("NormalizeText returned unreadable")
	}
	if got != "見て ゆーあーるえる" {
		t.Fatalf("NormalizeText() = %q", got)
	}
}

func TestNormalizeTextKeepsFullPost(t *testing.T) {
	input := strings.Repeat("あ", 140)
	got, ok := NormalizeText(input)
	if !ok {
		t.Fatal("NormalizeText returned unreadable")
	}
	if got != input {
		t.Fatalf("NormalizeText() length = %d, want %d", len([]rune(got)), len([]rune(input)))
	}
}

func TestSpeedScaleForMessageRampsToMaxAtMessageLimit(t *testing.T) {
	settings := Settings{SpeedScale: 1.0, MessageMaxRunes: 400}
	if got := SpeedScaleForMessage(settings, 50); got != 1.0 {
		t.Fatalf("speed before ramp = %f, want 1.0", got)
	}
	if got := SpeedScaleForMessage(settings, 400); got != maxSpeedScale {
		t.Fatalf("speed at max length = %f, want %f", got, maxSpeedScale)
	}
	if got := SpeedScaleForMessage(settings, 225); got <= 1.0 || got >= maxSpeedScale {
		t.Fatalf("speed in ramp = %f, want between 1.0 and %f", got, maxSpeedScale)
	}
}

func TestEnqueueMessageSkipsEmptySpeaker(t *testing.T) {
	enqueuer := NewEnqueuer(Settings{Enabled: true}, nil, nil, nil)
	if err := enqueuer.EnqueueMessage(context.Background(), 1, "general", 1, "", "読み上げない"); err != nil {
		t.Fatalf("EnqueueMessage() error = %v", err)
	}
}
