ALTER TABLE room_orchestration
    ADD COLUMN chat_session_id UUID REFERENCES chat_session(id) ON DELETE SET NULL;

CREATE INDEX idx_room_orchestration_chat_session ON room_orchestration(chat_session_id)
    WHERE chat_session_id IS NOT NULL;
