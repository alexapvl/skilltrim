---
name: skilltrim
description: Create and place agent skills. Use when asked to add, write, or split a skill, decide whether a skill should be global or project-specific, or check that a new skill is not a duplicate. Asks which repos it belongs to before writing, and stops when an existing skill already covers the job or could absorb it.
---

# skilltrim

skilltrim decides which installed skills an agent can see. This skill decides whether a new skill should exist, where its files live, and which project groups expose it.

Canonical skill files are never written straight into `~/.cursor/skills`, `~/.claude/skills`, or `~/.agents/skills`. Those directories are links.

## Before writing

1. Run `skilltrim list --full` and read each skill's name and description. Compare the request at that level. Do not diff whole `SKILL.md` bodies.
2. If an existing skill already does this job, stop. Name it and do not create another.
3. If the request is a section of an existing skill, stop and say so. A separate skill costs its description on every startup for each agent that can see it, or one line in a project group router. Estimate that cost as `description characters / 4` tokens. Fold it into the existing skill unless the user still wants a separate skill.
4. Ask which repos the skill is for when the user did not say. Use AskQuestion when it is available.
   - Every repo: global skill. Do not put it in a project group. `create-pr` is the pattern.
   - One repo, or several but not all: one skill file, listed in each of those project groups.
5. Ask for the missing job, trigger, and output shape before creating files. File shape still follows Cursor's `create-skill` skill.

## Where the files go

| Kind | Canonical path |
|------|----------------|
| Personal skill | `~/GitHub/nest/home/skills/<name>/SKILL.md` |
| First-party tool skill | `<tool-repo>/skills/<name>/SKILL.md`, symlinked from `~/GitHub/nest/home/skills/<name>` |

Then run `~/GitHub/nest/scripts/link-skills.sh` so the harness directories can see it.

## Project groups

A project group is bound to one or more repos. Its router appears only in those repos. The model may select it, and the user may invoke it with `/<group>`. Do not set `disable-model-invocation` on a project router. Members are off in the global catalog. The same skill is a member of each group that should expose it. It is not copied.

```bash
skilltrim group create <group> \
  --project <repo> \
  --description "Skills for <repo>."
skilltrim group add <group> <skill> --agent all
skilltrim apply
skilltrim apply --project <repo>
```

Repeat `--project` on create when one group covers more than one repo, then run `apply --project` once for each repo. Repeat `group add` when the same skill belongs in more than one group. `apply` with no project hides the member globally. `apply --project` writes that repo's router.

Cursor's copy is a real directory at `<repo>/.cursor/skills/<group>`, not a symlink. That is what makes `/<group>` and automatic selection both work. Claude and Codex get the same invocable router in their project skill directories. `apply --project` records those paths in the repo's local Git exclude.

Create the group before the first member. An empty group has no router, so it costs nothing until a skill is added. The next `apply --project` after the first member creates the router in this same shape.

After `link-skills.sh` or nest bootstrap, run `skilltrim apply` again. Linking restores every personal skill to the global catalogs, and apply puts project members back behind their routers.

## Do not

- Put a skill used in every repo into a project group.
- Mention one product inside another product's skill.
- Edit or delete a canonical skill when the task is only to hide or regroup it. Change skilltrim config, then plan and apply.
