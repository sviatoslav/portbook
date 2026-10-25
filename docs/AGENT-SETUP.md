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

The canonical skill lives in
[sviatoslav/portbook-skills](https://github.com/sviatoslav/portbook-skills).
The skill and binary are maintained separately; use the installed binary's
scoped help for supported commands and output options.

## Describe the project

Put these choices in the project's `AGENTS.md` or linked documentation:

- Service names, such as `api` and `web`.
- The owner directory for each service, expressed relative to the checkout.
- Permission to reserve a port when a booking is missing.
- The startup flag or environment variable that supplies the port.

For example:

```text
Use the checkout root as the Portbook owner directory.
The API service is named api. Start it with PORT set to its reserved port.
Agents may reserve api when Portbook confirms its booking is missing.
Keep the booking between server restarts.
```

## Agent workflow

1. Resolve the documented owner directory and service name.
2. Inspect bookings with `portbook list --directory "$owner"`.
3. Read the port with `portbook get api --directory "$owner"`.
4. If the result confirms a missing booking and allocation is authorized, run
   `portbook reserve api --directory "$owner"`.
5. Start the service with that port. Handle storage or permission errors before retrying.

Use the project's shutdown command to stop a service. Keep its booking for the
next start. For authorized worktree removal, release that owner's bookings while
the directory still exists:

```sh
portbook release --all --directory "$owner"
```

Resolve release errors before removing the directory. Pool changes and global
cleanup are separate, explicitly requested maintenance operations.
