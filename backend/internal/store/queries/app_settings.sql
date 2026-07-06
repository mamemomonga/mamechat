-- name: GetAppSetting :one
SELECT value FROM app_settings WHERE key = $1;

-- name: SetAppSetting :exec
INSERT INTO app_settings (key, value)
VALUES ($1, $2)
ON CONFLICT (key)
DO UPDATE SET value = EXCLUDED.value, updated_at = now();

-- name: DeleteAppSetting :exec
DELETE FROM app_settings WHERE key = $1;
