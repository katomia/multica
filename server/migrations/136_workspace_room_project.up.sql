-- Project-based rooms: bind a room to a project so it inherits the project's
-- resources as shared execution context. project_id is nullable — plain rooms
-- keep project_id = NULL. The partial unique index enforces "at most one
-- project-based room per project" without constraining plain rooms.
ALTER TABLE workspace_room
    ADD COLUMN project_id UUID REFERENCES project(id) ON DELETE CASCADE;

CREATE UNIQUE INDEX idx_workspace_room_project_id
    ON workspace_room(project_id) WHERE project_id IS NOT NULL;
