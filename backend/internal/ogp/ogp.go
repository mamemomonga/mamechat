// Package ogp はチャットに貼られたURLのOGP（Open Graph Protocol）メタ情報を取得する。
// SSRF対策として、接続先IPがプライベート/ループバック等の場合は接続を拒否する。
package ogp

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Preview は1つのURLから取得したOGP要約。
type Preview struct {
	URL         string `json:"url"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Image       string `json:"image,omitempty"`
	SiteName    string `json:"siteName,omitempty"`
}

// HasContent はプレビューとして表示する価値があるか（タイトルか画像があるか）を返す。
func (p Preview) HasContent() bool {
	return strings.TrimSpace(p.Title) != "" || strings.TrimSpace(p.Image) != ""
}

const (
	maxBodyBytes   = 512 * 1024
	fetchTimeout   = 6 * time.Second
	maxRedirects   = 5
	maxTitleLen    = 300
	maxDescLen     = 500
	requestUA      = "mamechat-ogp/1.0 (+link preview bot)"
	acceptLanguage = "ja,en;q=0.8"
)

// ErrBlocked は許可されないURL（不正なスキーム・プライベートIP等）を示す。
var ErrBlocked = errors.New("url is not allowed")

type Fetcher struct {
	client *http.Client
}

func NewFetcher() *Fetcher {
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
		// Control は名前解決後・接続直前に、実際の接続先IPで呼ばれる。
		// プライベート/ループバック等への接続を拒否し、DNSリバインディングも防ぐ。
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil || !isPublicIP(ip) {
				return ErrBlocked
			}
			return nil
		},
	}
	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		MaxIdleConns:        10,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
	}
	return &Fetcher{
		client: &http.Client{
			Transport: transport,
			Timeout:   fetchTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= maxRedirects {
					return errors.New("too many redirects")
				}
				if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
					return ErrBlocked
				}
				return nil
			},
		},
	}
}

// Fetch は指定URLのOGPメタ情報を取得する。HTML以外や取得失敗時は空のPreviewを返す。
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (Preview, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Preview{}, ErrBlocked
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Preview{}, err
	}
	req.Header.Set("User-Agent", requestUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", acceptLanguage)

	resp, err := f.client.Do(req)
	if err != nil {
		return Preview{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Preview{}, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "" && !strings.Contains(ct, "text/html") && !strings.Contains(ct, "application/xhtml") {
		// HTML以外（画像・PDF等）はプレビュー対象外。
		return Preview{URL: u.String()}, nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	// リダイレクト後の最終URLを基準に相対URLを解決する。
	base := resp.Request.URL
	if base == nil {
		base = u
	}
	return parse(base, string(body)), nil
}

var (
	headRe  = regexp.MustCompile(`(?is)</head\s*>`)
	metaRe  = regexp.MustCompile(`(?is)<meta\s+[^>]*?/?>`)
	attrRe  = regexp.MustCompile(`(?is)([a-z][a-z0-9:_-]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
	titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
)

func parse(base *url.URL, body string) Preview {
	out := Preview{URL: base.String()}
	// OGPメタは<head>内にあるため、<head>までに絞って本文の誤検出を避ける。
	head := body
	if loc := headRe.FindStringIndex(body); loc != nil {
		head = body[:loc[0]]
	}

	var fallbackTitle, fallbackDesc string
	for _, tag := range metaRe.FindAllString(head, -1) {
		attrs := parseAttrs(tag)
		content := strings.TrimSpace(attrs["content"])
		if content == "" {
			continue
		}
		key := attrs["property"]
		if key == "" {
			key = attrs["name"]
		}
		switch strings.ToLower(key) {
		case "og:title":
			out.Title = content
		case "og:description":
			out.Description = content
		case "og:image", "og:image:url", "og:image:secure_url":
			if out.Image == "" {
				out.Image = content
			}
		case "og:site_name":
			out.SiteName = content
		case "twitter:title":
			if fallbackTitle == "" {
				fallbackTitle = content
			}
		case "twitter:description":
			if fallbackDesc == "" {
				fallbackDesc = content
			}
		case "twitter:image", "twitter:image:src":
			if out.Image == "" {
				out.Image = content
			}
		case "description":
			if fallbackDesc == "" {
				fallbackDesc = content
			}
		}
	}

	if out.Title == "" {
		if fallbackTitle != "" {
			out.Title = fallbackTitle
		} else if m := titleRe.FindStringSubmatch(head); m != nil {
			out.Title = strings.TrimSpace(m[1])
		}
	}
	if out.Description == "" {
		out.Description = fallbackDesc
	}

	out.Title = clean(out.Title, maxTitleLen)
	out.Description = clean(out.Description, maxDescLen)
	out.SiteName = clean(out.SiteName, maxTitleLen)
	out.Image = resolveURL(base, strings.TrimSpace(out.Image))
	return out
}

func parseAttrs(tag string) map[string]string {
	attrs := make(map[string]string)
	for _, m := range attrRe.FindAllStringSubmatch(tag, -1) {
		name := strings.ToLower(m[1])
		value := m[2]
		if value == "" {
			value = m[3]
		}
		if value == "" {
			value = m[4]
		}
		if _, ok := attrs[name]; !ok {
			attrs[name] = html.UnescapeString(value)
		}
	}
	return attrs
}

func clean(s string, max int) string {
	s = strings.TrimSpace(html.UnescapeString(s))
	// 制御文字や連続する空白を1つにまとめる。
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) > max {
		s = string([]rune(s)[:max])
	}
	return s
}

// resolveURL は相対画像URLをページURLで絶対化する。http/https以外は捨てる。
func resolveURL(base *url.URL, raw string) string {
	if raw == "" {
		return ""
	}
	ref, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	abs := base.ResolveReference(ref)
	if abs.Scheme != "http" && abs.Scheme != "https" {
		return ""
	}
	return abs.String()
}

// isPublicIP はグローバルにルーティング可能な公開IPのみ true を返す。
func isPublicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() ||
		ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	// IPv4 のキャリアグレードNAT(100.64.0.0/10)も拒否する。
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return false
		}
	}
	return true
}
