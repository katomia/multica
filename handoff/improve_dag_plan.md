# Improve Room Orchestration DAG Plan

## Goal

Improve multi-agent room orchestration without introducing a separate DAG data model.

Use Multica's existing issue structure:

- `issue.parent_issue_id` as the orchestration root/children relationship
- `issue.stage` as fan-out/fan-in phase ordering
- `issue_dependency` as explicit dependency edges
- `issue.metadata` for room-orchestration action metadata
- existing issue/task/status/comment flow for execution and progress

## Current Problem

`ApplyOrchestratorDecision` treats orchestrator output as one of three mutually exclusive types:

- `chat_only`
- `single_issue`
- `issue_dag`

That is too limited for mixed requests such as:

> fish1 edits a file, fish2 and fish4 each produce a joke, fish3 saves fish2/fish4's jokes.

This is really:

- fan-out: fish1, fish2, fish4 run in parallel
- fan-in: fish3 waits for fish2/fish4 outputs

The existing `chat_only` branch only marks the orchestration applied and does not dispatch mentioned agents.

## Design Direction

Do not add `room_plan_node` / `room_plan_edge` tables yet.

Use fan-out/fan-in through stages:

- fan-out actions share the same `stage`
- fan-in actions use a later `stage`
- explicit `depends_on` keys become `issue_dependency` rows
- chat actions can be depended on by later issue actions, but they need a completion/output record
- issue actions waiting on incomplete dependencies start as `backlog` and become `todo` when dependencies complete

## Orchestrator Output Shape

Update the room orchestrator prompt to prefer a unified `actions` output:

```json
{
  "type": "plan",
  "title": "Complete the mixed room request",
  "actions": [
    {
      "key": "fish2_joke",
      "mode": "chat",
      "agent": "fish2",
      "title": "Produce a joke for downstream use",
      "stage": 1,
      "deliverable": "A joke text that fish3 can save",
      "depends_on": []
    },
    {
      "key": "fish4_joke",
      "mode": "chat",
      "agent": "fish4",
      "title": "Produce a second joke for downstream use",
      "stage": 1,
      "deliverable": "A joke text that fish3 can save",
      "depends_on": []
    },
    {
      "key": "fish3_save_jokes",
      "mode": "issue",
      "agent": "fish3",
      "title": "Save fish2 and fish4 jokes",
      "stage": 2,
      "deliverable": "The jokes are written to the requested location",
      "depends_on": ["fish2_joke", "fish4_joke"]
    }
  ]
}
```

Pure discussion should still use `type="plan"` with chat actions:

```json
{
  "type": "plan",
  "title": "Answer identity questions",
  "actions": [
    { "key": "fish1_identity", "mode": "chat", "agent": "fish1", "title": "Answer who you are", "stage": 1 },
    { "key": "fish2_identity", "mode": "chat", "agent": "fish2", "title": "Answer who you are", "stage": 1 }
  ]
}
```

Short-term compatibility should continue accepting:

- `chat_only` as a plan with chat actions for the originally mentioned agents
- `single_issue` as a one-action issue plan
- `issue_dag` as a plan with issue actions

## Functions To Change

### `server/internal/handler/room.go`

#### `roomOrchestratorInstructions`

Update the prompt text:

- Replace the current `chat_only / single_issue / issue_dag` framing with a `plan` made of `actions`.
- Explain that any output needed by another agent must be a named action with `key`.
- Explain that fan-out is represented by same-stage actions.
- Explain that fan-in is represented by later-stage actions with `depends_on`.
- Explain that actions may use `mode="chat"` or `mode="issue"`.
- Explain that chat actions may be dependencies; a later issue action should receive completed chat outputs as context.
- Keep compatibility examples for old output only if needed during transition.

#### `routeToRoomOrchestrator`

Keep this function.

Add stronger input guidance:

- Include whether the original message starts with `/issue`.
- Include that mixed conversation/task requests should produce `type="plan"`.
- Include that dependent conversational outputs should become named chat actions, not untracked prose.

No separate DAG object should be created here.

#### `ApplyOrchestratorDecision`

Refactor this function around a normalized plan.

Current behavior to replace:

- `chat_only` only updates the orchestration and returns.
- `single_issue` has a separate issue creation path.
- `issue_dag` has its own root/child creation path.

New behavior:

1. Parse orchestrator JSON.
2. Normalize JSON into one internal struct:

```go
type roomOrchestratorPlan struct {
  Type    string
  Title   string
  Actions []roomOrchestratorAction
}

type roomOrchestratorAction struct {
  Key         string
  Mode        string // "chat" or "issue"
  Agent       string
  Title       string
  Description string
  Deliverable string
  Stage       int32
  DependsOn   []string
}
```

3. Execute actions stage-by-stage.
4. For `mode="chat"`, enqueue a room-backed chat task for the target agent.
5. For `mode="issue"`, create:

- one root issue
- one child issue per issue action
- `stage` on every child issue
- `issue_dependency` for `depends_on`
- `room_issue_link` for root and children
- metadata on every created child issue
6. Store action outputs so later dependent actions can receive them as context.

#### New helper: `normalizeRoomOrchestratorDecision`

Purpose:

- Convert `plan`, `chat_only`, `single_issue`, and `issue_dag` into one internal action shape.
- Default missing action mode to `issue` for task-like legacy outputs.
- Default missing action stages to `1`.
- Default missing action key to a deterministic slug-like key.
- Preserve original JSON in `decision_json`.

#### New helper: `applyRoomActionPlan`

Purpose:

- Execute the normalized action plan.
- Create root issue only when at least one action uses `mode="issue"`.
- Enqueue chat actions.
- Create child issues for issue actions.
- Add metadata and dependencies.
- Update orchestration decision.

Important behavior:

- Fan-out chat actions do not create extra `room_orchestration` records.
- They belong to the original orchestration.
- A chat action can be depended on by a later issue action.
- Later actions wait until dependencies complete before moving to `todo`.

This should absorb most of the current `single_issue` and `issue_dag` logic.

#### New helper: `roomActionDependenciesReady`

Purpose:

- Check whether all `depends_on` actions for an action have completed.
- For issue dependencies, use the created child issue terminal state.
- For chat dependencies, use the completed chat action output.
- Only dependency-ready issue actions should be created/promoted as `todo`; otherwise they remain `backlog`.

#### New helper: `promoteReadyRoomActions`

Purpose:

- Run after a chat action completes or a child issue enters terminal state.
- Find later actions in the same orchestration whose dependencies are now complete.
- Promote their child issues from `backlog` to `todo`, or enqueue their chat tasks.
- Keep this idempotent so repeated completion events do not double-dispatch.

#### New helper: `roomNodeIssueMetadata`

Purpose:

Return JSON metadata for child issues:

```json
{
  "room_orchestration": {
    "orchestration_id": "...",
    "source_message_id": "...",
    "node_key": "fish2_joke",
    "stage": 1,
    "mode": "issue",
    "depends_on": ["..."],
    "deliverable": "..."
  }
}
```

#### New helper: `roomNodeIssueDescription`

Purpose:

Build a useful child issue description containing:

- original room message
- node-specific task
- expected deliverable
- dependency notes
- stage number
- completed chat outputs from dependency actions, when available
- whether the issue starts in `todo` or `backlog`

## Functions To Keep

### `createRoomSingleIssue`

Keep for the no-mention `/issue` branch:

```go
len(mentionedAgents) == 0 && isIssueCommand
```

It should continue creating a single backlog issue with no agent assignment.

### `createRoomChatOnlyWithType`

Keep for:

- single-agent mention
- dispatching orchestrator agent itself

Potential follow-up:

- Split lower-level chat-task creation into a helper that can enqueue a chat task without creating another orchestration.
- Use that lower-level helper for multi-agent chat actions.

## Parts To Delete Or Stop Using

### Stop treating these as final exclusive architecture

- `chat_only`
- `single_issue`
- `issue_dag`

They can remain as compatibility input values, but new orchestrator output should use:

- `plan`

### Remove duplicated issue creation paths in `ApplyOrchestratorDecision`

After `applyRoomActionPlan` exists, delete the separate `case "single_issue"` and `case "issue_dag"` bodies or reduce them to normalization.

### Do not add a new DAG table in this phase

Do not add:

- `room_plan_node`
- `room_plan_edge`
- independent node status tables

Use issue/stage/dependency first.

## Data Model Notes

No new DB table is required in this phase.

Potential migration only if needed:

- Add/allow `decision_type = 'plan'`

If we want to avoid a migration, store normalized plans as:

- `decision_type = 'issue_dag'` for `plan`

But the cleaner path is to add the new `plan` decision type.

## Tests To Add

### Backend

Add handler/service tests around `ApplyOrchestratorDecision`:

1. Chat-only action plans fan out to all mentioned agents and create no issues.
2. Issue action plans create root issue plus staged child issues.
3. Plans write `issue_dependency` rows from `depends_on`.
4. Issue actions depending on chat actions wait in `backlog` until chat completion, then become `todo`.
5. Old `chat_only` maps to chat actions for mentioned agents.
6. Old `single_issue` maps to one issue action.
7. Old `issue_dag` maps to issue actions.

### Manual Smoke Test

#### Smoke 1: mixed fan-out and fan-in

```text
@fish1 @fish2 @fish4 fish1 改一个文件，fish2 和 fish4 各讲一个笑话，fish3 把 fish2 和 fish4 的笑话落盘
```

Expected:

- root issue created
- fish1 issue action created in stage 1
- fish2/fish4 chat actions enqueued in stage 1
- fish3 issue action created in stage 2 as `backlog`
- fish3 issue action depends on fish2/fish4 chat action outputs
- when fish2/fish4 chat outputs complete, fish3 moves to `todo`
- room issue panel links fish1/fish3 issues

#### Smoke 2: parallel jokes, then judge

```text
@fish1 @fish2 @fish3 各自讲个笑话，@fish4 他们的笑话谁的最好笑？
```

Expected:

- root issue created
- fish1/fish2/fish3 chat actions enqueued in stage 1
- fish4 issue action created in stage 2 as `backlog`
- fish4 issue action depends on fish1/fish2/fish3 chat action outputs
- when all three jokes complete, fish4 moves to `todo`
- fish4 issue description includes the completed jokes and asks it to judge the funniest
- room issue panel links the fish4 issue

#### Smoke 3: mixed identity chat and dependent file work

```text
@fish1 你是谁 @fish2 写一首诗在我桌面 @fish3 等fish2写完了锐评一下他写的诗，并且把评论放到我的桌面上。
```

Expected:

- fish1 is treated as a chat action because its answer is not needed downstream
- fish2 child issue created in stage 1
- fish3 child issue created in stage 2
- fish3 child depends on fish2 child issue
- fish3 issue description includes that it should review fish2's completed poem and save the review to desktop
- room issue panel links fish2/fish3 issues

#### Smoke 4: pure multi-agent conversation

```text
@fish1 @fish2 你们是谁。
```

Expected:

- no issue created
- fan-out chat tasks are enqueued for fish1 and fish2
- fish1 and fish2 replies are written back to the room
- orchestration decision is stored as a `plan` with chat actions
