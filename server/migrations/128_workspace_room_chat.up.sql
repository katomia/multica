CREATE TABLE workspace_room (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    display_name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    visibility TEXT NOT NULL DEFAULT 'workspace' CHECK (visibility IN ('workspace', 'private')),
    created_by_id UUID NOT NULL REFERENCES member(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name)
);

CREATE TABLE room_member (
    room_id UUID NOT NULL REFERENCES workspace_room(id) ON DELETE CASCADE,
    member_type TEXT NOT NULL CHECK (member_type IN ('member', 'agent')),
    member_id UUID NOT NULL,
    joined_by_id UUID REFERENCES member(id) ON DELETE SET NULL,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (room_id, member_type, member_id)
);

CREATE TABLE room_message (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id UUID NOT NULL REFERENCES workspace_room(id) ON DELETE CASCADE,
    sender_type TEXT NOT NULL CHECK (sender_type IN ('member', 'agent', 'system')),
    sender_id UUID,
    message_type TEXT NOT NULL DEFAULT 'human'
        CHECK (message_type IN ('human', 'agent', 'system', 'orchestration', 'issue_created', 'issue_status')),
    content TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE room_orchestration (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id UUID NOT NULL REFERENCES workspace_room(id) ON DELETE CASCADE,
    source_message_id UUID NOT NULL REFERENCES room_message(id) ON DELETE CASCADE,
    decision_source TEXT NOT NULL CHECK (decision_source IN ('direct_command', 'mention_orchestrator')),
    decision_type TEXT NOT NULL CHECK (decision_type IN ('chat_only', 'ask_clarification', 'single_issue', 'issue_dag')),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'applied', 'failed')),
    input_snapshot JSONB NOT NULL DEFAULT '{}',
    decision_json JSONB NOT NULL DEFAULT '{}',
    model_provider TEXT,
    model_name TEXT,
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    applied_at TIMESTAMPTZ,
    UNIQUE (source_message_id)
);

CREATE TABLE room_issue_link (
    room_id UUID NOT NULL REFERENCES workspace_room(id) ON DELETE CASCADE,
    room_message_id UUID NOT NULL REFERENCES room_message(id) ON DELETE CASCADE,
    orchestration_id UUID REFERENCES room_orchestration(id) ON DELETE SET NULL,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    link_role TEXT NOT NULL CHECK (link_role IN ('root', 'child', 'related')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (room_id, issue_id)
);

CREATE INDEX idx_workspace_room_workspace ON workspace_room(workspace_id, created_at);
CREATE INDEX idx_room_member_member ON room_member(member_type, member_id);
CREATE INDEX idx_room_message_room_created ON room_message(room_id, created_at);
CREATE INDEX idx_room_issue_link_message ON room_issue_link(room_message_id);
CREATE INDEX idx_room_issue_link_issue ON room_issue_link(issue_id);
