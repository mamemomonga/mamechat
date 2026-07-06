package access

import "testing"

func TestAllowedBySubject(t *testing.T) {
	// Bluesky: 安定IDは DID。ハンドルが変わっても DID で一致する。
	blueskyUser := UserKeys("atproto", "did:plc:alice", "alice-new.bsky.social", "https://bsky.app/profile/did:plc:alice")
	// Fediverse: 安定IDは instance:accountID。
	fediUser := UserKeys("mastodon", "https://example.social:4242", "bob@example.social", "https://example.social/@bob")

	blueskyEntry := Entry{
		Provider:   "atproto",
		Subject:    "did:plc:alice",
		Handle:     "alice.bsky.social",
		ProfileURL: "https://bsky.app/profile/did:plc:alice",
	}
	fediEntry := Entry{
		Provider:   "mastodon",
		Subject:    "https://example.social:4242",
		Handle:     "bob@example.social",
		ProfileURL: "https://example.social/@bob",
	}

	cases := []struct {
		name  string
		mode  string
		list  []Entry
		keys  map[string]struct{}
		allow bool
	}{
		{"none always allows", ModeNone, nil, blueskyUser, true},
		{"whitelist matches by DID despite handle change", ModeWhitelist, []Entry{blueskyEntry}, blueskyUser, true},
		{"whitelist matches fedi by subject", ModeWhitelist, []Entry{fediEntry}, fediUser, true},
		{"whitelist no match", ModeWhitelist, []Entry{fediEntry}, blueskyUser, false},
		{"blacklist blocks listed subject", ModeBlacklist, []Entry{blueskyEntry}, blueskyUser, false},
		{"blacklist allows unlisted", ModeBlacklist, []Entry{fediEntry}, blueskyUser, true},
		{"empty whitelist blocks everyone", ModeWhitelist, nil, blueskyUser, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Allowed(tc.mode, tc.list, tc.keys); got != tc.allow {
				t.Errorf("Allowed(%q) = %v, want %v", tc.mode, got, tc.allow)
			}
		})
	}
}

func TestMatchFallbackByHandleAndRaw(t *testing.T) {
	user := UserKeys("atproto", "did:plc:alice", "alice.bsky.social", "https://bsky.app/profile/alice.bsky.social")

	// 解決されていない（ハンドルのみ／生入力のみ）エントリでも照合できる。
	handleOnly := Entry{Handle: "alice.bsky.social"}
	rawHandle := Entry{Raw: "@alice.bsky.social"}
	rawURL := Entry{Raw: "https://bsky.app/profile/alice.bsky.social/"}
	rawMiss := Entry{Raw: "@someone.else"}

	for _, tc := range []struct {
		name  string
		entry Entry
		want  bool
	}{
		{"handle-only match", handleOnly, true},
		{"raw handle match", rawHandle, true},
		{"raw url match trailing slash", rawURL, true},
		{"raw no match", rawMiss, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Listed([]Entry{tc.entry}, user); got != tc.want {
				t.Errorf("Listed(%+v) = %v, want %v", tc.entry, got, tc.want)
			}
		})
	}
}

func TestParseListLegacyAndStructured(t *testing.T) {
	raw := []byte(`["@legacy.bsky.social", {"provider":"atproto","subject":"did:plc:x","handle":"x.bsky.social"}]`)
	list := ParseList(raw)
	if len(list) != 2 {
		t.Fatalf("ParseList len = %d, want 2", len(list))
	}
	if list[0].Raw != "@legacy.bsky.social" {
		t.Errorf("legacy entry Raw = %q", list[0].Raw)
	}
	if list[1].Subject != "did:plc:x" || list[1].Provider != "atproto" {
		t.Errorf("structured entry = %+v", list[1])
	}
}

func TestCleanEntriesDedupe(t *testing.T) {
	in := []Entry{
		{Provider: "atproto", Subject: "did:plc:a", Handle: "a.bsky.social"},
		{Provider: "atproto", Subject: "did:plc:a", Handle: "a-renamed.bsky.social"}, // same subject → dup
		{Handle: "b@example.social"},
		{},          // no keys → dropped
		{Raw: "   "}, // empty raw → dropped
	}
	got := CleanEntries(in)
	if len(got) != 2 {
		t.Fatalf("CleanEntries len = %d (%+v), want 2", len(got), got)
	}
}

func TestNormalizeMode(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"whitelist", ModeWhitelist},
		{"blacklist", ModeBlacklist},
		{"none", ModeNone},
		{"", ModeNone},
		{"bogus", ModeNone},
	} {
		if got := NormalizeMode(tc.in); got != tc.want {
			t.Errorf("NormalizeMode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEffectiveMode(t *testing.T) {
	cases := []struct {
		name             string
		mode             string
		whitelistEnabled bool
		want             string
	}{
		{"whitelist disabled becomes none", ModeWhitelist, false, ModeNone},
		{"whitelist enabled stays whitelist", ModeWhitelist, true, ModeWhitelist},
		{"blacklist unaffected when disabled", ModeBlacklist, false, ModeBlacklist},
		{"blacklist unaffected when enabled", ModeBlacklist, true, ModeBlacklist},
		{"none stays none", ModeNone, false, ModeNone},
		{"unknown mode normalized to none", "bogus", true, ModeNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EffectiveMode(tc.mode, tc.whitelistEnabled); got != tc.want {
				t.Errorf("EffectiveMode(%q, %v) = %q, want %q", tc.mode, tc.whitelistEnabled, got, tc.want)
			}
		})
	}
}
