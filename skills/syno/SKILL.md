---
name: syno
description: "Answer questions about a Synology NAS (DSM) with the syno CLI or its MCP tools (syno_*): whether it is healthy, free space and the shared folders that use it, disks and temperatures, load, containers, package and DSM updates, and other DSM state such as users, backups and logs, and start, stop or restart its containers. Use when the user asks about their Synology, NAS or DSM."
license: MIT
---

# syno

syno reads the state of a Synology NAS through the DSM Web API. It never changes the NAS, and neither do you: when something needs fixing, tell the user what to do in DSM.

`syno -h` and `syno <command> -h` describe the commands, flags and output, and the descriptions of the `syno_*` MCP tools describe the tools. Read them before you use a command, rather than guessing; this skill does not repeat them, so that it does not go stale.

## Pick the route

1. If `syno_*` MCP tools are available, use them.
2. Otherwise use the CLI, with `--json` where the command has it. If `syno` is not installed, point the user to https://github.com/babarot/syno#install.

Each tool call and command uses the profile given by `--profile` or `SYNO_PROFILE`, or else the current profile, which the user can switch at any time. Every JSON answer has the `host` it came from: check it is the NAS the user asked about. `syno profile list` shows the NAS that syno knows.

## Answer from the commands first

- Is the NAS healthy, is anything wrong: `syno doctor` (`syno_doctor`). `syno doctor --list` tells what each check looks at.
- Space, pools, volumes, disks, temperature, CPU, memory, DSM version: `syno status` (`syno_status`). Disks show their power-on hours, and the life left when DSM estimates it (mostly SSDs; `-` or no `life_percent` means no estimate, not 0%).
- Containers of Container Manager: `syno container list` (`syno_containers`). Add `--usage` (`usage`) for the CPU and memory each running one uses.
- Package updates: `syno package list` (`syno_packages`).
- Shared folders and the space each uses: `syno share list` (`syno_shares`). Add `--recycle` (`recycle`) for what emptying the recycle bins would free.

Start broad and narrow down: a warning in doctor tells you which part to look at in status. In the answer, give the numbers and where they come from (the doctor check, or the field of status), and say which `host` answered when there is more than one NAS.

## Changing the NAS

syno changes the NAS only through these commands, and only from the CLI: the MCP tools never change anything.

- Start, stop or restart containers: `syno container start|stop|restart NAME...`.

Before running one, tell the user which containers you would act on and what you expect to happen, and run it only after they agree, with `--yes`, since there is no terminal to answer syno's own question. Name the containers exactly as `syno container list` shows them. If a container fails to start, report the reason syno prints; do not try to fix the NAS around it.

## Questions the commands do not cover

Shared folders, users, backups, logs, network and other DSM settings are only reachable through the DSM Web API. [references/dsm-apis.md](references/dsm-apis.md) lists APIs known to answer such questions.

1. Find candidates with `syno api --list <keyword>` or `syno_api_list`. The list has no secrets, so use it freely.
2. Before calling an API, ask the user. Even methods that only read can return secrets, such as the environment variables of containers or passwords in notification, DDNS and backup settings, and everything you read is sent to the provider of your model. Say which API you want to call and why, and call it only with their consent. Do not call APIs that exist to hold credentials, and use `syno container list` rather than `SYNO.Docker.Container`, which returns environment variables.
3. Call it with `syno_api`, or with `syno api` when there is no MCP tool. The `syno_api` tool is offered only when the server runs with `--allow-api`. If it is missing, tell the user; with their consent, the CLI does the same.
4. Call only methods whose name says they read: `list`, `get`, `info`, `load_info`, `query`, `status`, or a name starting with `get_`, `list_` or `load_`. `syno_api` refuses others. `syno api` does not, so the rule is yours to keep. Never call methods such as `set`, `create`, `delete`, `start`, `stop`, `reboot` or `shutdown`.
5. DSM does not list the methods of an API. Try `list`, `get` or `info`. Code 103 means the method does not exist; try another. Code 101 means a parameter is missing or wrong, or the API needs another version (`-v`, or `version` for the tool).
6. Responses can be large. With the CLI, pick the fields you need with `jq`.

## When the user has to act

Some states need the user, because they involve a password, a 2FA code or a decision about trust. Stop, tell the user what to run, and continue once they say it is done. Never ask for the password, set `SYNO_PASSWORD`, or pass `--trust-pin` yourself.

| syno says | Ask the user to |
|-----------|-----------------|
| `no profile configured, run syno login first`, or `no password saved for ..., run syno login` | Run `syno login` in their terminal. Find the NAS first with `syno discover --json`, which needs no login, and give them the full command: `syno login --host <dsm_url> -u <account>`. The account must be in the administrators group to read storage. If discover finds more than one NAS, ask which one. |
| The certificate is not trusted, or its public key does not match the pin | Check the certificate and run `syno login` again. A changed key means the certificate was renewed, or someone is in between: only the user can tell. |
| The keyring cannot be read | Unlock the keyring. On a machine without one, set `SYNO_PASSWORD` in their own environment. |
| A DSM error about permission | Log in with an account in the administrators group. |
