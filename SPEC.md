# SPEC: external skill sources as a separate step

Status: requested 2026-10-13 as P2 after the first-run retest (finding N9). Setup stays as it is; adding third-party skill sets is its own command. PR only; merge needs separate approval. Earlier specs are in git history (`git log -- SPEC.md`).

## Behavior

- **`tackroom skill sources`** lists the sources in `tackroom.yaml` (pin, skill count, excluded agents) and the ones installed outside tackroom: Claude Code plugins that ship skills (resolved through `known_marketplaces.json` and each marketplace's `marketplace.json`) and `npx skills` installs (`$XDG_STATE_HOME/skills/.skill-lock.json`, else `~/.agents/.skill-lock.json`). Each outside source gets a ready `add` command. `--json` prints both lists. No network.
- **`tackroom skill sources search <query>`** queries the skills.sh search API, groups hits by GitHub repo with summed installs, and marks repos already in `tackroom.yaml` or installed elsewhere.
- **`tackroom skill sources add <owner/repo[/path] | GitHub tree URL | git URL>`** clones the repo blob-less into the external cache and pins the newest commit at least 3 days old on the default branch (`--branch`, `--latest`). It finds skill directories (hidden directories and nested skills skipped), takes `--skills a,b`, `--all`, or a numbered pick on a terminal, and fails with the list otherwise. It writes one `external_skills` entry (`skill_dirs` from the picked skills' parents, `skills` allowlist unless `--all`, `materialize: true`) and a lock pin, then syncs unless `--no-sync`. It refuses a source already configured, and refuses to overwrite a different `~/.agents/skills/<name>` or a skill another source owns; an identical copy is adopted. A config write is rolled back if the lock write fails.
- **Claude Code plugins.** When the repo (and path) is an installed Claude Code plugin and Claude is enabled, the entry gets `exclude_agents: [claude]`, and `add` says so.
- **Config.** `external_skills` entries take an optional `name` (the cache directory, lock entry and `skill update` argument; default the repo basename) and `exclude_agents`. Two repos with the same basename need distinct names; `add` uses `<owner>-<repo>`. Names cannot contain path separators.
- **Doctor.** agnix findings inside skills a lock entry materialized are ignored like findings in `external/` clones, since that content is upstream's and the external audit covers it.
- **N9.** The generated `tackroom.yaml` comment points at `tackroom skill sources add`.

## Out of scope

- A curated catalog compiled into the binary; discovery uses skills.sh.
- Installing plugins' hooks, commands or MCP servers; only skills are added.
- Removing a source (`tackroom.yaml` edit plus `sync` covers it).
- `--all` following skills into directories upstream adds later.
- Locking `tackroom.yaml` against a concurrent `sync`; `add` re-reads both files right before writing and writes the lock first.

## Acceptance tests

- `add` of a local repo with a 10-day-old and a fresh commit pins the old one, materializes only the picked skills from it, links them for Claude and Codex, and a second `add` of the same repo fails. `--latest --all` pins the head, owns every visible skill and writes no allowlist.
- Without `--skills`/`--all` and no terminal, `add` fails listing the skills; on a terminal `1, tdd` picks two.
- A differing `~/.agents/skills/grill` stops `add` and leaves `tackroom.yaml` untouched; an identical one is adopted.
- `exclude_agents: [claude]` removes the source's links from Claude and keeps Codex; `status` is clean.
- Detection resolves URL and path plugin sources, skips plugins without skills, groups `npx skills` entries by repo, and ignores non-GitHub ones.
- Search groups registry hits by repo and drops non-repo sources.
- `go test ./...` passes; a sandbox run adds obra/superpowers (Claude excluded), mattpocock/skills, anthropics/skills (as `anthropics-skills`) and cursor/plugins/pstack, and `doctor` passes.
