---
name: multica-zhishu-docs
description: "Use when the user refers to 直书, wiki, 知识库, 文档空间, visitCode, or asks to search/read/create/write documents in that system. Teaches the zhishu-docs CLI contract: when to use find/ls/read/create/write/link, that '直书空间' means the remote docs system rather than the current workdir, that document/directory creation must happen through zhishu-docs instead of mkdir, and that the agent should inspect the remote hierarchy before creating siblings to avoid writing into the local filesystem by mistake."
user-invocable: false
allowed-tools: Bash(zhishu-docs *)
---

# Working with 直书 via zhishu-docs

Use this skill when the user clearly means the 直书 / wiki document system,
not the local task workdir.

Typical triggers:

- "在直书空间..."
- "去 wiki 里..."
- "查一下知识库文档"
- "新建一个目录/文档到直书"
- a `visitCode`

## Core rule

When the request targets 直书, do NOT use local filesystem commands like
`mkdir`, `touch`, or editing files in the current workdir as the primary action.

Instead, use `zhishu-docs` to inspect and mutate the remote document space.

If the user says "在直书空间新建一个 shared-workspace 目录", the default
interpretation is a remote 直书 folder/document operation, not a local
`./shared-workspace` directory.

## Commands

Search by keyword:

```bash
zhishu-docs find <keyword>
```

List the top-level libraries visible to the account:

```bash
zhishu-docs ls
```

List children under a known parent `visitCode`:

```bash
zhishu-docs ls <visitCode>
```

Read a document:

```bash
zhishu-docs read <visitCode>
zhishu-docs read <visitCode> --format markdown
zhishu-docs read <visitCode> --format text
zhishu-docs read <visitCode> --format html
```

Create under a parent node:

```bash
zhishu-docs create <parentVisitCode> --name "shared-workspace"
```

Write content to an existing node:

```bash
zhishu-docs write <visitCode> --content "..."
zhishu-docs write <visitCode> --file ./note.md
zhishu-docs write <visitCode> --content "..." --overwrite
```

Turn a `visitCode` into the browser URL:

```bash
zhishu-docs link <visitCode>
```

## Recommended flow

1. If the target parent is ambiguous, inspect before mutating:

```bash
zhishu-docs ls
zhishu-docs find <keyword>
```

2. If there are multiple possible parents, prefer listing the likely parent and
choosing the correct node before creating anything.

3. Only create when you know the parent `visitCode`.

4. Report back the returned `visitCode` or URL when relevant.

## Important interpretation rule

Words like "空间", "目录", "文档", "wiki", "知识库" are overloaded. When the
user explicitly says 直书 / wiki, bias toward the remote docs system.

Only fall back to the local workdir if the user explicitly asks for local files
or the request is obviously about the checked-out repository.

## Incorrect → correct

Incorrect for a 直书 request:

```bash
mkdir shared-workspace
```

Correct pattern:

```bash
zhishu-docs ls
zhishu-docs find shared-workspace
zhishu-docs create <parentVisitCode> --name "shared-workspace"
```
