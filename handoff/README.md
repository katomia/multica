# Handoff Docs

## Files

- [agent-harness-architecture-notes.md](/Users/admin/code/multica/handoff/agent-harness-architecture-notes.md): 记录 agent harness 相关的架构思路和实现笔记，用来帮助理解房间协作能力和现有 agent 执行链路的衔接点。
- [embedded-im-for-agent-os.md](/Users/admin/code/multica/handoff/embedded-im-for-agent-os.md): 描述把类 IM 的 room/chat 体验嵌入 agent OS 工作流的产品和交互方向。
- [improve_dag_plan.md](/Users/admin/code/multica/handoff/improve_dag_plan.md): 记录 DAG/plan 编排能力的改进计划，和 room orchestration 的后续规划直接相关。
- [product-spec-project-based-room.md](/Users/admin/code/multica/handoff/product-spec-project-based-room.md): 说明 project-based room 的产品目标、约束和用户预期行为。
- [project_chat_plan.md](/Users/admin/code/multica/handoff/project_chat_plan.md): 描述 project chat / room chat 的整体落地计划和阶段拆分。
- [room-chat-handoff.md](/Users/admin/code/multica/handoff/room-chat-handoff.md): 这是本次新增 room-chat、multi-agent 协作、shared resources/repo 逻辑的代码级流程图和接手说明。
- [room-orchestration-offline-debug.md](/Users/admin/code/multica/handoff/room-orchestration-offline-debug.md): 记录 room orchestrator 离线或不可用时的排查方法和已知问题。
- [tech-spec-project-based-room.md](/Users/admin/code/multica/handoff/tech-spec-project-based-room.md): 说明 project-based room 的后端设计、数据模型和实现细节。

## Current Progress

已完成的部分：

- room 现在支持通过 `project_id` 绑定到 project，并限制一个 project 只能有一个 room。
- room-chat 主链路已经接通：
  - 普通消息可直接入房间
  - `/issue` 可直接从 room message 生成 issue
  - 单 agent mention 会创建 agent chat task
  - 多 agent mention 会优先走 room orchestrator
  - orchestrator 不可用时会 fallback 到逐 agent chat fan-out
- project-based room 的共享资源授权链路已经接通：
  - 创建 project room 时回填 room resource grants
  - 给 room 新增 agent 时回填 grants
  - 给 project 新增 resource 时回填 grants
  - 删除 agent member 时清理该 agent 在 room 内的 grants
  - admin 可查看并切换 `read` / `write` grant
- 为 room chat / orchestration / project-based room / room resource grant 增加了对应 migrations 和路由入口。

还需要做的部分：

- 确认并补齐 orchestrator reply 被解析并落库为 `chat_only` / `single_issue` / `issue_dag` / `plan` / `ask_clarification` 的完整链路文档。
- 补或核对测试覆盖，尤其是：
  - duplicate `project_id` room 冲突
  - resource grant backfill 和 cleanup
  - orchestrator unavailable fallback
  - project resource 新增后现有 room agents 的 grant 回填
- 核对前端是否已经完整消费：
  - room resource grants 列表和 patch 能力
  - project-based room 的入口和展示
  - orchestration / system message / issue links 的 UI 状态
- 继续梳理 runtime 侧如何消费 project resources，把“共享 repo/resource”从 handler 逻辑一路串到 agent 实际执行上下文。
