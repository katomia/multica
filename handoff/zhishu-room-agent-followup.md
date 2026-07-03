# zhishu / room agent follow-up

## Why the agent created a local directory instead of using wiki

Root cause had two layers:

1. The agent had no skill teaching that "直书空间 / wiki" means the remote
   zhishu document system.
2. `zhishu-docs` existed in the pod, but it was installed at
   `/root/.local/bin/zhishu-docs` and that directory was not in the runtime's
   default `PATH`.

So the agent interpreted:

> 在直书空间新建一个 shared-workspace 目录

as a normal local workdir request and ran the equivalent of local filesystem
operations.

## Code changes made

### 1. New builtin skill

Added:

- [server/internal/service/builtin_skills/multica-zhishu-docs/SKILL.md](/Users/admin/code/multica/server/internal/service/builtin_skills/multica-zhishu-docs/SKILL.md)

This skill teaches:

- when "直书 / wiki / 知识库" should map to `zhishu-docs`
- that remote doc-space requests must not default to `mkdir`
- `find / ls / read / create / write / link` command patterns

### 2. Runtime bootstrap improvement

Updated:

- [deploy/k8s/codex-runtimes/runtime-bootstrap.sh](/Users/admin/code/multica/deploy/k8s/codex-runtimes/runtime-bootstrap.sh)

Added:

- `export PATH="/root/.local/bin:${PATH}"`
- install `zhishu-docs` from `/opt/zhishu-docs/zhishu-docs.py` when present
- write `~/.boss-ai/zhishu_apikey` from `ZHISHU_APIKEY` when present

This is the durable fix path for future pod recreation.

## Current limitation

These code changes are not live in the already-running pods yet.

Current pods were hot-patched manually before this change, but the durable
bootstrap path still needs to be rolled out so new pods inherit:

- PATH including `/root/.local/bin`
- `zhishu-docs`
- `ZHISHU_APIKEY` / `~/.boss-ai/zhishu_apikey`

## Next steps

1. Update the k8s runtime deployment/bootstrap payload so `/opt/zhishu-docs/zhishu-docs.py`
   is present in the pod image or mounted by ConfigMap.
2. Inject `ZHISHU_APIKEY` through k8s Secret/env.
3. Restart both runtime deployments.
4. Verify in pod:

```bash
command -v zhishu-docs
zhishu-docs --help
zhishu-docs find 测试
```

5. Verify agent behavior by asking it to create/list/read something in 直书.

## Product follow-up

If this should be selectable per agent, the next step is not more runtime work.
The next step is to bind `multica-zhishu-docs` to the target agent(s), or expose
it as a workspace skill and attach it during agent creation.
