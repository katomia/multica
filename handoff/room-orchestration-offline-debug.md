# Room 编排 "Applied 但无 issue / 任务堆积" 离线调试手册

本文记录一次真实排查：在 room 里 `@fish1 写诗，@fish2 锐评`，UI 显示
`orchestrator_routing / Applied` 但**没有任何 issue 被创建**，且任务在队列里堆积。

结论先行：**不是 room 编排代码 bug，而是 agent runtime 全部离线**。系统缺少
"入队前检查 runtime 是否在线" 的防护，导致任务被静默入队、编排记录被乐观地标成
`applied`，任务却永远卡在 `queued` 没人执行。

---

## 0. 背景：这条链路是怎么跑的

`@` 提到 **2 个及以上** agent 时（`server/internal/handler/room.go:669`）：

1. `routeToRoomOrchestrator` 把消息包装成 `[Coordination request]`，开一个跟房间
   **Orchestrator agent** 的 chat 会话，`EnqueueChatTask` 入队一个 chat task。
2. **紧接着立刻**把 `room_orchestration` 记录写成 `status = "applied"`
   （`room.go:992`）。所以 UI 上的 **"Applied" 不代表成功**，只表示"已派发给编排 agent"。
3. 真正建 issue 靠 Orchestrator agent 的**回复**里带
   `<orchestrator-decision>{...json...}</orchestrator-decision>`，由
   `server/internal/service/task.go:2315` 解析后调用 `ApplyOrchestratorDecision`
   （`room.go:1274`）建 issue。

因此"没结果"必然发生在其中一环：任务没跑完 / 回复里没有合法 decision 块 /
decision.type 是 `chat_only`。而任务没跑完最常见的原因就是 **runtime 离线**。

---

## 1. 找到进程 / 端口，确认服务是否真的起来

```bash
# multica 相关进程
ps aux | grep -iE "multica|next|go run|cmd/server|daemon" | grep -v grep

# 监听端口（后端默认 8080，前端 3000，均来自 .env 的 PORT/FRONTEND_PORT）
lsof -iTCP -sTCP:LISTEN -P -n | grep -iE "server|node|next"

# 后端健康检查
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8080/health   # 期望 200

# 前端（Next.js dev 首个请求会触发按需编译，第一次可能超时，属正常）
curl -s -o /dev/null -w "http_code=%{http_code} time=%{time_total}s\n" --max-time 30 http://localhost:3000/
```

> 备注：`make start` 把后端/前端日志打到它自己那个终端的 stdout，**不落盘**。
> 如需落盘日志可参考 `scripts/check.sh`，它把日志写到
> `/tmp/multica-check-backend.log` 和 `/tmp/multica-check-frontend.log`。

---

## 2. 连接数据库

数据库跑在 docker（`make dev` 起的），容器名 `multica-postgres-1`，库名来自
`.env` 的 `POSTGRES_DB`（默认 `multica`）。worktree 会用 `.env.worktree` 里
独立的库名/端口。

```bash
cd /Users/admin/code/multica

# 从 .env 读连接信息（密码不回显）
PW=$(grep -hiE "^POSTGRES_PASSWORD=" .env | head -1 | cut -d= -f2-)
USER=$(grep -hiE "^POSTGRES_USER=" .env | head -1 | cut -d= -f2-); USER=${USER:-postgres}
DB=$(grep -hiE "^POSTGRES_DB=" .env | head -1 | cut -d= -f2-); DB=${DB:-multica}

# 确认容器与端口映射
docker ps --format '{{.Names}}\t{{.Ports}}' | grep -i postgres

# 通用查询封装（后面都用它）
psql() { docker exec -e PGPASSWORD="$PW" multica-postgres-1 psql -U "$USER" -d "$DB" "$@"; }
```

---

## 3. 关键排查：从编排记录倒推到 runtime

### 3.1 看最近的编排记录 —— status 和 error 是第一现场

```bash
psql -c "SELECT id, decision_type, status, chat_session_id, left(error,80) AS err, created_at
         FROM room_orchestration ORDER BY created_at DESC LIMIT 10;"
```

判读：

- `status = failed` + `error` 有值 → 任务被执行过但失败了，`error` 就是根因
  （本次最早一条报 `Your organization has disabled Claude subscription access for Claude Code`）。
- `status = applied` 但对应 room 里**没有 issue** → 高度可疑：要么 orchestrator 没回复，
  要么回复里没有合法 decision 块。继续往下查 chat 会话。

### 3.2 看 orchestrator 的 chat 会话有没有回复

用上一步拿到的 `chat_session_id`：

```bash
SID=<上面查到的 chat_session_id>
psql -c "SELECT role, left(content,400) AS content, created_at
         FROM chat_message WHERE chat_session_id='$SID' ORDER BY created_at;"
```

判读：

- **只有 `user` 消息、没有 `assistant`** → orchestrator 从没回复。说明任务根本没被执行
  （runtime 离线 / 没人领队列），而不是回复格式问题。→ 跳到 3.4 查 runtime。
- 有 `assistant` 回复但内容里**没有 `<orchestrator-decision>`** → 是 prompt/模型输出问题，
  解析在 `task.go:parseOrchestratorDecision` 返回空，`ApplyOrchestratorDecision` 不建 issue。
- 有 decision 块但 `type` 是 `chat_only`/空 → 按设计不建 issue（`room.go:1294`）。

### 3.3 看任务队列 —— 是 failed 还是卡在 queued

```bash
psql -c "SELECT status, failure_reason, left(error,90) AS err, created_at
         FROM agent_task_queue ORDER BY created_at DESC LIMIT 10;"
```

判读：

- 一批 `failed` + `failure_reason = agent_error.provider_auth_or_access` →
  runtime 起来了但底层 CLI 鉴权被拒（订阅/API key 问题）。
- 一批 **`queued` 且一直不变** → 没有任何 online runtime 去领这些任务。**这就是"堆积"**。
  注意：`queued` 的任务不会自动失败，所以编排记录会一直停在 `applied`，UI 永远显示
  "Applied" 却没有结果。

### 3.4 看 runtime 状态 —— 根因通常在这

```bash
psql -c "SELECT id, provider, runtime_mode, status, last_seen_at
         FROM agent_runtime ORDER BY last_seen_at DESC NULLS LAST;"
```

判读：

- 全部 `offline` → **根因确认**：没有可用 runtime，任务无人执行。
- `last_seen_at` 停在某个时间点不再更新 → daemon 那时之后就没再心跳（掉线/被杀/鉴权失败退出）。

---

## 4. 检查本机 daemon

```bash
# daemon 进程是否还在
ps aux | grep "multica daemon" | grep -v grep

# 重启 daemon（在你自己的终端用 ! 前缀执行，让输出进会话）
multica daemon stop
multica daemon start --foreground --device-name <你的device-name>
```

重启后再跑 3.4，确认 runtime 变回 `online`；`last_seen_at` 应开始持续更新。

---

## 5. 处理堆积的 queued 任务（可选）

runtime 恢复后，历史 `queued` 任务可能被重新领取并跑一遍。若不想跑历史积压，
先取消它们，再发一条新消息重新测试：

```bash
# 先看清楚要动哪些（务必先 SELECT 确认再改）
psql -c "SELECT id, status, created_at FROM agent_task_queue WHERE status='queued' ORDER BY created_at;"

# 取消堆积任务（示例；确认无误后再执行）
psql -c "UPDATE agent_task_queue SET status='cancelled' WHERE status='queued';"
```

> 生产环境不要直接改库；这是本地 dev 的应急手段。

---

## 6. 一键速查脚本

把 2~3 步串起来，快速定位：

```bash
cd /Users/admin/code/multica
PW=$(grep -hiE "^POSTGRES_PASSWORD=" .env | head -1 | cut -d= -f2-)
USER=$(grep -hiE "^POSTGRES_USER=" .env | head -1 | cut -d= -f2-); USER=${USER:-postgres}
DB=$(grep -hiE "^POSTGRES_DB=" .env | head -1 | cut -d= -f2-); DB=${DB:-multica}
psql() { docker exec -e PGPASSWORD="$PW" multica-postgres-1 psql -U "$USER" -d "$DB" "$@"; }

echo "== runtimes ==";      psql -c "SELECT provider,status,last_seen_at FROM agent_runtime ORDER BY last_seen_at DESC NULLS LAST;"
echo "== task queue ==";    psql -c "SELECT status,count(*) FROM agent_task_queue GROUP BY status ORDER BY 1;"
echo "== recent orch ==";   psql -c "SELECT decision_type,status,left(error,60) err,created_at FROM room_orchestration ORDER BY created_at DESC LIMIT 5;"
```

---

## 7. 本次根因小结

| 现象 | 真正原因 |
| --- | --- |
| UI 显示 `orchestrator_routing / Applied` 但无 issue | "Applied" 只表示已派发给编排 agent；建 issue 依赖 agent 回复，而 agent 从没回复 |
| 任务在队列堆积 (`queued`) | 两个 runtime 全部 `offline`，没人领队列 |
| 最早一次是 `failed` 而非 `queued` | 那次 runtime 还在，但底层 Claude Code 鉴权被拒（组织禁用订阅），任务执行后失败 |
| 后续全部 `queued` | 鉴权失败后 daemon/runtime 掉线，之后入队的任务再没被执行 |

**设计缺陷**：`EnqueueChatTask`（`task.go:759`）入队前只检查 `RuntimeID.Valid`，
不检查 runtime 是否 `online`；而 Quick-Create 路径（`issue.go:1887` 的
`isRuntimeOnline`）会在入队前拦截并返回 422 `agent_unavailable`。room 编排走的是
前者，所以在 runtime 离线时静默入队 + 乐观标 `applied`，制造了"假成功 + 堆积"。
修复方向见 `room-orchestration-offline-fix-plan`（计划）。
