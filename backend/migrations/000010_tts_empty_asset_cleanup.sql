DELETE FROM tts_message_parts
WHERE content_hash IN (
  SELECT content_hash
  FROM tts_assets
  WHERE file_size_bytes <= 0
);

DELETE FROM tts_assets
WHERE file_size_bytes <= 0;
