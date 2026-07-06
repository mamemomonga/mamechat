package db

import (
	"context"
)

const getMastodonAppRegistration = `-- name: GetMastodonAppRegistration :one
SELECT id, instance_url, client_id, client_secret, created_at
FROM mastodon_app_registrations
WHERE instance_url = $1
`

func (q *Queries) GetMastodonAppRegistration(ctx context.Context, instanceUrl string) (MastodonAppRegistration, error) {
	row := q.db.QueryRow(ctx, getMastodonAppRegistration, instanceUrl)
	var i MastodonAppRegistration
	err := row.Scan(&i.ID, &i.InstanceUrl, &i.ClientID, &i.ClientSecret, &i.CreatedAt)
	return i, err
}

const upsertMastodonAppRegistration = `-- name: UpsertMastodonAppRegistration :one
INSERT INTO mastodon_app_registrations (instance_url, client_id, client_secret)
VALUES ($1, $2, $3)
ON CONFLICT (instance_url) DO UPDATE
  SET client_id = EXCLUDED.client_id,
      client_secret = EXCLUDED.client_secret
RETURNING id, instance_url, client_id, client_secret, created_at
`

type UpsertMastodonAppRegistrationParams struct {
	InstanceUrl  string `json:"instance_url"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

func (q *Queries) UpsertMastodonAppRegistration(ctx context.Context, arg UpsertMastodonAppRegistrationParams) (MastodonAppRegistration, error) {
	row := q.db.QueryRow(ctx, upsertMastodonAppRegistration, arg.InstanceUrl, arg.ClientID, arg.ClientSecret)
	var i MastodonAppRegistration
	err := row.Scan(&i.ID, &i.InstanceUrl, &i.ClientID, &i.ClientSecret, &i.CreatedAt)
	return i, err
}
