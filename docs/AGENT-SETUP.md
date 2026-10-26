# Set up coding agents

[README](../README.md) · [Command reference](REFERENCE.md)

## Install the skill

Install [Portbook](INSTALLING.md) and Node.js with npm/npx, then choose your agent:

```sh
portbook install-skills --agent codex
```

For Claude Code:

```sh
portbook install-skills --agent claude-code
```

The default target is Codex. Installation makes the `portbook` skill available
across your projects through the [Vercel Skills CLI](https://github.com/vercel-labs/skills).
Follow its terminal prompts. Back up local skill edits before reinstalling,
since installation can replace them.

The [Portbook skill](https://github.com/sviatoslav/portbook-skills/blob/main/skills/portbook/SKILL.md)
contains the recommended worktree workflow. The skill and binary are maintained
separately; use the [command reference](REFERENCE.md) or installed binary's scoped
help for command behavior.

## Project configuration

Keep service names, default ports, startup interfaces, and hook commands in the
project's `AGENTS.md` or linked workflow documentation. For example:

```text
Services: api (PORT, default 8080), web (PORT, default 3000).
Creation script: scripts/setup-worktree.sh
Removal script: scripts/remove-worktree.sh
Run Portbook commands from the checkout root.
```

## Integration examples

These examples run from the checkout root.

### Creation script

In the script or hook run automatically after the worktree directory is created:

```sh
for service in api web; do
  portbook get -- "$service" >/dev/null 2>&1 ||
    portbook reserve -- "$service" >/dev/null || exit 1
done
```

### Service startup

Read each port, falling back to its default when lookup fails:

```sh
api_port="$(portbook get api 2>/dev/null || printf '8080\n')"
web_port="$(portbook get web 2>/dev/null || printf '3000\n')"
```

Pass the selected value through that service's `PORT` environment variable.

### Removal script

After stopping remaining worktree services, before deleting the directory:

```sh
portbook release --all
```
