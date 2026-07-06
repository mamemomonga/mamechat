package ogp

import (
	"net"
	"net/url"
	"testing"
)

func TestParseExtractsOGP(t *testing.T) {
	base, _ := url.Parse("https://example.com/articles/1")
	body := `<!doctype html><html><head>
	<title>Fallback Title</title>
	<meta property="og:title" content="OGP &amp; Title">
	<meta property="og:description" content="A nice description.">
	<meta property="og:image" content="/img/cover.png">
	<meta property="og:site_name" content="Example">
	<meta name="description" content="ignored fallback">
	</head><body><meta property="og:title" content="should be ignored in body"></body></html>`

	p := parse(base, body)
	if p.Title != "OGP & Title" {
		t.Errorf("title = %q", p.Title)
	}
	if p.Description != "A nice description." {
		t.Errorf("description = %q", p.Description)
	}
	if p.Image != "https://example.com/img/cover.png" {
		t.Errorf("image = %q (relative should resolve to absolute)", p.Image)
	}
	if p.SiteName != "Example" {
		t.Errorf("siteName = %q", p.SiteName)
	}
	if !p.HasContent() {
		t.Error("expected HasContent true")
	}
}

func TestParseFallsBackToTitleAndTwitter(t *testing.T) {
	base, _ := url.Parse("https://example.com/")
	body := `<head><title>  Plain   Title </title>
	<meta name='twitter:description' content='tw desc'>
	<meta name="twitter:image" content="https://cdn.example.com/a.jpg"></head>`
	p := parse(base, body)
	if p.Title != "Plain Title" {
		t.Errorf("title = %q", p.Title)
	}
	if p.Description != "tw desc" {
		t.Errorf("description = %q", p.Description)
	}
	if p.Image != "https://cdn.example.com/a.jpg" {
		t.Errorf("image = %q", p.Image)
	}
}

func TestIsPublicIP(t *testing.T) {
	cases := map[string]bool{
		"8.8.8.8":         true,
		"1.1.1.1":         true,
		"127.0.0.1":       false,
		"10.0.0.5":        false,
		"192.168.1.1":     false,
		"172.16.5.4":      false,
		"169.254.169.254": false, // クラウドメタデータ
		"100.64.0.1":      false, // CGNAT
		"::1":             false,
		"2606:4700:4700::1111": true,
	}
	for s, want := range cases {
		if got := isPublicIP(net.ParseIP(s)); got != want {
			t.Errorf("isPublicIP(%s) = %v, want %v", s, got, want)
		}
	}
}
