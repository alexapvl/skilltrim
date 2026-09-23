# skilltrim

skilltrim controls which agent skills enter model context. It discovers existing `SKILL.md` files, measures catalog cost, and changes exposure through reversible symlinks and generated routers.

It does not replace a skill package manager. Keep using tools such as Vercel `skills` to install and update skills. skilltrim decides which installed skills each agent can see.

## Why

Agents receive the name and description of every visible skill in their startup context. As your skill library grows, so does that context, even though most tasks use only one or two skills. This wastes tokens, adds noise, and makes unrelated skills compete for attention.

Skill package managers solve installation and updates, but not exposure. skilltrim fills that gap: keep your full library installed, expose common skills automatically, put specialized skills behind explicit invocation or a group router, and hide unused skills without deleting them. Every filesystem change can be previewed and rolled back.

## Case study

One real setup exposed 54 skills to each of two agent integrations. Of those, 22 belonged to one specialized tool family. Keeping every member visible made discovery noisy and charged startup context for descriptions that were rarely relevant outside that workflow.

skilltrim grouped those 22 skills behind one router per integration. The original skills stayed installed and available, but each agent only needed to discover the router first. The change was previewed, applied as reversible symlink operations, and verified with a clean follow-up plan.

| Integration | Visible skills | Context before | Context after | Estimated tokens saved |
|---|---:|---:|---:|---:|
| A | 54 → 33 | 21,422 chars | 15,069 chars | 1,588 |
| B | 54 → 33 | 21,831 chars | 15,542 chars | 1,572 |

Across both integrations, this removed 42 redundant catalog entries and saved 12,642 characters, or about 3,160 estimated tokens, from startup context. That is roughly a 29% reduction without uninstalling a skill. Token estimates use skilltrim's approximation of four characters per token.

## Status

Early development. Codex, Claude Code, Cursor, and OpenCode paths are supported on macOS and Linux. Explicit-only mode currently requires Codex or Claude Code.

skilltrim currently manages filesystem skills in each configured `skills_dir`. Harness-managed built-ins and plugin-bundled skills remain under their harness or plugin manager and are not included in skilltrim's context totals.

## Install

Install the latest macOS or Linux release without Go:

```bash
curl -fsSL https://raw.githubusercontent.com/alexapvl/skilltrim/main/install.sh | sh
```

The installer downloads the binary matching your OS and architecture, verifies its SHA-256 checksum, and installs it to `~/.local/bin`. Override the destination or pin a version with environment variables:

```bash
curl -fsSL https://raw.githubusercontent.com/alexapvl/skilltrim/main/install.sh \
  | SKILLTRIM_INSTALL_DIR="$HOME/bin" SKILLTRIM_VERSION=v0.1.0 sh
```

Go developers can install directly:

```bash
go install github.com/alexapvl/skilltrim/cmd/skilltrim@latest
```

Or build a local checkout:

```bash
go build -o skilltrim ./cmd/skilltrim
./skilltrim
```

## Ask your agent to audit your skills

After installing skilltrim, copy this prompt into your coding agent:

```text
Use `skilltrim` to audit my installed agent skills. Start read-only.

Inspect the dashboard, full skill catalog, diagnostics, and relevant local project manifests. Report:

- current skill count and startup context for each agent
- skills that should remain globally available
- skills better scoped to one or more projects
- related skills that could share a group router
- skills better set to explicit-only or off
- projected character and token savings
- discovery tradeoffs for every recommendation

Do not change configuration or skill links until I approve the recommendations. After approval, preview every operation, apply it, run diagnostics, and confirm that a follow-up plan contains no remaining operations or conflicts. Never edit or delete canonical skill sources.
```

## Commands

```bash
skilltrim                                      # compact status dashboard
skilltrim list --agent codex                   # discover skills and current modes
skilltrim mode set craft-ui explicit --agent codex
skilltrim group create asc
skilltrim group add asc 'asc-*' --agent codex
skilltrim move astro-framework --to-project ~/GitHub/site --agent all
skilltrim move astro-framework --to-project ~/GitHub/site --agent all --apply
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

Move a global skill into one or more projects with one failure-safe transaction:

```bash
skilltrim move astro-framework \
  --to-project ~/GitHub/site \
  --to-project ~/GitHub/docs \
  --agent all

# Review the preview, then apply it.
skilltrim move astro-framework \
  --to-project ~/GitHub/site \
  --to-project ~/GitHub/docs \
  --agent all \
  --apply
```

`move` previews by default. Applying disables the selected global links, creates project-local links, updates the config, and adds only those generated paths to each repository's local `.git/info/exclude`. Nothing is added to the repository's tracked `.gitignore`.

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
- skilltrim only creates, replaces, or removes symlinks in configured agent skill directories.
- Real files and directories cause plan conflicts and remain untouched.
- Every non-empty apply stores one rollback snapshot at `~/.local/state/skilltrim/latest.json`.
- A project move stores global links, project links, config, and local Git exclusions in one rollback snapshot. Rollback also removes activation directories created by the move when they are still empty.
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
