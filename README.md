English | [繁體中文](README_zh.md)

# hostrunner-mcp

[![CI](https://github.com/jerryctt/hostrunner-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/jerryctt/hostrunner-mcp/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Local MCP server + Claude plugin for read-only Codex code reviews on your machine — an edit → review → edit loop, bridging the Claude sandbox to your host codex CLI.

---

## Purpose / Why

[Claude Cowork](https://claude.ai) edits code inside an isolated sandbox. That sandbox cannot run your host's `codex` CLI (it isn't installed there, and has no access to your auth tokens or local files). **hostrunner-mcp** bridges the gap:

- It runs as a **native process on your Mac or Linux machine**, spawned by Claude Desktop over stdio.
- It has full access to your host filesystem, your `codex` binary, and your auth credentials.
- Cowork edits files in a folder you share with the session, then calls the server's tools to trigger a read-only `codex` review on the host and receive the findings back.
- This enables a tight **edit → review → edit** loop without leaving Cowork.

---

## How It Works

```
┌─────────────────────────────────────────────────────────┐
│  Claude Cowork (sandbox)                                │
│                                                         │
│  ┌────────────┐   edits files   ┌──────────────────┐   │
│  │  AI agent  │ ──────────────► │  mounted folder  │   │
│  │  (Cowork)  │                 │  /sessions/…/mnt │   │
│  └─────┬──────┘                 └────────┬─────────┘   │
│        │  MCP tool call                  │             │
│        │  codex_review_start(folder=…)   │             │
└────────┼─────────────────────────────────┼─────────────┘
         │ stdio (MCP protocol)            │ host FS mount
         ▼                                 ▼
┌─────────────────────────────────────────────────────────┐
│  HOST MACHINE                                           │
│                                                         │
│  ┌──────────────────┐              ┌────────────────┐  │
│  │  hostrunner-mcp  │  codex review│  git repo      │  │
│  │  (native proc)   │ ────────────►│  /Users/…/proj │  │
│  │                  │              │  codex CLI     │  │
│  └──────────────────┘              └────────────────┘  │
│        spawned by Claude Desktop                        │
└─────────────────────────────────────────────────────────┘
```

---

## Features

- **`codex_review_start` / `codex_review_status`** — run a read-only `codex review` on your uncommitted changes, a base-branch diff, or a specific commit, as a **background job** on the host. `codex` computes the diff itself — no git handling on our side. The job model exists because Claude Desktop kills any single MCP tool call after ~180s, while real reviews often take longer; starting and polling are each fast calls, so the review itself can run as long as your configured `timeout`.
- **`run_command`** — run any allowlisted CLI (e.g. `git`, `codex`) in a host folder, as an argv array (never a shell).
- **Strict security** — command name allowlist, root-directory containment, symlink-resolved path checks, no shell ever.
- **Configurable** — allowed roots, allowed commands, timeout, max output size, and optional extra codex flags.
- **Audit log** — every tool invocation is logged to stderr via zerolog.
- **Extensible** — add other CLIs (e.g. `gemini`) by allowlisting them in config.

---

## Requirements

| Requirement | Notes |
|---|---|
| **codex CLI** | Must be installed and authenticated on the host (`codex` in `$PATH`) |
| **Claude Desktop** | Needed to register and spawn the MCP server |
| **Go 1.25+** | Only needed if building from source; pre-built release binaries need no Go |

---

## Installation

### Option 1: Install with `go install` (recommended if you have Go)

```bash
go install github.com/jerryctt/hostrunner-mcp/cmd/hostrunner@latest
```

The binary is placed in `$GOPATH/bin/hostrunner` (typically `~/go/bin/hostrunner`).

### Option 2: Download a release binary

Download the pre-built binary for your platform from the [Releases page](https://github.com/jerryctt/hostrunner-mcp/releases), extract it, and place `hostrunner` somewhere in your `$PATH` (e.g. `/usr/local/bin/hostrunner`).

### Option 3: Build from source

```bash
git clone https://github.com/jerryctt/hostrunner-mcp.git
cd hostrunner-mcp
make build          # produces ./hostrunner
```

### Option 4: Install as a Claude plugin (marketplace)

**In Claude Desktop:** go to Add marketplace → enter `jerryctt/hostrunner-mcp`, then install the **hostrunner** plugin.

**In Claude Code:**
```
/plugin marketplace add jerryctt/hostrunner-mcp
```
Then install the `hostrunner` plugin from the listing.

Installing via the marketplace installs both the MCP server (auto-started by the bundled launcher) and the `codex-loop` skill in one step.

**Prerequisite — create a config file:** the server looks for its config at `~/.config/hostrunner/config.yaml` by default. Create it from `examples/config.example.yaml` before starting Claude Desktop/Code:

```bash
mkdir -p ~/.config/hostrunner
cp examples/config.example.yaml ~/.config/hostrunner/config.yaml
# edit the file to match your paths
```

You can override the config path with the `-config` flag or the `HOSTRUNNER_CONFIG` environment variable.

> **macOS GUI apps and environment variables:** because macOS does not pass shell environment variables to GUI apps like Claude Desktop, setting `HOSTRUNNER_CONFIG` in your shell profile (`.zshrc`, `.bashrc`, etc.) has no effect when the plugin is launched from Claude Desktop. Place your config at the default path `~/.config/hostrunner/config.yaml` — the binary will find it without any environment variable.

**macOS/Linux:** the bundled `scripts/launch.sh` launcher downloads the matching release binary on first run and verifies it against the release `checksums.txt` before executing it. No manual binary install needed.

**Windows:** the launcher is not supported on Windows. Install the binary manually (Option 2 above) and register it via `claude_desktop_config.json` as described in the "Register with Claude Desktop" section.

> **Trust note:** the launcher downloads and runs a binary from GitHub Releases, checksum-verified against the release. Review the source at `scripts/launch.sh` if you have concerns.

---

## Configuration

The server resolves its config file in this order:
1. The path given by the `-config` flag, if provided.
2. The `HOSTRUNNER_CONFIG` environment variable, if set.
3. The default `~/.config/hostrunner/config.yaml`.

**Recommended:** place your config at `~/.config/hostrunner/config.yaml`. This is especially important when using the plugin with Claude Desktop on macOS, because GUI apps do not inherit shell environment variables — `HOSTRUNNER_CONFIG` set in your shell profile will not be visible to the server when launched from Claude Desktop.

Create a config file from the provided example — **the paths in it are placeholders; change them to match your own setup**:

```bash
mkdir -p ~/.config/hostrunner
cp examples/config.example.yaml ~/.config/hostrunner/config.yaml
```

```yaml
# Absolute host paths that tools are allowed to access.
# The tool rejects any request that resolves outside these roots.
allowed_roots:
  - /Users/yourname/code

# CLI executables that run_command may invoke.
# Only names listed here are allowed; the binary must be in $PATH on the host.
# codex_review only needs 'codex' — it does not run git itself.
# Add more (e.g. git, gemini) for use with run_command.
allowed_commands:
  - codex

# Maximum wall-clock time for a single command on the host (the codex review
# itself, or a run_command invocation). Must include a unit (e.g. 600s, 10m) —
# a bare number is rejected. Reviews run as background jobs, so this can be
# comfortably larger than the MCP client's ~180s per-call limit.
timeout: 600s

# Maximum bytes returned from a single tool invocation (output is truncated with a notice).
max_output_bytes: 200000

# Optional extra flags appended to every 'codex review' call (e.g. config overrides).
# codex_extra_args: ["-c", "model=o3"]

# Whether to tee codex/command output to the server's stderr in real time (default: true).
# Claude Desktop captures this in ~/Library/Logs/Claude/mcp-server-hostrunner.log.
# With stream_output enabled (the default), codex's live output is visible in that log.
# Set to false to silence it.
stream_output: true
```

> **Note:** the config file is read **once at server startup**. Claude Desktop spawns the server when it launches, so after editing `config.yaml` you must fully restart Claude Desktop for changes to take effect. The effective values are logged at startup to `~/Library/Logs/Claude/mcp-server-hostrunner.log` (look for the `config loaded` line — it shows the config path and the timeout actually in use).

---

## Register with Claude Desktop

Add the following to your Claude Desktop config (usually `~/Library/Application Support/Claude/claude_desktop_config.json` on macOS):

```json
{
  "mcpServers": {
    "hostrunner": {
      "command": "/usr/local/bin/hostrunner",
      "args": ["-config", "/Users/yourname/.config/hostrunner/config.yaml"]
    }
  }
}
```

Replace `/usr/local/bin/hostrunner` with the actual path to the binary (e.g. `~/go/bin/hostrunner`), and update the config path accordingly. The file `examples/claude_desktop_config.example.json` in this repo shows the same snippet.

Restart Claude Desktop after editing the config. You should see **hostrunner** appear in the MCP servers list.

---

## Available Tools

### `codex_review_start`

Start a read-only `codex review` of changes in a host git repository, as a **background job** on the host. Returns a `job_id` immediately; poll `codex_review_status` for the result. Uses the native `codex review` subcommand, which computes the diff itself and does not modify files. Only the `codex` CLI is required — no git handling on our side.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `folder` | string | yes | Absolute **HOST** path to the git repo (e.g. `/Users/you/proj`). Never a `/sessions/…` sandbox path. |
| `scope` | string | no | `uncommitted` (default — staged+unstaged+untracked), `base` (against a base branch), or `commit` (a specific commit) |
| `base` | string | no | Base branch or ref; required when `scope=base` (e.g. `main`) |
| `commit` | string | no | Commit SHA; required when `scope=commit` |
| `prompt` | string | no | Custom review instructions (e.g. `"focus on error handling"`). Only pass this for an explicitly focused review; omit for a general review. Combining `prompt` with any `scope` is safe: the codex CLI rejects scope flags alongside its positional `[PROMPT]`, so the server folds the scope into the prompt text. |

**Returns:** the `job_id`, mode, and folder.

```
Review started in the background.
job_id: 3fa4c19e02d1
mode: uncommitted
folder: /Users/you/proj
```

### `codex_review_status`

Fetch the status/result of a background review. Blocks up to ~50s waiting for completion (long-poll), then returns either the finished review or a `running` notice — call it again until it completes. Finished results stay retrievable for ~30 minutes.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `job_id` | string | yes | The id returned by `codex_review_start` |

**Returns (finished):**

```
status: completed (job 3fa4c19e02d1, elapsed 4m12s)

Mode: uncommitted (codex exit 0)

--- Codex review ---
… codex findings …
```

**Returns (still running):**

```
status: running (job 3fa4c19e02d1, elapsed 1m40s)
The review is still in progress. Call codex_review_status again with the same job_id.
```

### `run_command`

Run an allowlisted command (argv, no shell) in a host folder.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `command` | string | yes | Executable name; must be in `allowed_commands` |
| `args` | string[] | no | Arguments as a string array |
| `folder` | string | yes | Absolute **HOST** path inside an `allowed_root` |

**Returns:** exit code, elapsed time, stdout, and stderr.

```
exit=0 elapsed=1.23s
--- stdout ---
…
--- stderr ---
…
```

---

## Installing the Cowork Skill

The repository ships a ready-made Cowork skill at `skills/codex-loop/SKILL.md`. This skill teaches Cowork how to run `codex_review_start` / `codex_review_status` automatically after each round of edits.

To install it, copy the skill directory into your Cowork skills folder:

```bash
cp -r skills/codex-loop ~/.config/claude/skills/
```

(The exact skills directory may vary by your Cowork installation. Check Cowork's settings for the configured path.)

Once installed, you can trigger the skill in Cowork by saying things like:

- "Review my changes with codex"
- "Run codex review on my edits"
- "Start an edit-review loop"

---

## Usage

Here is a typical **edit → review → edit** session in Claude Cowork:

1. **Share your project folder** with the Cowork session. Cowork mounts it at `/sessions/<id>/mnt/yourproject`.

2. **Edit files** using Cowork's Read/Write/Edit tools. For example, Cowork edits `/sessions/<id>/mnt/yourproject/src/handler.go`.

3. **Start a codex review.** Call the `codex_review_start` tool with the **host** path:
   ```
   codex_review_start(
     folder = "/Users/yourname/code/yourproject"
   )
   ```
   Pass the host path — the same path you see in Finder or a terminal on your Mac — **not** the `/sessions/…` sandbox path. The tool returns a `job_id` immediately while the review runs in the background.

4. **Poll for the findings.** Call `codex_review_status` with the `job_id` until it returns `status: completed` with codex's review output. Each poll blocks up to ~50s, so a few calls cover a multi-minute review without ever hitting the MCP client's ~180s per-call limit.

5. **Iterate.** Fix the issues Cowork found, then start a new review. Repeat until the review is clean.

---

## Security

- **Command allowlist.** Only commands explicitly listed in `allowed_commands` can be invoked. Requests for any other command are rejected.
- **Root-directory containment.** Every path argument is resolved (including symlinks) and checked against `allowed_roots`. Requests for paths outside those roots are rejected.
- **No shell.** Commands are run as argv arrays via `os/exec`. There is no `sh -c`, no glob expansion, no shell injection.
- **Native host process, not Docker.** The server runs as a plain OS process, not inside a container. Running it in Docker would break access to the host's `codex` binary, auth tokens, and files — defeating its purpose entirely.
- **Host paths only.** Tools only accept absolute host paths (e.g. `/Users/…`). Any path beginning with `/sessions/` is rejected with a descriptive error pointing you to the correct host path.
- **Read-only codex.** `codex_review_start` uses the `codex review` subcommand, which is non-interactive and read-only — it computes the diff itself and never modifies files. No write or auto-apply flags are ever passed. The optional `codex_extra_args` config field lets you pass additional flags (e.g. model overrides) without affecting safety.

---

## Development

```bash
# Build the binary
make build

# Run all tests
make test

# Vet the code
make vet
```

---

## License

MIT — see [LICENSE](LICENSE).
