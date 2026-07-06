DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.tables
    WHERE table_schema = 'public' AND table_name = 'rooms'
  ) THEN
    ALTER TABLE rooms RENAME TO channels;
  END IF;

  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = 'public' AND table_name = 'chat_messages' AND column_name = 'room_id'
  ) THEN
    ALTER TABLE chat_messages RENAME COLUMN room_id TO channel_id;
  END IF;

  IF EXISTS (
    SELECT 1 FROM pg_indexes
    WHERE schemaname = 'public' AND indexname = 'chat_messages_room_created_idx'
  ) THEN
    ALTER INDEX chat_messages_room_created_idx RENAME TO chat_messages_channel_created_idx;
  END IF;
END $$;
