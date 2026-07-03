-- Allow multiple chat_only orchestrations per source_message (one per agent),
-- while keeping uniqueness for non-chat orchestrations (chat_session_id IS NULL).
ALTER TABLE room_orchestration DROP CONSTRAINT IF EXISTS room_orchestration_source_message_id_key;

CREATE UNIQUE INDEX room_orchestration_unique_source_no_chat
    ON room_orchestration(source_message_id)
    WHERE chat_session_id IS NULL;
