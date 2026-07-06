package httpserver

import (
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"

	db "tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/generated/db"
)

var channelOGPTemplate = template.Must(template.New("channel-ogp").Parse(`<!doctype html>
<html lang="ja">
  <head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.PageTitle}}</title>
    <meta name="description" content="{{.Description}}">
    <meta property="og:type" content="website">
    <meta property="og:title" content="{{.OGTitle}}">
    <meta property="og:description" content="{{.Description}}">
    <meta property="og:url" content="{{.URL}}">
    <meta property="og:site_name" content="{{.ServiceName}}">
    {{if .ImageURL}}<meta property="og:image" content="{{.ImageURL}}">{{end}}
    <meta name="twitter:card" content="{{.TwitterCard}}">
    <meta name="twitter:title" content="{{.OGTitle}}">
    <meta name="twitter:description" content="{{.Description}}">
    {{if .ImageURL}}<meta name="twitter:image" content="{{.ImageURL}}">{{end}}
  </head>
  <body>
    <main>
      <h1>{{.ChannelTitle}}</h1>
      <p>{{.Description}}</p>
      <p><a href="{{.URL}}">チャンネルを開く</a></p>
    </main>
  </body>
</html>
`))

type channelOGPData struct {
	PageTitle    string
	OGTitle      string
	Description  string
	URL          string
	ServiceName  string
	ChannelTitle string
	ImageURL     string
	TwitterCard  string
}

// channelOGP はSNS/チャットアプリのプレビューbot向けに、チャンネル固有のOGP HTMLを返す。
// 通常ブラウザには nginx が静的なReactアプリを返し、プレビューbotだけがここへproxyされる。
func (s *Server) channelOGP(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !slugPattern.MatchString(slug) {
		http.NotFound(w, r)
		return
	}
	ch, err := s.q.GetChannelOGPBySlug(r.Context(), slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		slog.Error("get channel ogp failed", "channel", slug, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	data := buildChannelOGPData(s.cfg.ServiceName, absoluteRequestURL(r), ch)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	if err := channelOGPTemplate.Execute(w, data); err != nil {
		slog.Warn("render channel ogp failed", "channel", slug, "error", err)
	}
}

func buildChannelOGPData(serviceName, pageURL string, ch db.GetChannelOGPBySlugRow) channelOGPData {
	if serviceName == "" {
		serviceName = "mamechat"
	}
	owner := strings.TrimSpace(ch.OwnerDisplayName)
	if owner == "" {
		owner = "Unknown"
	}
	title := strings.TrimSpace(ch.Title)
	if title == "" {
		title = ch.Slug
	}
	ogTitle := title + " - " + owner
	desc := strings.TrimSpace(textValue(ch.Description))
	if desc != "" {
		desc = "チャンネルオーナー: " + owner + " / " + desc
	} else {
		desc = "チャンネルオーナー: " + owner + " / " + serviceName + " のチャンネル"
	}
	imageURL := strings.TrimSpace(textValue(ch.OwnerAvatarUrl))
	card := "summary"
	if imageURL != "" {
		card = "summary_large_image"
	}
	return channelOGPData{
		PageTitle:    ogTitle + " | " + serviceName,
		OGTitle:      ogTitle,
		Description:  desc,
		URL:          pageURL,
		ServiceName:  serviceName,
		ChannelTitle: title,
		ImageURL:     imageURL,
		TwitterCard:  card,
	}
}

func absoluteRequestURL(r *http.Request) string {
	scheme := firstForwardedValue(r.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	host := firstForwardedValue(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}
	u := url.URL{
		Scheme: scheme,
		Host:   host,
		Path:   r.URL.Path,
	}
	return u.String()
}

func firstForwardedValue(v string) string {
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}
