# Product Spec: Room Git Collaboration v1.5

## Summary

在 project-based room 的基础上，新增一版面向“多人 + 多 agent 协作开发”的轻量工作流：

- room 继续作为协作入口
- room 中的 agent 只能访问自己被 `room_resource_grant` 授权的 repo / local directory
- room 发起的开发协作，先落到 issue 承载执行状态
- agent 可以基于授权 repo 拉代码、创建开发分支、提交 MR
- qa-agent 可以基于同一协作链路拉取分支并执行测试
- 人类成员在 room 内查看状态并决定是否 merge

这个版本是 `1.5` 版，不引入完整的 room-native change request 模型，优先复用现有 `issue / task / project resource / runtime` 体系完成闭环。

## Goals

1. 让 room 内的 agent 真正基于 room grant 执行开发任务，而不是只在 chat 层协作。
2. 支持开发 agent 和 qa-agent 围绕同一个 repo / 分支协作。
3. 保持 room 作为主要协作界面，让成员在 room 内看到开发与测试进展。
4. 尽量复用现有 issue/task 模型，降低首版复杂度。

## Non-Goals

1. 不在本版本引入完整的 room-native CR/MR 数据模型。
2. 不在本版本实现自动 merge。
3. 不在本版本做复杂 DAG 编排 UI。
4. 不在本版本支持一个 room message 同时驱动多个 repo 的复杂流水线。

## User Story

作为一个在 room 里协作的成员，
我希望：

- @开发 agent 后，它能访问当前 room 授权的代码仓库
- agent 能 checkout 一个开发分支并开始改代码
- agent 改完后能发起 merge request
- 我可以再 @qa-agent 基于同一分支执行测试
- qa-agent 测完后把结果同步回 room
- 我作为人类成员可以在 room 内决定是否 merge

## Core Workflow

### 1. Room 发起开发

用户在 project-based room 中发消息并 @ 某个开发 agent。

系统行为：

- 如果 room 绑定了 project，则任务上下文带上 `room_id + project_id`
- task claim 时，后端只下发当前 agent 在该 room 中有 grant 的 project resources
- agent 在任务中可通过 `multica repo checkout <url>` 拉取被授权 repo

### 2. 开发 agent 开始改代码

开发 agent：

- checkout 授权 repo
- 基于默认分支创建开发分支
- 在本地 workdir / local_directory 中开发
- push 分支
- 创建 merge request
- 在 room 中回报分支名和 MR 链接

### 3. Room 发起 QA

用户或 orchestrator 在 room 中 @ qa-agent。

qa-agent：

- 基于同一个 repo 和目标分支 checkout
- 执行测试
- 将测试结果同步回 room
- 更新 issue 状态/结果标记

### 4. 人类决策 merge

room 成员看到：

- 开发状态
- MR 链接
- QA 结果

然后人工决定是否 merge。

## Scope of New Functionality

### A. Room-grant-aware task execution

新增能力：

- room 发起的 task 在 claim 阶段读取 `room_resource_grant`
- 仅向当前 agent 暴露允许访问的 `project_resource`
- `github_repo` 资源进入 task `repos`
- `local_directory` 资源只在 daemon_id 匹配时作为可执行目录候选

这是 1.5 版最核心的新增能力。

### B. Room-driven dev/qa issue flow

新增能力：

- room 发起的开发协作可落为 issue 承载状态
- issue 用于承载：
  - 当前 repo
  - 当前分支
  - MR 链接
  - QA 结果

room 负责协作展示，issue 负责执行状态与历史。

### C. Branch / MR convention

新增约定：

- 首版支持默认开发分支策略
- agent 在完成开发后回传：
  - repo
  - branch name
  - MR URL

分支策略在首版不做复杂配置，采用一个统一默认规则即可。

### D. QA handoff

新增能力：

- room 中可明确触发 QA agent 对指定 repo/branch 执行测试
- QA 结果回写到 room 与 issue

## UX / Frontend Impact

本版本前端改动应控制在现有 room / project / issue 结构内。

### 1. Room Header / Room Meta

新增展示：

- 当前 room 绑定的 project
- 当前 room 可协作的资源数量

涉及组件：

- `RoomHeader`
- `RoomMetaPanel` 或同类侧边信息区

### 2. Room Resource Access Panel

新增展示：

- 当前 room 的资源列表
- 每个 agent 当前可访问哪些资源

用途：

- 让成员理解为什么某个 agent 能/不能操作某个 repo

涉及组件：

- `RoomResourceGrantTable`
- `RoomAgentAccessMatrix`

### 3. Room Message Composer

新增交互提示：

- 当用户 @开发 agent / @qa-agent 时，提示当前 room 是否存在可用 repo 资源
- 若 room 无可用资源，可给出轻量提醒

涉及组件：

- `RoomComposer`
- `MentionPicker`
- `ComposerHints`

### 4. Room Timeline / Activity

新增展示：

- agent 已 checkout 的 repo / branch
- MR 已创建
- QA 已开始 / 已完成
- QA pass / fail

这些信息首版可以先作为 system message 或 structured activity item 显示，不要求独立复杂视图。

涉及组件：

- `RoomMessageList`
- `SystemMessageCard`
- `ActivityTimelineItem`

### 5. Issue Detail / Issue Side Panel

新增展示：

- 来源 room
- repo
- branch
- MR URL
- QA 状态

这样 issue 仍然是执行状态的承载体。

涉及组件：

- `IssueDetailPanel`
- `IssueMetadataSection`
- `IssueStatusBadge`

### 6. Project Resource Management

已有 project resource 管理界面需要补足 room 协作语义：

- 标识哪些资源已被 room 使用
- 标识哪些 agent 已被 grant

涉及组件：

- `ProjectResourceList`
- `ProjectResourceRow`
- `GrantSummaryBadge`

## Functional Requirements

### Required

1. room 发起的 agent task 必须只看到被 grant 的资源。
2. 开发 agent 能在授权 repo 上执行 checkout。
3. 开发 agent 完成后能把 branch / MR 信息反馈回 room。
4. qa-agent 能围绕同一 repo/branch 执行测试。
5. room 成员能在 UI 中看到开发和 QA 的进展。

### Nice to Have

1. 在 room 中提供“发起开发 / 发起 QA”的快捷操作。
2. 在 issue 面板中直接跳转 repo / MR。
3. 在 room 中对开发、QA状态做 badge 展示。

## Backend Notes

1. room task enqueue 需要携带 `room_id`
2. claim task 需要按 `room_resource_grant` 过滤 `project_resources`
3. room chat 发起的开发/QA流程需要把执行上下文映射到 issue
4. repo checkout 默认仍通过 daemon `/repo/checkout` 完成

## Risks

1. room grant 只在 UI 层展示、但 claim 阶段未生效，会导致越权访问。
2. room 和 issue 双写状态如果没有约束，容易出现 UI 不一致。
3. local_directory 资源在多 agent 并发下仍需要依赖 daemon 侧 path lock。

## MVP Acceptance

满足以下条件即可认为 1.5 版完成：

1. 在 project-based room 中，agent 只能 checkout 自己被 grant 的 repo。
2. 开发 agent 能从 room 协作中完成一次：
   - checkout
   - 开发
   - push branch
   - 创建 MR
3. qa-agent 能基于该 branch 执行测试并把结果同步回 room。
4. 人类成员能在 room/issue UI 中看见 branch、MR、QA 结果并继续人工 merge。
