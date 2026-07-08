package chat

import "testing"

func TestAttachmentComment(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		wantComment string
		wantHasURL  bool
	}{
		{"plain text", "こんにちは", "こんにちは", false},
		{"only url", "https://example.com", "", true},
		{"http url", "http://example.com", "", true},
		{"url with comment", "見て https://example.com", "見て", true},
		{"comment then url", "https://example.com みて", "みて", true},
		{"empty", "", "", false},
		{"only spaces", "   ", "", false},
		{"multiple urls no comment", "https://a.example http://b.example", "", true},
		{"inline url is not stripped", "見てhttps://example.com", "見てhttps://example.com", false},
		{"uppercase scheme", "HTTPS://EXAMPLE.COM", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			comment, hasURL := attachmentComment(tc.body)
			if comment != tc.wantComment {
				t.Errorf("comment: got %q, want %q", comment, tc.wantComment)
			}
			if hasURL != tc.wantHasURL {
				t.Errorf("hasURL: got %v, want %v", hasURL, tc.wantHasURL)
			}
		})
	}
}
