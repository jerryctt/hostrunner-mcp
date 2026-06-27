English | [繁體中文](README_zh.md)

# hostrunner-mcp

[![CI](https://github.com/jerryctt/hostrunner-mcp/actions/workflows/ci.yml/badge.svg)](https://github.com/jerryctt/hostrunner-mcp/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A stdio MCP server that runs as a **native host process** — giving Claude Cowork access to your host machine's `codex` CLI, git, and filesystem from inside its sandboxed editing environment.

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
│        │  codex_review(folder=/Users/…)  │             │
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

- **`codex_review`** — run a read-only `codex review` on your uncommitted changes, a base-branch diff, or a specific commit. `codex` computes the diff itself — no git handling on our side.
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

**Prerequisite — set `HOSTRUNNER_CONFIG`:** the launcher reads your config path from this environment variable. Create a config file from `examples/config.example.yaml` and set the variable before starting Claude Desktop/Code:

```bash
export HOSTRUNNER_CONFIG=~/.config/hostrunner/config.yaml
```

**macOS/Linux:** the bundled `bin/launch.sh` launcher downloads the matching release binary on first run and verifies it against the release `checksums.txt` before executing it. No manual binary install needed.

**Windows:** the launcher is not supported on Windows. Install the binary manually (Option 2 above) and register it via `claude_desktop_config.json` as described in the "Register with Claude Desktop" section.

> **Trust note:** the launcher downloads and runs a binary from GitHub Releases, checksum-verified against the release. Review the source at `bin/launch.sh` if you have concerns.

---

## Configuration

Create a config file (e.g. `~/.config/hostrunner/config.yaml`). The repository ships `examples/config.example.yaml` as a starting point — **the paths in it are placeholders; change them to match your own setup**.

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

# Maximum wall-clock time for a single tool invocation.
timeout: 180s

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

### `codex_review`

Run a read-only `codex review` of changes in a host git repository. Uses the native `codex review` subcommand, which computes the diff itself and does not modify files. Only the `codex` CLI is required — no git handling on our side.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `folder` | string | yes | Absolute **HOST** path to the git repo (e.g. `/Users/you/proj`). Never a `/sessions/…` sandbox path. |
| `scope` | string | no | `uncommitted` (default — staged+unstaged+untracked), `base` (against a base branch), or `commit` (a specific commit) |
| `base` | string | no | Base branch or ref; required when `scope=base` (e.g. `main`) |
| `commit` | string | no | Commit SHA; required when `scope=commit` |

**Returns:** a text block with the mode and the full `codex review` output.

```
Mode: uncommitted

--- Codex review ---
… codex findings …
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

The repository ships a ready-made Cowork skill at `skills/codex-loop/SKILL.md`. This skill teaches Cowork how to call `codex_review` automatically after each round of edits.

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

3. **Trigger a codex review.** Call the `codex_review` tool with the **host** path:
   ```
   codex_review(
     folder = "/Users/yourname/code/yourproject",
     scope  = "uncommitted"
   )
   ```
   Pass the host path — the same path you see in Finder or a terminal on your Mac — **not** the `/sessions/…` sandbox path.

4. **Read the findings.** The tool returns the mode and codex's review output.

5. **Iterate.** Fix the issues Cowork found, then call `codex_review` again. Repeat until the review is clean.

---

## Security

- **Command allowlist.** Only commands explicitly listed in `allowed_commands` can be invoked. Requests for any other command are rejected.
- **Root-directory containment.** Every path argument is resolved (including symlinks) and checked against `allowed_roots`. Requests for paths outside those roots are rejected.
- **No shell.** Commands are run as argv arrays via `os/exec`. There is no `sh -c`, no glob expansion, no shell injection.
- **Native host process, not Docker.** The server runs as a plain OS process, not inside a container. Running it in Docker would break access to the host's `codex` binary, auth tokens, and files — defeating its purpose entirely.
- **Host paths only.** Tools only accept absolute host paths (e.g. `/Users/…`). Any path beginning with `/sessions/` is rejected with a descriptive error pointing you to the correct host path.
- **Read-only codex.** `codex_review` uses the `codex review` subcommand, which is non-interactive and read-only — it computes the diff itself and never modifies files. No write or auto-apply flags are ever passed. The optional `codex_extra_args` config field lets you pass additional flags (e.g. model overrides) without affecting safety.

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
