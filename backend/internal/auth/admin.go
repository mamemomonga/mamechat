package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/config"
	db "tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/generated/db"
	"tangled.org/mamemomonga.bsky.social/ex-wschat1/backend/internal/store"
)

const (
	OwnerRole     = "owner"
	AdminRole     = "admin"
	UserRole      = "user"
	ownerProvider = "owner"
	ownerSubject  = "owner"
)

func EnsureOwnerUser(ctx context.Context, pool *pgxpool.Pool, cfg config.Config) (UserSnapshot, error) {
	var out UserSnapshot
	err := store.WithTx(ctx, pool, func(tx pgx.Tx) error {
		q := db.New(tx)
		identity, err := q.GetAuthIdentityByProviderSubject(ctx, db.GetAuthIdentityByProviderSubjectParams{
			Provider: ownerProvider,
			Subject:  ownerSubject,
		})
		if err == nil {
			user, err := q.GetUserByID(ctx, identity.UserID)
			if err != nil {
				return err
			}
			if user.Role != OwnerRole || user.Status != "active" {
				user, err = q.UpdateUser(ctx, db.UpdateUserParams{
					ID:          user.ID,
					DisplayName: user.DisplayName,
					Handle:      user.Handle,
					AvatarUrl:   user.AvatarUrl,
					Status:      "active",
					Role:        OwnerRole,
				})
				if err != nil {
					return err
				}
			}
			out = SnapshotFromDB(user, ownerProvider, "", "", nil)
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		user, err := q.CreateUserWithRole(ctx, db.CreateUserWithRoleParams{
			DisplayName: cfg.OwnerDisplayName,
			Handle:      nullableText(cfg.OwnerHandle),
			AvatarUrl:   nullableText(cfg.OwnerAvatarURL),
			Role:        OwnerRole,
		})
		if err != nil {
			return err
		}
		if _, err := q.CreateAuthIdentity(ctx, db.CreateAuthIdentityParams{
			UserID:      user.ID,
			Provider:    ownerProvider,
			Subject:     ownerSubject,
			Handle:      nullableText(cfg.OwnerHandle),
			DisplayName: nullableText(cfg.OwnerDisplayName),
			AvatarUrl:   nullableText(cfg.OwnerAvatarURL),
		}); err != nil {
			return err
		}
		out = SnapshotFromDB(user, ownerProvider, "", "", nil)
		return nil
	})
	return out, err
}

func AuthenticateOwner(ctx context.Context, pool *pgxpool.Pool, cfg config.Config, password string) (UserSnapshot, bool, error) {
	if !passwordEqual(cfg.OwnerPassword, password) {
		return UserSnapshot{}, false, nil
	}
	user, err := EnsureOwnerUser(ctx, pool, cfg)
	if err != nil {
		return UserSnapshot{}, false, err
	}
	return user, true, nil
}

func IsPrivileged(role string) bool {
	return role == OwnerRole || role == AdminRole
}

func passwordEqual(expected, actual string) bool {
	expectedHash := sha256.Sum256([]byte(expected))
	actualHash := sha256.Sum256([]byte(actual))
	return subtle.ConstantTimeCompare(expectedHash[:], actualHash[:]) == 1
}

func nullableText(v string) pgtype.Text {
	if strings.TrimSpace(v) == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: strings.TrimSpace(v), Valid: true}
}
