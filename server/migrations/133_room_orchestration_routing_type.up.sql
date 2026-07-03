ALTER TABLE room_orchestration DROP CONSTRAINT IF EXISTS room_orchestration_decision_type_check;

ALTER TABLE room_orchestration
    ADD CONSTRAINT room_orchestration_decision_type_check
    CHECK (decision_type IN ('chat_only', 'ask_clarification', 'single_issue', 'issue_dag', 'orchestrator_routing'));
