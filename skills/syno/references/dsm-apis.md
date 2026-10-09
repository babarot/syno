# DSM APIs for questions the commands do not cover

These APIs answered with the methods below on a DS923+ with DSM 7.2.2-72806. DSM does not document most of them, and a NAS may differ by model, DSM version and installed packages: when one fails, look for another with `syno api --list <keyword>`.

Ask the user before calling any of them (see SKILL.md), and pick only the fields the question needs.

| Question | API | Method | Notes |
|----------|-----|--------|-------|
| Quotas of shared folders | `SYNO.Core.Share` | `list` | Use `syno share list` for usage and flags. Add `additional=["share_quota"]` for `quota_value`, whose unit is not confirmed. |
| Snapshots of a shared folder | `SYNO.Core.Share.Snapshot` | `list` | Needs `name=<share>`. |
| Users | `SYNO.Core.User` | `list` | |
| Groups | `SYNO.Core.Group` | `list` | |
| Hyper Backup tasks | `SYNO.Backup.Task` | `list` | Needs `-v 1`; the latest version answers code 103. |
| Scheduled tasks | `SYNO.Core.TaskScheduler` | `list` | |
| System log | `SYNO.Core.SyslogClient.Log` | `list` | Has counts of info, warning and error entries. |
| Network settings: gateway, DNS | `SYNO.Core.Network` | `get` | |
| Network interfaces | `SYNO.Core.Network.Interface` | `list` | |
| UPS | `SYNO.Core.ExternalDevice.UPS` | `get` | Charge and runtime when a UPS is connected. |
| Load averages and per-device usage | `SYNO.Core.System.Utilization` | `get` | More detail than `syno status`. |
| Fan mode | `SYNO.Core.Hardware.FanSpeed` | `get` | |
| Services and whether they run | `SYNO.Core.Service` | `get` | |
| SMB settings | `SYNO.Core.FileServ.SMB` | `get` | |
| Auto block of failed logins | `SYNO.Core.Security.AutoBlock` | `get` | |

## Do not call

These return secrets, or exist to hold them. Answer without them, or tell the user where to look in DSM.

- `SYNO.Docker.Container`: returns the environment variables of containers. Use `syno container list`.
- Notification settings (`SYNO.Core.Notification.*`): mail and SMS credentials.
- DDNS settings (`SYNO.Core.DDNS.*`): provider credentials.
- Backup destinations (`SYNO.Backup.Repository*`, `SYNO.Backup.Storage.*`): credentials of the destinations.
