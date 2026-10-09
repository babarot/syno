<img src="docs/logo.svg" alt="syno" width="180">

[![Test](https://github.com/babarot/syno/actions/workflows/test.yaml/badge.svg)](https://github.com/babarot/syno/actions/workflows/test.yaml)

Find the Synology NAS on your network and check on it from the terminal.

syno finds a NAS without logging in, the way [Synology's Web Assistant](https://finds.synology.com/) does, and then reads its state through the DSM Web API: the system, storage pools, volumes and disks, and a health check you can run from cron.

```console
$ syno discover
NAME   IP             MAC                 DSM                         SOURCE
nas    192.168.1.10   90:09:d0:12:34:56   https://192.168.1.10:5001   mdns,arp

$ syno doctor
✓ pools               1 pool healthy
! volumes             /volume1 88% used
✓ disks               4 disks healthy
✓ disk-temperature    hottest disk is 38°C
! scrubbing           Pool 1 has no scrubbing schedule
✓ system-temperature  52°C, no warning from DSM
✓ reboot              no reboot pending
✓ dsm-update          DSM is up to date
✓ security-advisor    no findings, last scan 3 days ago
✓ certificates        2 certificates valid, next expiry in 64 days

8 ok, 2 warn
```

syno is not affiliated with or endorsed by Synology Inc. Synology and DSM are trademarks of Synology Inc.

## Install

Archives for macOS and Linux (arm64 and x86_64) are on the [releases page](https://github.com/babarot/syno/releases).

With Go:

```bash
go install github.com/babarot/syno@latest
```

With Nix, from [babarot/nur-packages](https://github.com/babarot/nur-packages):

```bash
nix profile install github:babarot/nur-packages#syno
```

## Quick start

```bash
syno discover   # find the NAS on the local network
syno login      # log in once and save it as a profile
syno status     # system, pools, volumes and disks
syno doctor     # health checks, with an exit status for monitoring
```

Storage information needs an account in the administrators group.

## Commands

### discover

Finds Synology NAS on the local network without logging in. Candidates come from mDNS, from the ARP table (Synology's MAC address prefixes) and, with `--scan`, from a sweep of the local /24 networks for the DSM ports 5000 and 5001. Each candidate is then checked for a DSM Web API.

```bash
syno discover
syno discover --scan --json
```

### login

Logs in to DSM, then saves the NAS and the account as a profile and the password in the OS keyring. Without `--host`, the NAS is found with `discover`. Before the password is sent, the server certificate is checked; see [Security](#security).

```bash
syno login
syno login --host https://192.168.1.10:5001 -u admin --profile home
```

When the account uses 2FA, syno asks for a code once and saves the device token DSM issues, so later commands do not ask again.

Commands share one DSM session per profile instead of logging in each time, which saves about a third of a second per command. When DSM no longer accepts the session, the next command logs in again with the saved password. `syno logout` ends the session; the password stays saved.

### profile

```bash
syno profile list           # the current profile is marked with *
syno profile use office
syno profile remove office  # also removes its password from the keyring
```

`remove` keeps the keyring items when another profile uses the same URL and account.

Every command that logs in uses the profile given by `--profile` (`-p`), then `SYNO_PROFILE`, then the current profile.

### status

Shows the system, CPU and memory usage, storage pools, volumes and disks. `--json` prints the same as JSON with sizes in bytes, and `--raw` prints the API responses as they are.

```console
$ syno status
SYSTEM
  Host          https://192.168.1.10:5001
  Model         DS923+
  Serial        0000ABCDE0000
  DSM           DSM 7.2.2-72806 Update 4
  Uptime        12d 3h 4m
  Temperature   52°C
  CPU           4%
  Memory        21% of 3.8 GB

POOL     STATUS   TYPE                      DISKS   USED      TOTAL     USE%
Pool 1   normal   shr_with_1_disk_protect   4       19.1 TB   21.8 TB   88%

VOLUME     STATUS   FS      POOL     USED      TOTAL     USE%
/volume1   normal   btrfs   Pool 1   18.6 TB   21.1 TB   88%

DISK      MODEL                        SIZE     STATUS   SMART    TEMP   POOL
Drive 1   WDC WD80EFZZ-68BTXN0         7.3 TB   normal   normal   37°C   Pool 1
...
```

### doctor

Runs health checks and reports each as ok, warn, fail, unknown or skip. `syno doctor --list` shows every check:

| Check | What it looks at |
|-------|------------------|
| `pools` | Failed or missing disks, and DSM's status of each storage pool |
| `volumes` | Usage against the thresholds, and DSM's status of each volume |
| `disks` | Disk status, SMART, the remaining life DSM estimates and uncorrectable sectors |
| `disk-temperature` | Disk temperatures against the thresholds |
| `scrubbing` | Whether data scrubbing is scheduled and when it last ran |
| `system-temperature` | DSM's own temperature warning |
| `reboot` | A reboot pending to finish an update |
| `dsm-update` | A newer DSM (the NAS asks Synology's update server) |
| `package-update` | Security updates of the installed packages in Package Center. Other updates are only counted |
| `security-advisor` | Findings of the Security Advisor and the age of its last scan |
| `certificates` | Broken, expired and expiring certificates |
| `containers` | Running containers that are unhealthy or keep restarting. Stopped ones are only counted. Skipped without Container Manager |

```bash
syno doctor --skip dsm-update   # turn checks off
syno doctor --only volumes,disks
syno doctor --json
```

The exit status follows the Nagios plugin convention, so `syno doctor` can be used from cron or a monitoring system as it is:

| Exit status | Meaning |
|-------------|---------|
| 0 | Every check is ok or skipped |
| 1 | At least one check warns |
| 2 | At least one check fails |
| 3 | A check could not run, the NAS could not be reached, or the settings are invalid |

### container

Lists the containers of Container Manager with their state, health check and Docker Compose project. Stopped containers are listed too, unlike `docker ps`, so that a container that went down after a deploy is not missed. Environment variables are never read or printed.

```console
$ syno container list
NAME          STATE     HEALTH      PROJECT   IMAGE                   STATUS
web-app-1     running   healthy     web       example/app:latest      Up 2 days
web-db-1      exited    unhealthy   web       postgres:16             Exited (1) 3 hours ago
proxy         running   -           -         nginx:latest            Up 5 days
```

```bash
syno container list --running
syno container list --project web --json
```

### package

Lists the installed packages with the latest version in Package Center. LATEST shows the newer version when there is one, marked `(security)` for a security update, `-` when the package is up to date, and `?` when Package Center does not know the package, as with third-party ones. Nothing is updated.

```console
$ syno package list
ID            NAME            VERSION        LATEST                     STATUS
FileStation   File Station    1.4.2-1575     -                          running
git           Git             2.53.0-40      ?                          running
MariaDB10     MariaDB 10      10.11.6-1369   10.11.11-1551 (security)   stop
WebStation    Web Station     4.2.3-0522     4.3.1-0530                 running
```

```bash
syno package list --outdated
syno package list --json
```

### api

Calls any DSM Web API with the saved account and prints the `data` field of the response as JSON. DSM has hundreds of APIs and few are documented, so this is the way to look around. The path and the latest version of each API come from `SYNO.API.Info`.

```bash
syno api --list storage                   # the APIs the NAS provides (no login needed)
syno api SYNO.Core.System info
syno api SYNO.Core.Share list -f additional='["share_quota"]'
```

For APIs that take JSON parameters, `-f` values that are not valid JSON are sent as JSON strings, so `-f name=homes` works as well as `-f 'name="homes"'`. A value that is valid JSON is sent as it is: quote a number the API expects as a string, as in `-f 'id="123"'`.

### mcp

Runs an MCP server over stdio, so that an AI assistant such as Claude Code can answer questions like "how much space is left?" or "is any container down?". The tools only read, and give the same JSON as the commands:

| Tool | Same as |
|------|---------|
| `syno_status` | `syno status --json` |
| `syno_doctor` | `syno doctor --json`, with `skip` and `only` |
| `syno_containers` | `syno container list --json`, with `running` and `project` |
| `syno_packages` | `syno package list --json`, with `outdated` |
| `syno_api_list` | `syno api --list --json`, with `filter`, under `apis` next to `host` |
| `syno_api` | `syno api`, under `data` next to `host`; only with `--allow-api` |

`syno_api` lets the assistant answer what the other tools do not cover, such as shared folders or backups, and is off unless the server is started with `--allow-api`. Even DSM methods that only read can return secrets, such as the environment variables of containers or the passwords in notification, DDNS and backup settings, and what a tool returns is sent to the provider of the AI. Turn it on only if you accept that. `syno_api` then calls only the methods whose name says they read (`list`, `get`, `info`, `load_info`, `query`, `status`, and ones starting with `get_`, `list_` or `load_`), which lowers the chance of changing the NAS without ruling it out.

Each tool call uses the profile given by `--profile` or `SYNO_PROFILE`, or else the current profile at the time of the call, and every answer has the `host` it came from. The server starts without a profile too; its tools then answer that `syno login` is needed, and work once you have logged in. Register one server per NAS:

```bash
claude mcp add syno -- syno mcp
claude mcp add syno-office -- syno mcp --profile office
claude mcp add syno -- syno mcp --allow-api   # also offer syno_api
```

## Agent skill

[`skills/syno`](skills/syno/SKILL.md) is an [Agent Skill](https://agentskills.io) that teaches an AI agent to answer questions about the NAS with syno: which command to start from, how to look for a DSM API when no command covers the question, and when to ask you to log in. It works with or without `syno mcp`. Install syno first, then the skill:

```bash
gh skill install babarot/syno syno --agent claude-code --scope user
npx skills add babarot/syno -g
```

In Claude Code, the plugin installs the skill and registers `syno mcp` (without `--allow-api`) in one step:

```
/plugin install syno --marketplace babarot/syno
```

The release archives carry the skill too, and the Nix package installs it in `share/skills/syno`, so that Home Manager can link it with the binary of the same release:

```nix
home.file.".claude/skills/syno".source = "${syno}/share/skills/syno";
```

## Configuration

syno keeps two files in `~/.config/syno` (or `$XDG_CONFIG_HOME/syno`):

| File | Written by | Holds |
|------|------------|-------|
| `profiles.yaml` | syno | Profiles: the DSM URL, the account and the TLS pin |
| `config.yaml` | You | Settings; syno only reads this file, so your comments stay |

`config.yaml` holds the doctor settings. Leave out what you do not want to change:

```yaml
doctor:
  skip: [dsm-update]
  thresholds:
    volume_usage: {warn: 85, fail: 95}       # percent
    disk_temperature: {warn: 50, fail: 60}   # Celsius
    scrub_age_days: 90
    security_scan_age_days: 30
    cert_expiry_days: 30
    renewable_cert_expiry_days: 14           # certificates DSM renews, such as Let's Encrypt
```

Unknown keys are errors, so a typo does not go unnoticed. `--skip` adds to `doctor.skip`, and `--only` ignores it.

## Security

- Passwords, 2FA device tokens and the shared DSM session are kept in the OS keyring: the Keychain on macOS and the Secret Service on Linux. On a machine without a keyring, set `SYNO_PASSWORD`; each command then logs in and logs out as it ends.
- The server certificate is verified. A NAS often has a certificate the system does not trust, such as DSM's self-signed one, or one for a domain name while you connect by IP address. In that case `syno login` shows the certificate and asks whether to pin its public key, as SSH does with host keys. Later commands then accept only that key, and fail if it changes. `--trust-pin` accepts a known pin without asking.
- To avoid pinning, connect with a host name that has a valid certificate, for example `syno login --host https://nas.example.com:5001`.
- `syno login` refuses plain HTTP unless `--allow-http` is given, since the password would be sent in clear text.
- `discover` and `api --list` do not verify certificates. They only read `SYNO.API.Info`, which needs no login, and send no credentials.

## Compatibility

syno has been tested with a DS923+ on DSM 7.2.2. Most of the DSM Web APIs it uses are not documented by Synology, and their responses may change between DSM versions. `syno status --raw` and `syno api` help to see what a NAS actually returns.

## Development

```bash
make build   # ./syno
make test    # go vet and go test
make lint    # golangci-lint
```

## License

[MIT](LICENSE)
