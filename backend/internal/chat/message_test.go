package chat

import (
	"encoding/json"
	"testing"
)

func TestTTSPartReadyIncludesZeroPartIndex(t *testing.T) {
	msg := TTSPartReady("123", 0, "abc", 0)
	body, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal TTSPartReady: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal TTSPartReady: %v", err)
	}
	if got["partIndex"] != float64(0) {
		t.Fatalf("partIndex = %#v, want 0; json=%s", got["partIndex"], body)
	}
}
