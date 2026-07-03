ALTER TABLE room_orchestration DROP CONSTRAINT IF EXISTS room_orchestration_decision_type_check;

ALTER TABLE room_orchestration
    ADD CONSTRAINT room_orchestration_decision_type_check
    CHECK (decision_type IN ('chat_only', 'ask_clarification', 'single_issue', 'issue_dag', 'orchestrator_routing', 'plan'));

CREATE TABLE room_orchestration_action (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    orchestration_id UUID NOT NULL REFERENCES room_orchestration(id) ON DELETE CASCADE,
    action_key TEXT NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('chat', 'issue')),
    agent_id UUID REFERENCES agent(id) ON DELETE SET NULL,
    title TEXT NOT NULL DEFAULT '',
    stage INTEGER NOT NULL DEFAULT 1 CHECK (stage >= 1),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'blocked', 'running', 'done', 'failed')),
    deliverable TEXT NOT NULL DEFAULT '',
    depends_on JSONB NOT NULL DEFAULT '[]',
    chat_session_id UUID REFERENCES chat_session(id) ON DELETE SET NULL,
    issue_id UUID REFERENCES issue(id) ON DELETE SET NULL,
    output TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (orchestration_id, action_key)
);

CREATE UNIQUE INDEX room_orchestration_action_chat_session_unique
    ON room_orchestration_action(chat_session_id)
    WHERE chat_session_id IS NOT NULL;

CREATE UNIQUE INDEX room_orchestration_action_issue_unique
    ON room_orchestration_action(issue_id)
    WHERE issue_id IS NOT NULL;

CREATE INDEX room_orchestration_action_orchestration_status
    ON room_orchestration_action(orchestration_id, status);
