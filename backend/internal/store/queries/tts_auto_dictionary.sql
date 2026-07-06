-- name: ListTTSAutoDictionaryEntries :many
SELECT term_key, term, reading, registered_by_user_id, registered_by_handle, registered_at
FROM tts_auto_dictionary_entries
ORDER BY length(term) DESC, term ASC;

-- name: UpsertTTSAutoDictionaryEntry :one
INSERT INTO tts_auto_dictionary_entries (
  term_key, term, reading, registered_by_user_id, registered_by_handle, registered_at
)
VALUES ($1, $2, $3, $4, $5, now())
ON CONFLICT (term_key)
DO UPDATE SET
  term = EXCLUDED.term,
  reading = EXCLUDED.reading,
  registered_by_user_id = EXCLUDED.registered_by_user_id,
  registered_by_handle = EXCLUDED.registered_by_handle,
  registered_at = now()
RETURNING term_key, term, reading, registered_by_user_id, registered_by_handle, registered_at;

-- name: DeleteTTSAutoDictionaryEntry :execrows
DELETE FROM tts_auto_dictionary_entries
WHERE term_key = $1;
