package httpserver

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/mamemomonga/mamechat/backend/internal/generated/db"
)

func TestBuildChannelOGPDataIncludesChannelAndOwner(t *testing.T) {
	data := buildChannelOGPData("mamechat", "https://example.com/channels/general-room", db.GetChannelOGPBySlugRow{
		Slug:             "general-room",
		Title:            "雑談",
		Description:      pgtype.Text{String: "ゆるく話す場所", Valid: true},
		OwnerDisplayName: "Owner",
		OwnerAvatarUrl:   pgtype.Text{String: "https://example.com/avatar.png", Valid: true},
	})

	if data.OGTitle != "雑談 - Owner" {
		t.Fatalf("OGTitle = %q", data.OGTitle)
	}
	if data.Description != "チャンネルオーナー: Owner / ゆるく話す場所" {
		t.Fatalf("Description = %q", data.Description)
	}
	if data.PageTitle != "雑談 - Owner | mamechat" {
		t.Fatalf("PageTitle = %q", data.PageTitle)
	}
	if data.URL != "https://example.com/channels/general-room" {
		t.Fatalf("URL = %q", data.URL)
	}
	if data.ImageURL != "https://example.com/avatar.png" {
		t.Fatalf("ImageURL = %q", data.ImageURL)
	}
	if data.TwitterCard != "summary_large_image" {
		t.Fatalf("TwitterCard = %q", data.TwitterCard)
	}
}

func TestBuildChannelOGPDataFallsBackWhenOwnerMissing(t *testing.T) {
	data := buildChannelOGPData("", "http://localhost/channels/general-room", db.GetChannelOGPBySlugRow{
		Slug:  "general-room",
		Title: "",
	})

	if data.OGTitle != "general-room - Unknown" {
		t.Fatalf("OGTitle = %q", data.OGTitle)
	}
	if data.Description != "チャンネルオーナー: Unknown / mamechat のチャンネル" {
		t.Fatalf("Description = %q", data.Description)
	}
	if data.TwitterCard != "summary" {
		t.Fatalf("TwitterCard = %q", data.TwitterCard)
	}
}
