# SkillTrim

SkillTrim controls which agent skills enter model context. It discovers existing `SKILL.md` files, measures catalog cost, and changes exposure through reversible symlinks and generated routers.

It does not replace a skill package manager. Keep using tools such as Vercel `skills` to install and update skills. SkillTrim decides which installed skills each agent can see.

## Why

Agents receive the name and description of every visible skill in their startup context. As your skill library grows, so does that context, even though most tasks use only one or two skills. This wastes tokens, adds noise, and makes unrelated skills compete for attention.

Skill package managers solve installation and updates, but not exposure. SkillTrim fills that gap: keep your full library installed, expose common skills automatically, put specialized skills behind explicit invocation or a group router, and hide unused skills without deleting them. Every filesystem change can be previewed and rolled back.

## Status

Early development. Codex, Claude Code, Cursor, and OpenCode paths are supported on macOS and Linux. Explicit-only mode currently requires Codex or Claude Code.

SkillTrim currently manages filesystem skills in each configured `skills_dir`. Harness-managed built-ins and plugin-bundled skills remain under their harness or plugin manager and are not included in SkillTrim's context totals.

## Install

```bash
go install github.com/alexapvl/skilltrim/cmd/skilltrim@latest
```

Or build a local checkout:

```bash
go build -o skilltrim ./cmd/skilltrim
./skilltrim
```

## Commands

```bash
skilltrim                                      # compact status dashboard
skilltrim list --agent codex                   # discover skills and current modes
skilltrim mode set craft-ui explicit --agent codex
skilltrim group create asc
skilltrim group add asc 'asc-*' --agent codex
skilltrim plan --agent codex                   # preview exact filesystem changes
skilltrim apply --agent codex                  # apply and save rollback snapshot
skilltrim rollback                             # restore complete last apply
skilltrim doctor --agent codex
skilltrim tui
```

Successful commands and errors emit JSON on stdout. Add `--toon` for token-efficient TOON output. Progress and diagnostics use stderr.

## Modes

| Mode | Effect |
|---|---|
| `auto` | Direct skill link. Agent may select skill automatically. |
| `explicit` | Generated proxy blocks automatic invocation where adapter supports it. User can still invoke skill directly. |
| `group:<name>` | Individual skill disappears. One generated router represents group. |
| `off` | Skill is not linked into target agent. Source remains installed. |

Global and project-local scopes are separate from mode:

```bash
skilltrim mode set deploy off --agent codex --project ~/GitHub/example
skilltrim plan --agent codex --project ~/GitHub/example
```

Without `--agent`, read commands show all agents and `plan` or `apply` handles all configured agents. `mode set` requires one explicit agent to avoid accidental cross-agent changes.

## TUI

```bash
skilltrim tui --agent codex
```

Keys:

- `↑`/`↓` or `j`/`k`: move
- `space`: cycle auto, explicit, off
- `g`: assign next configured group
- `/`: search
- `tab`: switch agent
- `p`: preview plan
- `a`: review and apply
- `r`: reload filesystem and config
- `q`: quit

Changes remain staged until apply. Apply always shows a confirmation screen.

Plan output and TUI preview show context characters before and after changes, plus an estimated token saving.

Context cost counts skill name, description, and active path. Skills marked explicit-only by Codex `agents/openai.yaml` or Claude `disable-model-invocation` remain available but contribute zero startup context.

## Configuration

Default path: `~/.config/skilltrim/config.toml`

```toml
version = 1
sources = ["~/.local/share/agent-skills", "~/.agents/routed-skills"]

[settings]
data_dir = "~/.local/share/skilltrim"
state_dir = "~/.local/state/skilltrim"

[agents.codex]
skills_dir = "~/.agents/skills"

[agents.claude]
skills_dir = "~/.claude/skills"

[[groups]]
name = "asc"
members = ["asc-build-lifecycle", "asc-testflight"]

[[rules]]
skill = "asc-testflight"
agent = "codex"
scope = "global"
mode = "group:asc"
```

CLI mutations write this config atomically. Skill source parents are recorded automatically so turning a skill off does not make its source undiscoverable.

## Safety

- Canonical skill sources are never edited or deleted.
- SkillTrim only creates, replaces, or removes symlinks in configured agent skill directories.
- Real files and directories cause plan conflicts and remain untouched.
- Every non-empty apply stores one rollback snapshot at `~/.local/state/skilltrim/latest.json`.
- Rollback refuses to overwrite paths changed after apply.
- `plan` performs no writes.

## Development

```bash
go test ./...
go vet ./...
go build ./cmd/skilltrim
```

Tests use temporary homes and never touch live agent directories.

## License

MIT
