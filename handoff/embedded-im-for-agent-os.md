# Embedded IM Layer For Agent OS

## Problem

If an agent platform depends on enterprise IM integrations first, every test or deployment has to deal with bot approval, callback URLs, signature verification, permissions, and tenant-specific policies.

For early product validation and self-hosted deployments, the platform needs an IM layer that works out of the box.

## Proposal

Provide an embedded IM layer similar to AgentTeams:

- Element Web as the browser chat client.
- Tuwunel / Matrix homeserver as the message backend.
- Matrix rooms as the visible coordination surface for humans and agents.
- Shared object storage, such as MinIO, for large artifacts and durable task state.

This acts like an internal Feishu/Lark for the agent platform.

## Core Model

```text
Human
  -> Element Web
  -> Matrix Room
  -> Manager / Team Leader / Worker Agent
  -> Runtime Harness
  -> MinIO shared workspace
```

Rooms carry control messages:

- who is being asked to act
- what task changed
- where the task files live
- whether the agent is blocked, done, or needs a decision

MinIO carries large context:

- task specs
- code and logs
- intermediate workspace files
- result reports
- shared knowledge

## Why This Helps

The IM layer handles communication, wakeup, visibility, and audit.

The file layer handles heavy context. Agents pass paths and summaries in the room instead of copying large documents through chat history.

```text
Room message:
  @qa please review task-123. Spec is at shared/tasks/task-123/spec.md.

MinIO:
  shared/tasks/task-123/spec.md
  shared/tasks/task-123/workspace/
  shared/tasks/task-123/result.md
```

## Design Boundary

Embedded IM should not replace enterprise IM integrations.

It should be the default internal collaboration substrate. Feishu, DingTalk, Slack, and other IMs can later be adapters that bridge external messages into the same room/task model.

```text
Feishu / Slack / DingTalk
  -> IM Adapter
  -> Agent OS room/task event
  -> Agent runtime
```

## Agent OS Implication

For the first version, agent-os can model:

- `Room`: visible collaboration channel.
- `AgentMember`: participant with runtime identity.
- `TaskArtifactPath`: pointer into shared storage.
- `MentionEvent`: wakeup signal for an agent.
- `TaskStatusEvent`: progress, blocked, completed, needs-review.

This keeps collaboration observable while avoiding early dependency on company IM approval workflows.
