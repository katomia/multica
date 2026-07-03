DROP INDEX IF EXISTS idx_workspace_room_project_id;
ALTER TABLE workspace_room DROP COLUMN IF EXISTS project_id;
