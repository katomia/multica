# Project-Based Room — MVP 技术 Spec

> 前置产品 spec：`docs/product-spec-project-based-room.md`

## 范围

MVP 实现 project-based room 的创建 + resource 自动继承。不含编排升级（`improve_dag_plan.md`）、不含 worktree 执行、不含 `project-based-orchestration` skill。

## 1. DB 改动

### 1.1 `workspace_room` 加 `project_id`

```sql
ALTER TABLE workspace_room
  ADD COLUMN project_id UUID REFERENCES project(id) ON DELETE CASCADE;

CREATE UNIQUE INDEX idx_workspace_room_project_id
  ON workspace_room(project_id) WHERE project_id IS NOT NULL;
```

- 可空：普通 room 的 `project_id` 为 NULL，不受约束。
- 非空唯一：每 project 至多一个 project-based room。
- `ON DELETE CASCADE`：project 删除时 room 自动清理。

### 1.2 新表 `room_resource_grant`

```sql
CREATE TABLE room_resource_grant (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id     UUID NOT NULL REFERENCES workspace_room(id) ON DELETE CASCADE,
    resource_id UUID NOT NULL REFERENCES project_resource(id) ON DELETE CASCADE,
    agent_id    UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    access_level TEXT NOT NULL DEFAULT 'write' CHECK (access_level IN ('read', 'write')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (room_id, resource_id, agent_id)
);

CREATE INDEX idx_room_resource_grant_room ON room_resource_grant(room_id);
CREATE INDEX idx_room_resource_grant_agent ON room_resource_grant(agent_id);
```

- 三元唯一约束 `(room_id, resource_id, agent_id)` 防止重复授权。
- `access_level` 枚举：`write`（默认）/ `read`（降级后）。

## 2. API 改动

### 2.1 `POST /api/rooms` 扩展

`CreateRoomRequest` 加可选字段：

```typescript
{
  name: string;
  display_name?: string;
  description?: string;
  project_id?: string;  // 新增
}
```

服务端事务内三步：

1. 创建 room（带 `project_id`）。
2. 查询 `project_resource WHERE project_id = ...`。
3. 对 room 内每位 agent × 每个 resource 写 `room_resource_grant(access_level='write')`。

### 2.2 `GET /api/projects?without_room=true`

在现有 `ListProjects` 上加可选过滤：只返回尚未有 project-based room 的 project。SQL 逻辑：

```sql
SELECT p.* FROM project p
WHERE p.workspace_id = $1
AND NOT EXISTS (
  SELECT 1 FROM workspace_room r
  WHERE r.project_id = p.id
  AND r.project_id IS NOT NULL
)
```

### 2.3 `room_resource_grant` 读写

- `GET /api/rooms/{id}/resource-grants` — 返回当前 grant 列表（按 agent 和 resource 分组，方便前端渲染矩阵）。
- `PATCH /api/rooms/{id}/resource-grants/{grantId}` — 修改 `access_level`（write ↔ read）。

### 2.4 自动补 grant 钩子

| 触发点 | handler | 行为 |
|---|---|---|
| agent 加入 room | `AddRoomMember` | 对该 room 下每个 resource 写 grant(agent, resource, write) |
| project 新增 resource | `CreateProjectResource` | 查询该 project 的 room（如有），对该 room 下每个 agent 写 grant |
| agent 被移出 room | `DeleteRoomMember` | 删除该 agent 在该 room 下的全部 grant |

## 3. 前端改动

### 3.1 chat 页布局：上下分屏

`apps/web/app/[workspaceSlug]/(dashboard)/chat/page.tsx`：

- 上半：room 列表（已有）+ "Create from project" button 在 room 列表顶部工具栏。
- 下半：当前选中 room 关联 project 的 resource 列表。

状态流：

1. 用户选中一个 room → 从 room 对象读取 `project_id`。
2. 若 `project_id` 非空 → 用 `projectResourcesOptions(wsId, projectId)` 拉 resource 列表，渲染 `ProjectResourcesSection`。
3. 若 `project_id` 为空（普通 room）→ resource 区域显示空状态或隐藏。

### 3.2 "Create from project" 流程

已存在的组件和 hook：

- 复用 `packages/views/projects/components/project-resources-section.tsx`
- 复用 `packages/core/projects/resource-queries.ts` 的 `projectResourcesOptions`

新增交互：

1. "Create from project" button → 弹出 popover/modal。
2. Modal 内调用 `GET /api/projects?without_room=true` ⇒ 渲染可选 project 列表。
3. 用户选择一个 project → 调用 `POST /api/rooms { ..., project_id }`。
4. 成功后刷新 room 列表，自动选中新 room，resource 面板随之展示。

### 3.3 resource grant 管理（MVP 精简）

- room 设置面板 > "Resource access" tab。
- 列表：每行 = `agent 名 × resource 名 × access_level`。
- 被降级为 `read` 的行高亮；默认 `write` 的行保持普通样式。
- 点击切换 write ↔ read（`PATCH /resource-grants/{id}`）。
- 仅展示例外项或全部展示，产品上先不做复杂矩阵。

### 3.4 前端改动清单

| 文件 | 改动 |
|---|---|
| `packages/views/rooms/components/rooms-page.tsx` | 加 "Create from project" button + project picker |
| `apps/web/app/[workspaceSlug]/(dashboard)/chat/page.tsx` | 上下分屏布局，集成 resource section |
| `packages/core/rooms/queries.ts` | 加 `projectWithoutRoomOptions`（如有需要） |
| `packages/core/rooms/mutations.ts` | `useCreateRoom` 支持 `project_id` 参数 |
| `packages/core/rooms/index.ts` | 导出新增的 query/mutation |

## 4. Verify Spec

### 4.1 基于 project 新建 room + resource 继承

**V1 — 端到端，手动：创建入口**

1. 准备一个 workspace，里面有一个 project（带至少一个 resource，如 github_repo），且该 project 尚未建 project-based room。
2. 进入 chat 页面，点击 "Create from project"。
3. 弹出的 project picker 只列出**未建 room 的 project**，已建 room 的不出现。
4. 选中该 project，提交。
5. 检验：room 列表中出现新 room，`project_id` 正确绑定。
6. 检验：`GET /api/rooms/{id}/resource-grants` 返回的 grant 列表覆盖了 room 内所有 agent × project 下所有 resource，每条 `access_level = "write"`。
7. 检验：chat 页下半部 resource 列表正确渲染该 project 的资源。

**V2 — 端到端，手动：@agent 能看到 resource**

1. 在上述 project-based room 里，以 member 身份发一条消息 @mention 一个 room 内 agent。
2. 检验：被 @ 的 agent 收到的执行上下文（issue description 或 chat action 的 prompt）中，包含该 project 下 resource 的列表及 access_level 信息。
3. 检验：agent 能区分哪些 resource 它可读写，哪些只读。

**V3 — API 级，后端测试：唯一性约束**

1. 对已建 project-based room 的 project，再次调用 `POST /api/rooms` 带同一个 `project_id`。
2. 检验：返回 409 Conflict 或 4xx，提示该 project 已有 room。
3. 补充：`GET /api/projects?without_room=true` 不返回该 project。

**V4 — API 级，后端测试：agent 加入时自动补 grant**

1. 在一个已有 resource 的 project-based room 里，通过 `POST /api/rooms/{id}/members` 新增一个 agent。
2. 检验：`GET /api/rooms/{id}/resource-grants` 返回结果中新增了该 agent × 所有 resource 的 grant（write）。
3. 检验：已存在的其他 agent 的 grant 不受影响。

**V5 — API 级，后端测试：resource 新增时自动补 grant**

1. 在一个已有 agent 的 project-based room 里，通过 `POST /api/projects/{id}/resources` 给该 project 新增一个 resource。
2. 检验：`GET /api/rooms/{id}/resource-grants` 返回结果中新增了该 resource × 所有 agent 的 grant（write）。

**V6 — API 级，后端测试：grant 降级**

1. 调用 `PATCH /api/rooms/{id}/resource-grants/{grantId}` 把某 agent 对某 resource 的 access_level 改为 `read`。
2. 检验：该 grant 的 access_level 变为 `read`。
3. 检验：同一 room 下其他 agent 对该 resource 的 grant 不变。

## 5. 不覆盖

- `improve_dag_plan.md` 的编排升级（plan/actions/modes）
- `project-based-orchestration` skill
- worktree 执行
- skill 懒加载重构
- resource grant 的复杂矩阵 UI
