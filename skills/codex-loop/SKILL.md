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

#### Optional: focused reviews with `prompt`

The `prompt` parameter maps to `codex review`'s trailing positional `[PROMPT]` argument — custom instructions that focus the review. Use it when the user asks to narrow the scope (e.g. "focus on security", "review only the concurrency changes", "check error handling in foo.go"). Pass their intent as `prompt`:

```
codex_review(
  folder = "/Users/yourname/code/myproject",
  scope  = "uncommitted",
  prompt = "focus on error handling and resource cleanup"
)
```

Leave `prompt` empty or omit it entirely for a general review (the default behavior).

### Step 4 — Summarize and fix

The verdict is the text under `--- Codex review ---` in the tool result. **Make the next-step decision by reading that text — not by the exit code.** `codex review` always exits 0, even when it lists problems.

- **If the verdict lists concrete issues or risks** → summarize them as a numbered list, apply fixes using the Edit tool (or Read then Write for larger changes), then re-review (Step 5).
- **If the verdict says it found no issues, looks clean, or approves the changes** → STOP. Report to the user that the review is clean. Do not loop again.

> **Note on the tool result:** the result contains only codex's verdict, not its progress/exec trace. The full live trace is in the desktop log `~/Library/Logs/Claude/mcp-server-hostrunner.log` (when `stream_output` is enabled, which is the default). If the result header shows a non-zero `codex exit N` (e.g. `Mode: uncommitted (codex exit 1)`), codex itself failed — a `--- codex stderr ---` section will be included. Treat that as a tool error to report to the user, not as review findings.

### Step 5 — Re-review

After applying all fixes, call `codex_review` again with the same parameters. If the review returns no issues or only informational notes, the loop is complete. Tell the user the review is clean.

### Step 6 — Repeat if needed

If new issues appear in the re-review, repeat from Step 4.

## Tips

- Always pass `scope: uncommitted` unless the user specifically asks for base or commit review.
- `scope: uncommitted` covers staged, unstaged, and untracked changes — `codex review` computes the diff itself.
- If the tool returns an error like `"got a sandbox path"`, you passed a `/sessions/...` path. Stop and ask the user for the correct host path.
- If the output is marked `[output truncated]`, the diff is very large. Consider asking the user to scope the review to fewer files or review a specific commit instead.
