# zhishu-docs on K8s Runtimes

Status: pending follow-up

## What was done

- Installed `zhishu-docs` into both k8s codex runtime pods:
  - `multica-codex-go-runtime-8fc5d86-mj258`
  - `multica-codex-python-runtime-675fbfd98b-29fl8`
- Install target inside both pods:
  - `/root/.local/bin/zhishu-docs`
- Verified in both pods:
  - `python3 -m py_compile /root/.local/bin/zhishu-docs`
  - `/root/.local/bin/zhishu-docs --help`

## Important detail

The Python file provided by the user had a broken quote sequence in `_normalize_api_key_text()`.
The installed copy was corrected so the script is syntactically valid and runnable.

## What is still missing

`zhishu-docs` is installed but not usable yet because the required credential is not configured.

It requires one of:

- env var: `ZHISHU_APIKEY`
- file: `~/.boss-ai/zhishu_apikey`

Without that, real commands like `find/read/ls/write/create/link` fail at runtime due to missing auth.

## Next step

Configure `ZHISHU_APIKEY` in both pods, then verify with a real call, for example:

```bash
/root/.local/bin/zhishu-docs find 测试
```

or:

```bash
/root/.local/bin/zhishu-docs read <visitCode>
```

## Suggested persistence improvement

Current install was done directly inside running pods, so it is not durable across pod recreation.

If this needs to survive rollout/restart, add the install into the runtime bootstrap path for both k8s codex runtimes.
