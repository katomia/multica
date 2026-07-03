-- Room resource grants: a per-(room, resource, agent) access matrix.
-- Every agent in a project-based room gets a row per project resource;
-- access_level defaults to 'write' and can be downgraded to 'read'.
-- The unique constraint prevents duplicate grants; the auto-fill helpers
-- (AddRoomMember, CreateProjectResource, CreateRoom) upsert with DO NOTHING.
CREATE TABLE room_resource_grant (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id      UUID NOT NULL REFERENCES workspace_room(id) ON DELETE CASCADE,
    resource_id  UUID NOT NULL REFERENCES project_resource(id) ON DELETE CASCADE,
    agent_id     UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    access_level TEXT NOT NULL DEFAULT 'write' CHECK (access_level IN ('read', 'write')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (room_id, resource_id, agent_id)
);

CREATE INDEX idx_room_resource_grant_room ON room_resource_grant(room_id);
CREATE INDEX idx_room_resource_grant_agent ON room_resource_grant(agent_id);
CREATE INDEX idx_room_resource_grant_resource ON room_resource_grant(resource_id);
