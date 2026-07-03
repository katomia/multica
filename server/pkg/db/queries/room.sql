-- name: ListRooms :many
SELECT * FROM workspace_room
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: GetRoomInWorkspace :one
SELECT * FROM workspace_room
WHERE id = $1 AND workspace_id = $2;

-- name: CreateRoom :one
-- project_id is optional (sqlc.narg): plain rooms pass NULL, project-based
-- rooms pass the bound project. The partial unique index on project_id
-- enforces "at most one project-based room per project" at the DB layer.
INSERT INTO workspace_room (workspace_id, name, display_name, description, visibility, created_by_id, project_id)
VALUES ($1, $2, $3, $4, $5, $6, sqlc.narg('project_id'))
RETURNING *;

-- name: GetRoomByProjectID :one
-- Resolves the project-based room for a project, if any. Used by the
-- CreateProjectResource hook to backfill grants for the room's agents.
SELECT * FROM workspace_room WHERE project_id = $1;

-- name: UpdateRoom :one
UPDATE workspace_room
SET
    name = COALESCE(sqlc.narg('name'), name),
    display_name = COALESCE(sqlc.narg('display_name'), display_name),
    description = COALESCE(sqlc.narg('description'), description),
    visibility = COALESCE(sqlc.narg('visibility'), visibility),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteRoom :exec
DELETE FROM workspace_room
WHERE id = $1 AND workspace_id = $2;

-- name: ListRoomMembers :many
SELECT * FROM room_member
WHERE room_id = $1
ORDER BY joined_at ASC;

-- name: GetRoomMember :one
SELECT * FROM room_member
WHERE room_id = $1 AND member_type = $2 AND member_id = $3;

-- name: AddRoomMember :one
INSERT INTO room_member (room_id, member_type, member_id, joined_by_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (room_id, member_type, member_id) DO UPDATE
SET joined_by_id = EXCLUDED.joined_by_id
RETURNING *;

-- name: DeleteRoomMember :exec
DELETE FROM room_member
WHERE room_id = $1 AND member_type = $2 AND member_id = $3;

-- name: ListRoomMessages :many
SELECT * FROM room_message
WHERE room_id = $1
ORDER BY created_at ASC
LIMIT $2;

-- name: ListRecentRoomMessages :many
SELECT * FROM room_message
WHERE room_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: GetRoomMessageInRoom :one
SELECT * FROM room_message
WHERE id = $1 AND room_id = $2;

-- name: CreateRoomMessage :one
INSERT INTO room_message (id, room_id, sender_type, sender_id, message_type, content, metadata)
VALUES (COALESCE(sqlc.narg('id')::uuid, gen_random_uuid()), $1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: CreateRoomOrchestration :one
INSERT INTO room_orchestration (
    room_id, source_message_id, decision_source, decision_type,
    status, input_snapshot, decision_json, model_provider, model_name, error, applied_at, chat_session_id
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    sqlc.narg('model_provider'), sqlc.narg('model_name'), sqlc.narg('error'), sqlc.narg('applied_at'),
    sqlc.narg('chat_session_id')
)
RETURNING *;

-- name: GetRoomOrchestrationBySourceMessage :one
SELECT * FROM room_orchestration
WHERE source_message_id = $1;

-- name: GetRoomOrchestration :one
SELECT * FROM room_orchestration
WHERE id = $1;

-- name: ListRoomOrchestrations :many
SELECT * FROM room_orchestration
WHERE room_id = $1
ORDER BY created_at ASC;

-- name: UpdateRoomOrchestrationApplied :one
UPDATE room_orchestration
SET status = 'applied', decision_json = $2, error = NULL, applied_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateRoomOrchestrationFailed :one
UPDATE room_orchestration
SET status = 'failed', error = $2
WHERE id = $1
RETURNING *;

-- name: CreateRoomIssueLink :one
INSERT INTO room_issue_link (room_id, room_message_id, orchestration_id, issue_id, link_role)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (room_id, issue_id) DO UPDATE
SET room_message_id = EXCLUDED.room_message_id,
    orchestration_id = EXCLUDED.orchestration_id,
    link_role = EXCLUDED.link_role
RETURNING *;

-- name: ListRoomIssueLinks :many
SELECT * FROM room_issue_link
WHERE room_id = $1
ORDER BY created_at DESC;

-- name: GetRoomOrchestrationByChatSession :one
SELECT * FROM room_orchestration
WHERE chat_session_id = $1
ORDER BY created_at DESC
LIMIT 1;

-- name: GetWorkspaceRoomByID :one
SELECT * FROM workspace_room WHERE id = $1;

-- name: GetRoomByOrchestratorAgent :one
SELECT * FROM workspace_room
WHERE workspace_id = $1 AND orchestrator_agent_id = $2
LIMIT 1;

-- name: UpdateRoomOrchestratorAgent :one
UPDATE workspace_room
SET orchestrator_agent_id = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateRoomOrchestrationDecision :one
UPDATE room_orchestration
SET decision_type = $2, decision_json = $3, status = 'applied', applied_at = now()
WHERE id = $1
RETURNING *;

-- name: InsertIssueDependency :one
INSERT INTO issue_dependency (issue_id, depends_on_issue_id, type)
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateRoomOrchestrationAction :one
INSERT INTO room_orchestration_action (
    orchestration_id, action_key, mode, agent_id, title, stage, status,
    deliverable, depends_on, chat_session_id, issue_id, output
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7,
    $8, $9, sqlc.narg('chat_session_id'), sqlc.narg('issue_id'), $10
)
RETURNING *;

-- name: ListRoomOrchestrationActions :many
SELECT * FROM room_orchestration_action
WHERE orchestration_id = $1
ORDER BY stage ASC, created_at ASC;

-- name: GetRoomOrchestrationActionByChatSession :one
SELECT * FROM room_orchestration_action
WHERE chat_session_id = $1
ORDER BY created_at DESC
LIMIT 1;

-- name: GetRoomOrchestrationActionByIssue :one
SELECT * FROM room_orchestration_action
WHERE issue_id = $1
ORDER BY created_at DESC
LIMIT 1;

-- name: UpdateRoomOrchestrationActionStatus :one
UPDATE room_orchestration_action
SET status = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CompleteRoomOrchestrationAction :one
UPDATE room_orchestration_action
SET status = 'done', output = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: FailRoomOrchestrationAction :one
UPDATE room_orchestration_action
SET status = 'failed', output = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- ===========================================================================
-- Room resource grants (project-based room context access matrix)
-- ===========================================================================

-- name: ListRoomResourceGrants :many
SELECT * FROM room_resource_grant
WHERE room_id = $1
ORDER BY created_at ASC;

-- name: CreateRoomResourceGrantIfMissing :exec
-- Idempotent backfill: inserts a default 'write' grant only when no row
-- exists yet. ON CONFLICT DO NOTHING means existing grants (including ones
-- downgraded to 'read') are left untouched — this never clobbers an admin's
-- explicit permission change.
INSERT INTO room_resource_grant (room_id, resource_id, agent_id, access_level)
VALUES ($1, $2, $3, 'write')
ON CONFLICT (room_id, resource_id, agent_id) DO NOTHING;

-- name: UpdateRoomResourceGrantAccess :one
UPDATE room_resource_grant
SET access_level = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteRoomResourceGrantsForMember :exec
-- Cascade cleanup when an agent is removed from a room. Only meaningful for
-- agent members (member-type members never have grant rows).
DELETE FROM room_resource_grant
WHERE room_id = $1 AND agent_id = $2;

-- name: ListRoomAgentMemberIDs :many
-- Returns the agent member IDs for a room; used by the grant backfill helper
-- to enumerate who needs grants for a newly-added resource.
SELECT member_id FROM room_member
WHERE room_id = $1 AND member_type = 'agent';
