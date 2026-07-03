# Projects 页面探索笔记

> URL `http://localhost:3000/dev/projects` 中 `dev` 是 **workspaceSlug**(工作区),真正的路由是 `apps/web/app/[workspaceSlug]/(dashboard)/projects/`。

## 路由与页面装配

```
apps/web/app/[workspaceSlug]/(dashboard)/projects/
├── page.tsx         → <ProjectsPage />        列表页
└── [id]/page.tsx    → <ProjectDetail id={} /> 详情页
```

Web 端的 `page.tsx` 只是薄壳,取出 URL 参数后委托给 `@multica/views/projects/components` 里的共享组件(遵循 web/desktop 共享约定)。

## 资源类(数据模型)

两个核心资源类,一对多:`Project 1 —— * ProjectResource`。

### 1. `Project`(`packages/core/types/project.ts` / 表 `project`,migration 034 + 035)

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` / `workspace_id` | UUID | 工作区隔离 |
| `title` / `description` / `icon` | text | 基本信息 |
| `status` | 枚举 | `planned / in_progress / paused / completed / cancelled`(DB CHECK 约束) |
| `priority` | 枚举 | `urgent / high / medium / low / none`(035 补加列) |
| `lead_type` + `lead_id` | 多态 | 负责人可以是 `member` 或 `agent`(与 issue assignee 同构) |
| `issue_count` / `done_count` / `resource_count` | int | 服务端聚合的滚动统计 |

关联:`issue` 表有 `project_id`(`ON DELETE SET NULL`)—— 项目是 issue 的可选归属容器。

### 2. `ProjectResource`(表 `project_resource`,migration 065)

多态的"外部资源指针",设计成 schema-free 扩展:

- `resource_type`:自由字符串。已知类型 `github_repo`(云端 git checkout)、`local_directory`(在指定 daemon 上就地执行)。
- `resource_ref`:JSONB,形状随 type 变化(`GithubRepoResourceRef` vs `LocalDirectoryResourceRef`)。
- 唯一约束 `(project_id, resource_type, resource_ref)` 防重复。

类型注释明确要求:新增 type 时前端 UI 必须走 default-case、服务端在 `validateAndNormalizeResourceRef` 加分支(符合 API 兼容规则)。

## 前端状态分层(遵循 State Rules)

- **服务端状态(TanStack Query)** — `packages/core/projects/`
  - `queries.ts`:`projectListOptions` / `projectDetailOptions`,query key 都带 `wsId`。
  - `mutations.ts`:`useCreateProject/Update/Delete`,乐观更新 + 回滚 + settle 时 invalidate。
  - `resource-queries.ts`:资源 CRUD,同样乐观更新。
- **客户端状态(Zustand)** — `stores/view-store.ts`:视图模式(compact 表格 / comfortable 卡片)、排序、隐藏列、多选过滤器,持久化且 workspace-aware;`draft-store.ts` 存创建草稿。
- **展示配置** — `config.ts`:status/priority 的顺序、颜色、badge 样式。

## 页面职责

**`ProjectsPage`(列表页)** — 工作区所有项目的总览与管理入口:
- 双视图切换(密集表格 / 卡片网格),排序 / 多维过滤(状态、优先级、负责人)、本地搜索、列显隐。
- 行级操作:改状态 / 优先级、删除、Pin 到侧边栏(集成 `@multica/core/pins`)、新建项目(走 modal)。

**`ProjectDetail`(详情页)** — 单个项目的工作台:
- 编辑标题 / 描述 / 状态 / 优先级 / 负责人。
- 内嵌该项目下的 issue 列表,复用 issues 的多种视图(Board / List / Gantt / SwimLane)、批量操作工具栏、按 assignee 分组。
- `ProjectResourcesSection`:管理挂载的外部资源(GitHub 仓库、本地目录),是 agent 执行任务的代码上下文来源。

## 后端 API

路由 `server/cmd/server/router.go:862`,handler 在 `project.go` / `project_resource.go`:

```
GET/POST         /api/projects          + /search
GET/PUT/DELETE   /api/projects/{id}
GET/POST         /api/projects/{id}/resources
PUT/DELETE       /api/projects/{id}/resources/{resourceId}
```

## 一句话总结

Projects 页面是"把 issue 组织成有生命周期、有负责人(人或 agent)、并挂载可执行代码资源(repo / 本地目录)的容器"的管理界面 —— ProjectResource 的多态设计正是为了让 agent 能在项目关联的真实代码环境里干活。
