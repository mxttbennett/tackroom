# SPEC: session hooks for Pi and OMP

Status: requested 2026-10-17. PR only; merge needs separate approval. Earlier specs are in git history (`git log -- SPEC.md`).

## Problem

Pi and OMP had no managed hook surface, so memory hooks (the AgentsView recent-sessions context at session start) reached Claude Code, Codex, Droid and Hermes but not them. Both run hooks as TypeScript extension modules, not shell commands: Pi loads `~/.pi/agent/extensions/*.ts` (Pi 1.0 `docs/extensions.md`), OMP loads `~/.omp/agent/hooks/{pre,post}/*.ts` (https://omp.sh/docs/hooks).

## Behavior

- **One generated module per hook.** `tackroom sync` writes `~/.pi/agent/extensions/tackroom-<name>.ts` and `~/.omp/agent/hooks/post/tackroom-<name>.ts` from an embedded template (`internal/app/extension_hook.ts`). The module runs the hook command with `/bin/sh -c`, a Claude Code-style JSON payload (`hook_event_name`, `cwd`, `agent`) on stdin and the configured timeout (default 60 s, Claude Code's default).
- **Events.** `SessionStart` maps to `session_start`; its `hookSpecificOutput.additionalContext` is returned from `before_agent_start` as a hidden custom message, which both harnesses send to the model with the next prompt. `SessionEnd` maps to `session_shutdown`. `Stop`, `PreToolUse`, `PostToolUse` and `UserPromptSubmit` need block or continue semantics the module does not reproduce and are reported as unsupported.
- **Status.** A module is synced when its bytes match the render, drifted otherwise (rewritten by sync), missing when absent.
- **`tackroom hook list` / `remove`** read the command and event back from generated modules; removing one deletes the file.
- **Docs.** README table and footnote, site matrix and Pi note.

## Verification

- Unit tests: render, write, synced, drifted on a timeout change, list as managed, remove; unsupported events; default timeout and safe file names.
- OMP 18.8.3, live: `omp -p --hook <generated module>` ran the hook (payload logged with `agent: "omp"`), injected the canary as a `tackroom-hook` custom message, and the model answered with the canary word, in both text and JSON mode.
- Pi 1.0.0: the module loaded, ran the hook and injected the custom message (JSON trace). The model reply could not be checked: every `openai-codex` model on this machine fails with "not supported when using Codex with a ChatGPT account". Pi's `docs/message-types.md` says custom message content is sent to the model as a user message.
- `go test ./...` passes.
