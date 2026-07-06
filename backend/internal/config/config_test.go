package config

import (
	"testing"
	"time"
)

func TestLoadNormalizesRealtimeIntervals(t *testing.T) {
	t.Setenv("ACTIVE_POLL_SECONDS", "1")
	t.Setenv("BEACON_SECONDS", "2")
	t.Setenv("ACTIVE_WINDOW_SECONDS", "3")

	cfg := Load()
	if cfg.ActivePollSeconds != minActivePollSeconds {
		t.Fatalf("ActivePollSeconds = %d, want %d", cfg.ActivePollSeconds, minActivePollSeconds)
	}
	if cfg.BeaconSeconds != minBeaconSeconds {
		t.Fatalf("BeaconSeconds = %d, want %d", cfg.BeaconSeconds, minBeaconSeconds)
	}
	wantWindow := time.Duration(minBeaconSeconds*activeWindowBeaconFactor) * time.Second
	if cfg.ActiveWindow != wantWindow {
		t.Fatalf("ActiveWindow = %s, want %s", cfg.ActiveWindow, wantWindow)
	}
}

func TestLoadCapsRealtimeIntervals(t *testing.T) {
	t.Setenv("ACTIVE_POLL_SECONDS", "999")
	t.Setenv("BEACON_SECONDS", "999")
	t.Setenv("ACTIVE_WINDOW_SECONDS", "999")

	cfg := Load()
	if cfg.ActivePollSeconds != maxActivePollSeconds {
		t.Fatalf("ActivePollSeconds = %d, want %d", cfg.ActivePollSeconds, maxActivePollSeconds)
	}
	if cfg.BeaconSeconds != maxBeaconSeconds {
		t.Fatalf("BeaconSeconds = %d, want %d", cfg.BeaconSeconds, maxBeaconSeconds)
	}
	wantWindow := time.Duration(maxActiveWindowSeconds) * time.Second
	if cfg.ActiveWindow != wantWindow {
		t.Fatalf("ActiveWindow = %s, want %s", cfg.ActiveWindow, wantWindow)
	}
}
