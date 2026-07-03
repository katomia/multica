ALTER TABLE room_orchestration DROP CONSTRAINT IF EXISTS room_orchestration_decision_source_check;

ALTER TABLE room_orchestration
    ADD CONSTRAINT room_orchestration_decision_source_check
    CHECK (decision_source IN (
        'direct_command',
        'mention_orchestrator',
        'only_issue_command',
        'single_agent_mention',
        'multi_agent_mention'
    ));
