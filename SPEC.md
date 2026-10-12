# SPEC: MCP removal, secrets and a calm doctor

Status: requested 2026-10-12 after the fix/first-run retest (findings N1, N2, N3, N7). PR only; merge needs separate approval. Earlier specs are in git history (`git log -- SPEC.md`).

## Behavior

- **Removal (N1).** Sync records, per agent, the MCP servers it actually wrote (never an entry the agent already had) (`$XDG_STATE_HOME/tackroom/mcp-servers.json`, default `~/.local/state/...`). A recorded server that is no longer wanted for that agent (removed from `tackroom.yaml`, disabled, or dropped from its `agents`) and still present natively is `mcp stale` in `status` (exit 1, `agents[].mcp.stale` in JSON). The next `sync` copies the native config into the backup directory and removes the entry. `mcp remove` also records the targets whose native entry still matches the removed definition, so copies written before tracking existed are cleaned up. Other entries are never touched; a Codex file that does not parse is never edited. Setup and the web UI list the removals and ask, like other destructive changes; a declined removal stays recorded. The record is written atomically after every run, also after a failure; an unreadable record is an error, not an empty record.
- **Secrets (N3).** `mcp import` names the redacted keys and warns that other targets get the literal `${KEY}` reference. OpenCode, Hermes and dsh follow the rule JSON and TOML already had: a `${KEY}` reference is satisfied by any native value and never replaces one. `mcp add` warns when `--env` stores a literal value. `mcp add`, `import` and `remove` print what they did and the next step.
- **Doctor (N2, N7).** A package younger than three days, or a registry that cannot be reached, is a warning, not a failure. `hermes hooks` passes when no hook targets Hermes. The exit-code contract is unchanged (warnings still exit 1); `doctor --json` `failed` separates failures.

## Out of scope

- Per-harness env reference syntax (OpenCode `{env:VAR}`, Codex `env_vars`).
- Backups for every native write, a restore command (N5), `doctor` reporting unreadable configs (N4).

## Acceptance tests

- `tackroom.yaml` drops a server: `status` reports it stale, `sync` removes it from Claude and Codex, keeps a hand-configured Codex server and the remaining managed one, and leaves a backup of the Codex config holding the old entry; `status` is then clean.
- Remove works and is idempotent for Claude, Codex, OpenCode, Hermes, dsh and OpenClaw; a broken Codex TOML is left byte-identical.
- A real secret in OpenCode, Hermes or dsh survives a sync of a `${KEY}` reference and counts as synced.
- `go test ./...` passes.
