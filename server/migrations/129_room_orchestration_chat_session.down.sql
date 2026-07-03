DROP INDEX IF EXISTS idx_room_orchestration_chat_session;
ALTER TABLE room_orchestration DROP COLUMN IF EXISTS chat_session_id;
