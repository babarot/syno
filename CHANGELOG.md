# Changelog

## [v0.5.0](https://github.com/babarot/syno/compare/v0.4.0...v0.5.0) - 2026-10-10
### New Features
- Add syno dashboard, a web page of the NAS on localhost by @babarot in https://github.com/babarot/syno/pull/27
- Show a running data scrubbing on the dashboard storage card by @babarot in https://github.com/babarot/syno/pull/32
### Bug fixes
- Treat running data scrubbing as maintenance, not a problem by @babarot in https://github.com/babarot/syno/pull/31
### Improvements
- Honor NO_COLOR in the output of syno doctor by @babarot in https://github.com/babarot/syno/pull/33

## [v0.4.0](https://github.com/babarot/syno/compare/0.3.0...v0.4.0) - 2026-10-10
### New Features
- Show the life left and power-on hours of disks in status by @babarot in https://github.com/babarot/syno/pull/25
- Show the drive bays of the NAS in status by @babarot in https://github.com/babarot/syno/pull/28
- Show scrubbing, I/O and RAID names of storage in status by @babarot in https://github.com/babarot/syno/pull/29
### Improvements
- Keep the DSM session open in syno mcp by @babarot in https://github.com/babarot/syno/pull/26
### Others
- Set the plugin version to the released 0.3.0 by @babarot in https://github.com/babarot/syno/pull/22
- Sync shared files from github-config by @babarot in https://github.com/babarot/syno/pull/24

## [0.3.0](https://github.com/babarot/syno/compare/0.2.0...0.3.0) - 2026-10-09
### New Features
- Add syno share list to see which shared folders use the space by @babarot in https://github.com/babarot/syno/pull/14
- Add syno share list --recycle to show what the recycle bins hold by @babarot in https://github.com/babarot/syno/pull/15
- Add syno wake to start the NAS with Wake-on-LAN by @babarot in https://github.com/babarot/syno/pull/16
- Add syno container start, stop and restart by @babarot in https://github.com/babarot/syno/pull/18
- Add syno container list --usage for CPU and memory by @babarot in https://github.com/babarot/syno/pull/19

## [0.2.0](https://github.com/babarot/syno/compare/0.1.0...0.2.0) - 2026-10-09
### New Features
- Add syno container list and a containers check to doctor by @babarot in https://github.com/babarot/syno/pull/2
- Add syno package list and a package-update check to doctor by @babarot in https://github.com/babarot/syno/pull/5
- Add syno mcp to answer questions about the NAS by @babarot in https://github.com/babarot/syno/pull/7
- Let syno mcp find DSM APIs and start without a profile by @babarot in https://github.com/babarot/syno/pull/8
- Add an agent skill for answering questions about the NAS by @babarot in https://github.com/babarot/syno/pull/9
- Ship syno as a Claude Code plugin by @babarot in https://github.com/babarot/syno/pull/10
### Improvements
- Reuse the DSM session across commands by @babarot in https://github.com/babarot/syno/pull/6
### Refactorings
- Move syno containers to syno container list by @babarot in https://github.com/babarot/syno/pull/4

## [0.1.0](https://github.com/babarot/syno/commits/0.1.0) - 2026-10-09
