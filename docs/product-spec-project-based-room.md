# Project-Based Room — 产品 Spec

> 状态：产品决策已完成，待进入技术 Spec。
> 前置探索：见 `project_chat_plan.md`（projects 数据模型与现有 rooms 架构）。

## 产品定位

Project-based room 是 project 的 **AI-native 执行面**——在 project 已有 issue 管理能力之上，叠加一层多 agent 协作的 chat 界面。room 自动继承 project 的 repo/resource，agent 通过 orchestrator 编排获得对代码库的读写权限，member 在 chat 中 @agent 即可驱动代码修改。

## 核心决策

| # | 决策点 | 结论 |
|---|--------|------|
| 1 | 创建入口 | chat 页建 room 时增加 "Create from project" 按钮，列出来建 room 的 project |
| 2 | 每 project 唯一 | `room.project_id` 可空外键 + 非空时 UNIQUE 约束；普通 room 的 `project_id` 为 NULL，不受限 |
| 3 | 资源挂载 | 自动全量继承 project 下所有 ProjectResource，零配置 |
| 4 | 权限模型 | room × resource × agent 矩阵，默认 write（读写），可降级为 read（只读）。UI 先不暴露复杂矩阵 |
| 5 | 派活方式 | 复用现有 orchestrator 编排：member 发消息 @ 多个 agent，orchestrator 拆分为 issue / DAG，执行时带 room 的 repo/resource context |
| 6 | 写层方案 | 每 agent 开独立 git worktree / 分支并行改代码；完成后拉 verify agent 做 merge + 验证 |
| 7 | 编排策略 | 沉淀为 builtin skill `project-based-orchestration`（`multica-room-orchestrating` 的 project 特化版） |
| 8 | 生命周期 | 不能独立删除，随 project 走 |

### 补充说明

- **成员管理**：room 内成员（agent）可随时增减，不做特殊约束。
- **新 agent / 新 resource**：加入 room 的新 agent、或 project 新增的 resource，默认也是 write 权限（具体填充时机在技术 spec 定）。

## 范围外（MVP 不做）

- Skill 系统懒加载重构（独立的后续项目）

## 用户故事（简版）

1. 用户进入 chat 页面 → 点 "Create from project" → 看到未建 room 的 project 列表 → 选择一个 → room 自动创建，project 的 repo/resource 全量挂入。
2. 在 project-based room 里 @ 两个 agent 说"给 user-service 加一个 /health 接口"→ orchestrator 拆成实现 issue + 验证 issue → agent A 在自己 worktree 写代码 → agent B（verify）merge 并验证 → member 在 room 里看到结果。
3. 管理员在 room 设置里，把某 agent 对某 repo 的权限从 write 降为 read —— 该 agent 此后只能读代码不能改。
