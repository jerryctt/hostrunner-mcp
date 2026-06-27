---
name: codex-loop
description: >
  Run a codex review on my latest edits. Use when the user says things like
  "review my changes with codex", "run codex on my edits", "start an edit-review loop",
  "check my code with codex", or "do a codex review". After each round of file edits,
  calls the codex_review MCP tool with the connected folder's HOST path and scope
  uncommitted, summarizes the findings, and iterates until the review is clean.
---

# codex-loop skill

## What this skill does

After you (or the user) have edited files in the connected project folder, this skill:

1. Calls the `codex_review` MCP tool with the project's **host path** and `scope: uncommitted`.
2. Reads the findings returned by `codex_review`.
3. Summarizes the issues and suggests fixes.
4. Applies fixes using Read/Write/Edit tools.
5. Calls `codex_review` again to verify the fixes.
6. Repeats until the review reports no issues.

## Critical: always use the HOST path

The `folder` parameter to `codex_review` must be the **host machine path** — the path you would use in a terminal on the user's Mac or Linux machine.

**Correct:**
```
codex_review(
  folder = "/Users/yourname/code/myproject",
  scope  = "uncommitted"
)
```

**WRONG — never pass a sandbox path:**
```
codex_review(
  folder = "/sessions/abc123/mnt/myproject"   <- REJECTED
)
```

Determine the folder's HOST path from the user (e.g. the path they shared when connecting the folder, such as `/Users/yourname/code/myproject`). Never derive it from the sandbox mount path. If you are unsure, ask the user: "What is the absolute path to this project on your machine?"

## Step-by-step instructions

### Step 1 — Identify the host path

Ask the user for the absolute host path if it is not already known:

> "What is the absolute path to the project on your host machine? (e.g. /Users/yourname/code/myproject)"

### Step 2 — Confirm there are uncommitted changes

You can optionally run:

```
run_command(
  command = "git",
  args    = ["status", "--short"],
  folder  = "/Users/yourname/code/myproject"
)
```

If the output is empty there is nothing to review yet. Tell the user and wait for more edits.

### Step 3 — Call codex_review

```
codex_review(
  folder = "/Users/yourname/code/myproject",
  scope  = "uncommitted"
)
```

For a branch diff against a base ref (e.g. `main`):
```
codex_review(
  folder = "/Users/yourname/code/myproject",
  scope  = "base",
  base   = "main"
)
```

For a specific commit:
```
codex_review(
  folder = "/Users/yourname/code/myproject",
  scope  = "commit",
  commit = "<SHA>"
)
```

### Step 4 — Summarize and fix

Parse the `--- Codex review ---` section. For each finding:

- State the file and line range.
- Describe the issue in one sentence.
- Apply the fix using the Edit tool (or Read then Write for larger changes).

### Step 5 — Re-review

After applying all fixes, call `codex_review` again with the same parameters. If the review returns no issues or only informational notes, the loop is complete. Tell the user the review is clean.

### Step 6 — Repeat if needed

If new issues appear in the re-review, repeat from Step 4.

## Tips

- Always pass `scope: uncommitted` unless the user specifically asks for base or commit review.
- `scope: uncommitted` covers staged, unstaged, and untracked changes — `codex review` computes the diff itself.
- If the tool returns an error like `"got a sandbox path"`, you passed a `/sessions/...` path. Stop and ask the user for the correct host path.
- If the output is marked `[output truncated]`, the diff is very large. Consider asking the user to scope the review to fewer files or review a specific commit instead.
