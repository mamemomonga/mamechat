package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	db "github.com/mamemomonga/mamechat/backend/internal/generated/db"
	"github.com/mamemomonga/mamechat/backend/internal/webpush"
)

// pushSubscriptionBody はブラウザの PushSubscription.toJSON() をそのまま受け取る形。
type pushSubscriptionBody struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// subscribePush はブラウザのPush購読を保存する。
func (s *Server) subscribePush(w http.ResponseWriter, r *http.Request) {
	if s.push == nil {
		writeError(w, http.StatusNotFound, "push notifications are disabled")
		return
	}
	session, err := s.auth.CurrentSession(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req pushSubscriptionBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Endpoint == "" || req.Keys.P256dh == "" || req.Keys.Auth == "" {
		writeError(w, http.StatusBadRequest, "endpoint and keys are required")
		return
	}
	if err := s.q.UpsertPushSubscription(r.Context(), db.UpsertPushSubscriptionParams{
		UserID:   session.User.ID,
		Endpoint: req.Endpoint,
		P256dh:   req.Keys.P256dh,
		Auth:     req.Keys.Auth,
	}); err != nil {
		slog.Error("upsert push subscription failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save subscription")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// unsubscribePush は指定endpointのPush購読を削除する。
func (s *Server) unsubscribePush(w http.ResponseWriter, r *http.Request) {
	if _, err := s.auth.CurrentSession(r.Context(), r); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Endpoint != "" {
		if err := s.q.DeletePushSubscriptionByEndpoint(r.Context(), req.Endpoint); err != nil {
			slog.Error("delete push subscription failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to delete subscription")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// setChannelNotify はログインユーザーのチャンネル営業開始通知のオン/オフを切り替える。
func (s *Server) setChannelNotify(w http.ResponseWriter, r *http.Request) {
	session, err := s.auth.CurrentSession(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	channel, err := s.q.GetChannelBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "channel not found")
			return
		}
		slog.Error("get channel for notify failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load channel")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Enabled {
		err = s.q.SetChannelNotificationOptin(r.Context(), db.SetChannelNotificationOptinParams{
			ChannelID: channel.ID,
			UserID:    session.User.ID,
		})
	} else {
		err = s.q.DeleteChannelNotificationOptin(r.Context(), db.DeleteChannelNotificationOptinParams{
			ChannelID: channel.ID,
			UserID:    session.User.ID,
		})
	}
	if err != nil {
		slog.Error("update channel notify optin failed", "channel", channel.Slug, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update notification setting")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": req.Enabled})
}

// notifyChannelOperating はチャンネルが営業中になったとき、通知をオンにしている
// ユーザー全員へ「営業中になりました」のPushを送る。バックグラウンドで実行する。
func (s *Server) notifyChannelOperating(channel db.Channel) {
	if s.push == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		subs, err := s.q.ListPushSubscriptionsForChannelOptins(ctx, channel.ID)
		if err != nil {
			slog.Error("list push subscriptions for channel failed", "channel", channel.Slug, "error", err)
			return
		}
		slog.Info("channel operating push", "channel", channel.Slug, "subscriptions", len(subs))
		if len(subs) == 0 {
			return
		}
		payload, err := json.Marshal(map[string]string{
			"title": fmt.Sprintf("%s が営業中になりました", channel.Title),
			"body":  s.cfg.ServiceName,
			"url":   "/channels/" + channel.Slug,
		})
		if err != nil {
			slog.Error("marshal push payload failed", "error", err)
			return
		}
		targets := make([]webpush.Subscription, 0, len(subs))
		for _, sub := range subs {
			targets = append(targets, webpush.Subscription{Endpoint: sub.Endpoint, P256dh: sub.P256dh, Auth: sub.Auth})
		}
		sent, gone, failed := s.sendPushBatch(ctx, targets, payload)
		slog.Info("channel operating push done", "channel", channel.Slug, "sent", sent, "gone", gone, "failed", failed)
	}()
}

// sendPushBatch は複数の購読へ payload を送り、成功/失効/失敗の件数を返す。失効はDB削除する。
func (s *Server) sendPushBatch(ctx context.Context, targets []webpush.Subscription, payload []byte) (sent, gone, failed int) {
	for _, sub := range targets {
		err := s.push.Send(ctx, sub, payload, 24*60*60)
		if err == nil {
			sent++
			continue
		}
		var goneErr *webpush.ErrSubscriptionGone
		if errors.As(err, &goneErr) {
			gone++
			if delErr := s.q.DeletePushSubscriptionByEndpoint(ctx, sub.Endpoint); delErr != nil {
				slog.Warn("delete gone push subscription failed", "error", delErr)
			}
			continue
		}
		failed++
		slog.Warn("send push failed", "error", err)
	}
	return sent, gone, failed
}

// testPush は現在のユーザー自身の全購読へテスト通知を送る（動作確認用）。
func (s *Server) testPush(w http.ResponseWriter, r *http.Request) {
	if s.push == nil {
		writeError(w, http.StatusNotFound, "push notifications are disabled")
		return
	}
	session, err := s.auth.CurrentSession(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	subs, err := s.q.ListPushSubscriptionsByUser(r.Context(), session.User.ID)
	if err != nil {
		slog.Error("list push subscriptions by user failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load subscriptions")
		return
	}
	payload, err := json.Marshal(map[string]string{
		"title": "テスト通知",
		"body":  s.cfg.ServiceName + " からのテスト通知です",
		"url":   "/",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build payload")
		return
	}
	targets := make([]webpush.Subscription, 0, len(subs))
	for _, sub := range subs {
		targets = append(targets, webpush.Subscription{Endpoint: sub.Endpoint, P256dh: sub.P256dh, Auth: sub.Auth})
	}
	sent, gone, failed := s.sendPushBatch(r.Context(), targets, payload)
	slog.Info("test push done", "user", session.User.ID, "sent", sent, "gone", gone, "failed", failed)
	writeJSON(w, http.StatusOK, map[string]int{"sent": sent, "gone": gone, "failed": failed})
}
