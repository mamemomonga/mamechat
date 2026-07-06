-- name: GetMastodonAppRegistration :one
SELECT id, instance_url, client_id, client_secret, created_at
FROM mastodon_app_registrations
WHERE instance_url = $1;

-- name: UpsertMastodonAppRegistration :one
INSERT INTO mastodon_app_registrations (instance_url, client_id, client_secret)
VALUES ($1, $2, $3)
ON CONFLICT (instance_url) DO UPDATE
  SET client_id = EXCLUDED.client_id,
      client_secret = EXCLUDED.client_secret
RETURNING id, instance_url, client_id, client_secret, created_at;
