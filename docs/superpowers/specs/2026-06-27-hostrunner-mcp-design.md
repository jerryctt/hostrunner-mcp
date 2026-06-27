# hostrunner-mcp — Design Spec

Date: 2026-06-27
Status: Approved for planning
Author: Jerry (with Claude / Cowork)

## 1. Problem & Purpose

Claude Cowork edits source code inside a sandbox. Its Bash tool runs in an
isolated Linux container, so it **cannot** execute host binaries such as the
`codex` CLI: codex is not installed in the sandbox, it is not authenticated
there, and the sandbox cannot see the host filesystem (only folders the user
mounts into Cowork). The existing `codex` plugin works only in Claude Code's
terminal, where Bash runs directly on the host.

`hostrunner-mcp` bridges that gap. It is a small Go MCP server that runs **as a
native process on the host** (spawned by Claude Desktop over stdio). Because it
runs on the host, it has the host filesystem, the user's codex auth, and the
`codex` binary available. Cowork edits files in a mounted folder, then calls the
server's tools to run a codex review on the host and get findings back —
enabling an `edit → review → edit → review` loop.

### Goals

- Let Cowork trigger a codex review of changed code on the host and receive
  structured findings, without leaving the chat.
- Provide a narrow, safe generic command runner (`run_command`) so other host
  CLIs (e.g. gemini) can be added later just by allowlisting them.
- Lowest practical host footprint; single static binary; easy to publish on
  GitHub as a public repo.

### Non-goals

- No arbitrary-shell execution (no shell interpretation, no pipes/redirects).
- No Docker/container deployment (see §7 — it conflicts with using host auth).
- codex does not write code in this design; Cowork does the editing, codex
  reviews read-only. (Generic `run_command` is for the user's own future uses.)
- Not packaged as a shareable plugin in v1 — built for Jerry's machine first.

## 2. Constraints & Key Decisions

- **Transport:** local **stdio MCP server**, registered as a connector in
  Claude Desktop. Claude Desktop spawns it on the host — no manually managed
  daemon.
- **Language/framework:** Go + `github.com/mark3labs/mcp-go` (the mature
  community SDK; also what `sonirico/mcp-shell` uses). Official
  `modelcontextprotocol/go-sdk` considered but mcp-go chosen for maturity.
- **Why not reuse `mcp-shell`:** mcp-shell is mcp-go plus a generic-shell safety
  layer (`mvdan.cc/sh` AST parsing, audit, yaml config). Its core value is
  running *arbitrary shell* safely — a problem we don't have. We only ever run
  fixed-argv commands (`codex`, `git`), so direct argv `exec` (no shell) is
  simpler and safer. We borrow mcp-shell's *ideas* (allowed roots, audit log,
  timeouts, structured results), not its generic-shell machinery.
- **Path rule:** the server runs on the host and only accepts **host paths**.
  Cowork must pass the host path it uses with Read/Write/Edit (e.g.
  `/Users/jerryctt/code/...`), never a sandbox mount path (`/sessions/.../mnt/...`).
  The server rejects paths outside the configured `allowed_roots`.
- **Codex assumption:** codex CLI is already installed and authenticated on the
  host. The server invokes it; it does not manage install/auth (but reports a
  clear error if codex is missing or unauthenticated).

## 3. Architecture

```
Cowork (sandbox)
   │  edits files in mounted folder (host path P)
   │  MCP tool call: codex_review(folder=P, scope=working-tree)
   ▼
Claude Desktop ── spawns ──► hostrunner (Go, stdio MCP) on HOST
                                  │ validate P ∈ allowed_roots
                                  │ argv exec (no shell), with timeout
                                  ▼
                               codex / git  (run in P on host)
                                  │ stdout/stderr/exit
                                  ▼
                          structured result ──► back to Cowork
```

## 4. Components (single responsibility each)

- **`cmd/hostrunner/main.go`** — entry point. Loads config, constructs the
  server, serves stdio.
- **`internal/config`** — loads/validates configuration: `allowed_roots`,
  `allowed_commands`, `timeout`, `max_output_bytes`. YAML file with env
  overrides. Path containment helper (is a given path inside an allowed root,
  after symlink resolution).
- **`internal/exec`** — the executor. Runs a command as an **argv array via
  `os/exec` (no shell)**, in a given working directory, with a context timeout;
  kills the process group on timeout; captures stdout/stderr/exit code/elapsed;
  truncates output to `max_output_bytes`. Returns a structured result.
- **`internal/server`** — mcp-go wiring; registers tools and maps tool args to
  config validation + exec calls; formats results for MCP.
- **`internal/codex`** — the `codex_review` preset: from `scope`, build the diff
  context (`working-tree` / `staged` / `branch`) via `git`, assemble the codex
  review invocation (read-only, no `--write`), run it through `internal/exec`,
  and shape the output into structured findings.

## 5. Tools (MCP surface)

### `codex_review` (flagship)

- **Params:** `folder` (host path, required), `scope`
  (`working-tree` | `staged` | `branch`, default `working-tree`),
  `base` (ref, optional, for `branch` scope).
- **Behavior:** validate `folder` ∈ `allowed_roots`; ensure it's a git repo;
  determine the diff for the scope; run codex review read-only; return findings
  plus raw output and metadata (scope, files touched, exit code, elapsed).
- **Read-only:** never passes `--write`; codex does not modify files.

### `run_command` (generic, narrow)

- **Params:** `command` (must be in `allowed_commands`), `args` (string array),
  `folder` (host path, must be ∈ `allowed_roots`).
- **Behavior:** argv exec, no shell. Returns the structured exec result.
- **Purpose:** extensibility — adding another host CLI later (e.g. gemini) is
  just an allowlist entry; no code change.

## 6. Configuration

`config.yaml` (path passed via flag/env; `examples/config.example.yaml` shipped):

```yaml
allowed_roots:
  - /Users/jerryctt/code
allowed_commands:
  - codex
  - git
timeout: 180s            # per command
max_output_bytes: 200000 # truncate beyond this
```

Env overrides for the config path and key fields. Claude Desktop registration
lives in `examples/claude_desktop_config.example.json` (an `mcpServers` entry
pointing at the built binary + config path).

## 7. Why not Docker

mcp-shell offers a Docker deployment to *isolate* execution — a feature for
untrusted shell, but a blocker for us. codex needs three things present at
runtime: the codex binary, the host auth credentials (`~/.codex` / ChatGPT
login / API key), and the project files. None are in a container by default;
mounting all of them in defeats the "use my host setup, lowest footprint"
goal. So hostrunner runs as a native host process, not in Docker.

## 8. Error Handling

Each returns a clear, structured error:

- folder not within `allowed_roots`; folder not a git repo (for codex_review).
- command not in `allowed_commands`.
- codex/git binary not found, or codex not authenticated.
- timeout → kill process group, return partial output + timeout note.
- output exceeds `max_output_bytes` → truncate + mark truncated.
- sandbox-style path passed (`/sessions/...`) → explicit "pass a host path" error.

## 9. Security

- Allowlist on both commands and root directories; path containment checked
  after symlink resolution.
- No shell at any point — argv arrays only; no injection surface.
- Native host process (no extra runtime); per-command timeout; output caps.
- Audit log (one line per invocation: command, folder, exit, elapsed) via a
  structured logger (zerolog-style), to stderr/file.

## 10. Testing

- **Unit — config:** path containment (inside/outside roots, symlink escape),
  command allowlist, config load/validate.
- **Unit — exec:** argv passing (no shell interpretation), timeout kills the
  process group, output truncation, exit-code capture.
- **Unit — codex:** scope → git diff selection; invocation assembly is
  read-only; output shaping. Use a fake `codex` stub binary on PATH to verify
  the round trip without real codex.
- **Integration:** end-to-end through the MCP server with the stub.
- **Manual:** a real codex review on a sample repo, driven from Cowork.

## 11. Repo Layout (public GitHub repo)

```
hostrunner-mcp/
├── cmd/hostrunner/main.go
├── internal/
│   ├── config/
│   ├── exec/
│   ├── server/
│   └── codex/
├── skills/codex-loop/SKILL.md        # Cowork skill: edit→review loop
├── examples/
│   ├── config.example.yaml
│   └── claude_desktop_config.example.json
├── .github/workflows/{ci.yml,release.yml}
├── go.mod / go.sum
├── Makefile
├── .goreleaser.yaml                  # cross-platform release binaries
├── .gitignore
├── LICENSE                           # MIT
├── README.md                         # English (primary)
└── README_zh.md                      # Traditional Chinese
```

- `skills/codex-loop/SKILL.md`: a Cowork skill that wraps the
  edit → `codex_review` → edit loop, shipped in-repo with install steps.
- Releases via goreleaser so users don't need Go installed.
- README in English (primary) + README_zh in Traditional Chinese, with a
  language switcher line at the top of each.
- No CONTRIBUTING.md in v1 (per decision).

### README.md section outline (English, primary)

1. Language switcher + one-line description + badges (CI, license, Go version)
2. Purpose / Why (the sandbox→host bridge; the codex edit→review loop)
3. How it works (the diagram in §3)
4. Features
5. Requirements (Go only to build; host codex installed+authed; git)
6. Installation (`go install` / download release binary / build from source)
7. Configuration (`config.yaml`: allowed_roots / allowed_commands / timeout /
   max_output_bytes; env overrides)
8. Register with Claude Desktop / Cowork (`mcpServers` snippet)
9. Available tools (`codex_review`, `run_command` — params & return shape)
10. Installing the Cowork skill (`codex-loop`)
11. Usage (a worked edit→review→edit example)
12. Security (allowlist, no shell, native host process, not Docker, host-path rule)
13. Development (build / test)
14. License

`README_zh.md` mirrors the same sections in Traditional Chinese.

## 12. Open Assumptions (validate during implementation)

1. Claude Desktop on this machine supports registering a custom local stdio MCP
   server. If not, fall back to the file-bridge daemon approach.
2. Cowork consistently exposes the host path of the connected folder (the path
   Read/Write/Edit use), so it can be passed to the server.
