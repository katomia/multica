-- Add orchestrator_agent_id to workspace_room so each room can track which
-- agent acts as its built-in multi-agent coordinator.
ALTER TABLE workspace_room
    ADD COLUMN orchestrator_agent_id UUID REFERENCES agent(id) ON DELETE SET NULL;
