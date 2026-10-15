# SPEC: v1.6 — Gemini CLI, remote MCP servers, restore

Status: requested 2026-10-14 for the 1.6 release before a Show HN. PR only; merge needs separate approval. Earlier specs are in git history (`git log -- SPEC.md`).

## Behavior

- **Gemini CLI target (`gemini`).** Detected by the `gemini` binary. Skills: Gemini CLI reads `~/.agents/skills` itself, so tackroom mirrors into `~/.gemini/skills` only when the config root is not `~/.agents`. MCP servers and hooks go to `~/.gemini/settings.json` (`mcpServers`; `hooks` with events mapped: UserPromptSubmit→BeforeAgent, Stop→AfterAgent, PreToolUse→BeforeTool, PostToolUse→AfterTool, PreCompact→PreCompress; SessionStart, SessionEnd and Notification as is; timeouts in milliseconds). Roles render to `~/.gemini/agents/<name>.md` as local subagents (`kind: local`, Gemini tool names). Root instructions link `~/.gemini/GEMINI.md` to the shared `AGENTS.md`. Other settings in the file stay.
- **Remote MCP servers.** A server has either `command` (stdio) or `url` (streamable HTTP) with optional `headers`; config validation enforces that. `tackroom mcp add NAME --url URL [--header 'Name: value']`. Rendered for Claude Code (`"type": "http"`), Codex (`url`, `bearer_token_env_var` for `Authorization: Bearer ${X}`, `env_http_headers` for a header that is exactly `${X}`, `http_headers` for literals; any other reference is an error naming the fix), Pi (native `~/.pi/agent/mcp.json`), Grok Build (`url`, `headers`), Cursor (`url`, `headers`, references as `${env:X}`) and Gemini CLI (`httpUrl`, `headers`). Other agents skip remote servers; `mcp add` says which. A header that references a variable is satisfied by any non-empty native value and never replaces one. `mcp import` reads remote entries and turns secret-looking headers into `${SERVER_HEADER}` references. SSE and per-agent OAuth settings are out of scope until requested.
- **`tackroom restore`.** Without arguments it lists backups under `$XDG_STATE_HOME/tackroom/backups` (default `~/.local/state/tackroom/backups`), newest first. `tackroom restore <id|latest> [--dry-run]` puts each backed-up path back. A backed-up directory whose target is now a link (or missing) is restored as a whole, so nothing is written through a link into the shared repo. The original of a file wins over numbered intermediate copies. Whatever is replaced is moved into a new backup first, so `restore latest` undoes a restore. Paths already identical are skipped.
- **Docs.** README table gains Gemini CLI; Pi's MCP column is native (`pi-mcp-adapter` only for older Pi); remote MCP and restore documented.

## Not changed

- Claude Code keeps `~/.claude/CLAUDE.md` linked to the shared `AGENTS.md`. Claude Code reads `AGENTS.md` only as project instructions; its user-level file is still `~/.claude/CLAUDE.md` (https://code.claude.com/docs/en/memory#agents-md).

## Acceptance tests

- Gemini end to end: role, MCP, hooks (AfterAgent, ms timeout, Notification), GEMINI.md link, no skill mirror under `~/.agents`, mirror elsewhere, second sync changes nothing, status clean.
- A remote server patched into each of the six formats contains the expected keys, no command, inspects as synced and imports back. A real `Bearer` value survives a `${VAR}` reference for Claude Code, Codex, Grok and Pi. OpenCode gets only stdio servers. Codex rejects a header it cannot read from the environment with an actionable error.
- Restore: dry run changes nothing; restore brings back a file, a skill directory over a link (shared copy untouched) and the original of a numbered file; `restore latest` brings the replaced state back; unknown ids are refused.
- `go test ./...` passes.
