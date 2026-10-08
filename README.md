# bilgie

A bilge pump for developer machines. Caches pile up quietly in the hull of your disk until
something floods; bilgie works like the float switch that guards a boat's bilge:

- **Idle** while free space stays above the level you set.
- **Pump** as soon as it drops below: caches are trimmed, cheapest to rebuild first, until the water level is back.
- **Sweep hard** at the critical level: stale directories go too.

Interactive TUI, 27 built-in providers for macOS, Linux and Windows, auto-discovery, smart LRU cleaning and an
unattended `auto` mode that runs from the OS scheduler.

> bilgie was called `cache-buster` and lived at `github.com/Automaat/cache-buster`. The old module path is
> moved: import and install `github.com/smykla-skalski/bilgie`. The old repository URLs redirect.

![demo](./doc/demo.gif)
<!-- Generate with: brew install vhs && vhs doc/demo.tape -->

## Install

### Homebrew

```bash
brew install smykla-skalski/tap/bilgie
```

### Go Install

```bash
go install github.com/smykla-skalski/bilgie/cmd/bilgie@latest
```

### Binary Download

Download the archive for your platform from [releases](https://github.com/smykla-skalski/bilgie/releases):

| OS | Architectures | Archive |
|----|---------------|---------|
| macOS | amd64, arm64 | `.tar.gz` |
| Linux | amd64, arm64 | `.tar.gz` |
| Windows | amd64, arm64 | `.zip` |

Extract it and put `bilgie` (`bilgie.exe` on Windows) on your `PATH`.

### Upgrading from cache-buster

The first run of any `bilgie` command moves `~/.config/cache-buster` to `~/.config/bilgie` and
`~/.local/state/cache-buster` to `~/.local/state/bilgie` (config, run history and the first-run marker),
and prints one line per move. A new directory that already exists is never overwritten, and a legacy
directory that cannot be read is skipped with a warning. `install-agent` and `uninstall-agent` also remove
an agent installed under the old names (launchd label `dev.mskalski.cache-buster`, the `cache-buster`
systemd unit, crontab tag and Task Scheduler task). Run `bilgie install-agent` once after upgrading; the first
run after it is a dry-run again.

## Quick Start

```bash
# Launch interactive TUI (default)
bilgie

# Check all cache sizes
bilgie status
```

## Interactive Mode

Running `bilgie` with no arguments launches a full-screen TUI built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

Providers are scanned in parallel with live size updates. Select what to clean, confirm, and watch progress — all without leaving the terminal.

### Keyboard Shortcuts

#### Selection Screen

| Key | Action |
|-----|--------|
| `j` / `k` | Move cursor up/down |
| `space` | Toggle provider |
| `a` | Select all |
| `n` | Select none |
| `o` | Select over-limit only |
| `enter` | Confirm selection |
| `q` / `esc` | Quit |

#### Confirmation Popup

| Key | Action |
|-----|--------|
| `y` | Start cleaning |
| `s` | Switch to smart mode |
| `f` | Switch to full mode |
| `n` / `esc` | Back to selection |

### TUI Flow

```
Selection → Confirmation → Cleaning (with progress bar) → Done (summary)
```

### Flags

```bash
bilgie --dry-run     # Preview without deleting
bilgie --full        # Use full clean instead of smart (default)
```

## Providers

Providers are auto-detected — only tools installed on your system appear in the TUI and status output. Unavailable providers are dimmed.

| Provider | Default Limit | Clean Method |
|----------|---------------|--------------|
| **Go** | | |
| go-build | 10G | `go clean -cache` |
| go-mod | 5G | `go clean -modcache` |
| **JavaScript** | | |
| npm | 3G | `npm cache clean --force` |
| yarn | 2G | `yarn cache clean` |
| pnpm | 5G | `pnpm store prune` |
| **Python** | | |
| uv | 4G | file-based |
| pip | 3G | `pip cache purge` |
| **Rust** | | |
| cargo | 5G | file-based |
| **Java** | | |
| gradle | 10G | file-based |
| **Apple** | | |
| xcode-deriveddata | 20G | file-based |
| xcode-archives | 10G | file-based |
| ios-simulator | 10G | `xcrun simctl delete unavailable` |
| **Tools** | | |
| homebrew | 5G | `brew cleanup -s` |
| mise | 8G | `mise prune` |
| docker | 50G | `docker system prune -af` |
| docker-volumes (disabled by default; ignores max_age) | 50G | `docker volume prune -f` |
| jetbrains | 3G | file-based |
| **Browsers** | | |
| edge | 3G | file-based |
| vivaldi | 3G | file-based |
| **ML and test tooling** | | |
| huggingface (disabled by default; `~/.cache/huggingface/hub` only) | 20G | whole-entry (newest kept) |
| playwright (disabled by default) | 5G | whole-entry |
| **Other caches** | | |
| lima | 10G | whole-entry |
| gh | 1G | file-based |
| chrome-devtools-mcp (disabled by default; never evicts `chrome-profile-*`) | 2G | whole-entry |
| vscode-shipit | 1G | file-based |
| **Project artifacts** | | |
| project-artifacts (Rust `target`, `node_modules`, Python venvs of idle projects) | 20G | whole-directory, oldest project first |
| **Toolchains** | | |
| rustup (disabled by default; once over the limit uninstalls every toolchain except `stable`, default, active and directory-override ones) | 10G | `rustup toolchain uninstall` |

### Project artifacts

`project-artifacts` removes the build output that dominates a developer disk: Rust `target/`
directories (4 to 12 GB each, many in git worktrees) and idle `node_modules`. It scans
project roots, finds artifact directories by marker, and removes whole directories of idle
projects, oldest project first and only as far as needed. Every artifact regenerates with a
build or install.

An artifact counts only when its marker and the project file beside it both exist, so a
directory that merely shares the name is never touched:

| Kind | Artifact | Marker inside | Project file beside it | Default |
|------|----------|---------------|------------------------|---------|
| `rust` | `target/` | `CACHEDIR.TAG` whose first line is cargo's signature | `Cargo.toml` | on |
| `node` | `node_modules/` | none | `package.json` | on |
| `python` | `.venv/`, `venv/` | `pyvenv.cfg` | `pyproject.toml` or `requirements*.txt` | off |

Roots default to whichever of `~/sideprojects`, `~/kong`, `~/work`, `~/src`, `~/code` and
`~/projects` exist. Defaults are not written to the saved config, so one file works on every
machine. The search goes `max_depth` levels below each root (default 4), never follows
symlinks, never enters `.git`, `node_modules`, `target`, `.venv` or `venv`, and stops after
`scan_budget` (default 10s) or on Ctrl-C. A nested project in a monorepo is found at its own
level. A root that is home, a parent of home or the filesystem root is ignored.

A project is idle when both of these are older than `min_idle` (default 30 days): the newest
mtime among files outside the artifact directories and `.git` (a sample of the first 3000
entries, breadth first), and the git `HEAD` and reflog time. A linked worktree is judged by its
own `HEAD`. A directory-only removal never makes a project look active: the parent keeps its
mtime.

An artifact is skipped, with the reason in `--verbose` output, when:

- its project or the artifact lies in a protected location, or under a `Downloads` or
  `opencode` directory. A git worktree is not protected by being a worktree: an idle one is
  cleaned, the worktree itself is never deleted
- the project is not idle yet
- the git tree is dirty (`git status --porcelain` is non-empty, or git is missing or fails).
  Set `skip_if_dirty: false` to turn this off
- a tool of that kind runs in the project: `cargo` or `rustc` for Rust, `node`, `npm`, `npx`,
  `pnpm`, `yarn` or `bun` for Node, `python`, `pip`, `uv` or `poetry` for Python. A process
  counts when its command line names the project or its working directory is inside it. A
  process whose working directory cannot be read counts as busy when the lookup fails outright
  (always on Windows, where only image names are visible)
- a process has an open file in it (`lsof`; `skip_if_open: false` turns this off). Windows
  cannot list handles, but refuses to rename a directory that has open files, so the rename is
  the check there

Removal renames the directory aside (`.bilgie-trash-*` in the same folder) and then deletes it,
so a crash leaves either the intact artifact or a trash directory that no marker check accepts,
never a half-deleted `target`. The next run sweeps leftover trash.

`clean project-artifacts` removes every eligible artifact (full mode). `clean --smart` and
`auto` remove only until the artifacts are under `max_size`; `auto` under low-space pressure
instead stops as soon as free space is back above the floors. `--dry-run` lists what would go;
by default the five largest, with kind, project and idle days, and `--verbose` lists all of them
and every skip. `status` shows the size of all artifacts and how much of it is recoverable now
(the clean checks except the slow open-file probe).

```yaml
providers:
  project-artifacts:
    paths: [~/sideprojects, ~/work]
    max_size: 20G
    min_idle: 30d
    max_depth: 4
    scan_budget: 10s
    rust: true
    node: true
    python: false
    skip_if_dirty: true
    skip_if_open: true
```

## Commands

### status

```bash
bilgie status          # Table output
bilgie status --json   # JSON output
bilgie status --unmanaged 20  # List more unmanaged directories (0 turns the scan off)
```

After the provider table, `status` lists the largest directories (100 MiB and up) that no
provider covers. It looks at the top-level directories of `~/.local/share`, `~/Library/Caches`
(`~/.cache` off macOS), the OS temp dir and `/private/tmp`. A directory counts as covered when
an enabled provider, or a `dir-pattern` sweep, points at it, into it or at a parent of it. The
scan measures top-level totals only, runs for at most 10 seconds and stops on Ctrl-C; when it
stops early the sizes are lower bounds and `status` says so. `--json` adds an `unmanaged` object.

`status` then lists protected data that exists, with sizes, under "Needs a human (protected, never
auto-deleted)": the `protected` list plus `worktrees` directories up to two levels below home (Library, Documents, Desktop, Pictures, Movies and Music are not searched) and the other paths auto always skips, such as Xcode archives. Sizes are
measured with the same kind of bounded scan (10 second budget, Ctrl-C stops it, partial sizes are marked
"at least"). `--json` adds a `protected` object with `entries` and `incomplete`.

### clean

```bash
bilgie clean go-build npm  # Specific providers
bilgie clean --all         # All enabled
bilgie clean --dry-run     # Preview only
bilgie clean --force       # Skip confirmation
bilgie clean --smart       # LRU-based trimming
bilgie clean --dry-run --verbose  # List every entry
```

**Clean modes:**
- **Full** (default): Runs native tool commands (e.g., `go clean -cache`) or deletes files directly
- **Smart** (`--smart`): Removes files older than `max_age`, then LRU-trims to `max_size`

### Output

`clean`, `auto` and every dry-run print one block per provider: the action, the bytes, the
number of entries and the five largest entries, then the providers that were skipped with their
reason and a total. A provider that cleans through a tool (`go clean -cache`) has no entry list
and shows the command it would run instead.

```text
$ bilgie auto --dry-run
free 18 GiB of 460 GiB: tier low (dry-run, nothing is deleted)
npm: would free 12 GiB (56012 entries)
  largest:
       212 MiB  /Users/me/.npm/_cacache/content-v2/sha512/4e/9a/f3c1d0
       198 MiB  /Users/me/.npm/_cacache/content-v2/sha512/b1/07/22ae94
       171 MiB  /Users/me/.npm/_cacache/content-v2/sha512/0c/d8/7745be
       160 MiB  /Users/me/.npm/_cacache/content-v2/sha512/e9/31/a0c2d7
       155 MiB  /Users/me/.npm/_cacache/content-v2/sha512/73/5f/918b06
    ... and 56007 more (--verbose lists all)
go-build: would free 3.1 GiB
  would run: go clean -cache
skipped (2):
  docker: unavailable
  gradle: within limit
total: would free 15 GiB across 2 providers, 56012 entries; 2 skipped
done: free 18 GiB
```

`--verbose` prints every entry (`would delete: <path> (<size>)`) instead of the summary.
`--json` keeps its fields and adds a `summary` object to each provider (`action`, `entries`,
`bytes`, `top` with `path` and `size_bytes`, and `skipped_entries` for directory-pattern providers)
and to the document (`providers`, `skipped`, `errors`, `entries`, `bytes`).

### auto

```bash
bilgie auto             # Trim by free-space tier
bilgie auto --dry-run   # Preview only
bilgie auto --verbose   # List every entry
bilgie install-agent    # Run auto every auto.interval with the OS scheduler
bilgie uninstall-agent  # Unload and remove everything install-agent created
```

`auto` reads the free space of the data volume (`statfs` of `/System/Volumes/Data`)
and always trims in smart mode (files older than `max_age`, then LRU to `max_size`):

| Tier | Free space | What runs |
|------|------------|-----------|
| ok | at or above `min_free` and `min_free_pct` | only enabled providers over their limit |
| low | below `min_free` or `min_free_pct` | every enabled provider, cheapest to rebuild first; stops as soon as free space is back above the thresholds |
| critical | under 5 GiB | the stale-directory sweeps first (`dir-pattern` providers such as `sail-dirs`, even when disabled), then the low-tier trim |

Safety rules:

- `docker-volumes` never runs in `auto`, enabled or not.
- `xcode-archives` never runs in `auto`, by name or by path (any provider on an Xcode `Archives` folder is skipped): archives hold App Store dSYMs and signed builds.
- Every provider path is scanned for protected descendants (a `worktrees` or `opencode` directory, or a `Downloads` directory with macOS capitalisation) before it runs, built-in providers included. A tree over 500000 entries fails closed and the provider is skipped.
- A provider with a path inside or containing `Downloads`, `opencode` or a `worktrees` directory is skipped.
- Docker prune commands are cancelled after 10 minutes; command providers keep their `clean_timeout`.
- Providers whose tool is busy are skipped, as in `clean`.
- Two runs never overlap; a second one exits immediately.
- The first run after `install-agent` is a dry-run that deletes nothing. A marker in
  `~/.local/state/bilgie/` records it, so it survives restarts and only a completed
  dry-run in the low or critical tier, where at least one provider ran, clears it, and only when no provider failed. Running `install-agent` again arms it again.

`install-agent` picks the scheduler of the running OS. Install from a built or installed
binary, not `go run`; the job records the binary path, so run `install-agent` again after
moving it. The first run after install is a dry-run on every OS.

| OS | Scheduler | What is written | Output |
|----|-----------|-----------------|--------|
| macOS | launchd agent, loaded with `launchctl bootstrap` | `~/Library/LaunchAgents/dev.mskalski.bilgie.plist` (`StartInterval` from `auto.interval`, `RunAtLoad`, low priority, a `PATH` with Homebrew, mise, Go, Cargo and Docker) | `~/Library/Logs/bilgie/auto.log` |
| Linux | systemd user timer | `$XDG_CONFIG_HOME/systemd/user` (default `~/.config/systemd/user`) `bilgie.service` and `bilgie.timer` (first run a minute after enabling, then `auto.interval` after each run), enabled with `systemctl --user enable` | `~/.local/state/bilgie/auto.log` |
| Linux without a systemd user manager | cron | one crontab line tagged `# bilgie`; other entries are kept | `~/.local/state/bilgie/auto.log` |
| Windows | Task Scheduler task `bilgie`, created with `schtasks /Create /XML` | `~/.local/state/bilgie/bilgie-task.xml` (repeats every `auto.interval`, below-normal priority, runs only while you are logged on) | none |

Cron fires on fixed minute and hour marks, so the cron fallback only accepts an `auto.interval`
that divides an hour or a day evenly (1m, 2m, 5m, 10m, 15m, 20m, 30m, 1h, 2h, 3h, 4h, 6h, 8h, 12h,
24h) and refuses any other. Installing the systemd timer removes an earlier cron entry and the cron fallback removes earlier systemd units.
The systemd user timer runs only while your user manager is up; on a headless box run
`loginctl enable-linger $USER` so it survives logout, otherwise install falls back to cron.
`uninstall-agent` removes the job, its definition
files and the first-run marker, and succeeds when nothing is installed; on Linux it clears both
the systemd units and the crontab line.

Every run appends one JSON line to `~/.local/state/bilgie/runs.jsonl`: time, tier, free space
before and after, bytes freed per provider, and skipped providers with their reasons. The log rotates
to `runs.jsonl.1` at 8 MiB.

```bash
bilgie history           # Last 10 runs
bilgie history -n 50     # More runs (0 shows all)
bilgie history --json    # Full records, including per-provider detail
bilgie history --providers -n 20   # Bytes freed per provider over the last 20 runs
```

```text
$ bilgie history --providers -n 20
Bytes freed per provider over the last 20 run(s); dry-runs count as WOULD FREE
PROVIDER  RUNS  FREED     WOULD FREE  SKIPPED  ERRORS
npm       18    41.2 GiB  12.0 GiB    11       0
go-build  12    9.8 GiB   3.1 GiB     6        0
docker    7     0 B       0 B         2        5
```

`history` skips unreadable lines and reports how many. When a real run (not a dry-run) ends with
free space still under `min_free` or `min_free_pct`, `auto` shows one desktop notification (`osascript` on macOS, `notify-send` on Linux, a PowerShell toast on
Windows); a run that recovered enough space stays quiet. A notifier that is not installed is logged as a skipped
notification. A failed notification or log write is reported but does not fail the run, and a notifier is
cancelled after 10 seconds so it cannot hold the run lock.

### doctor

```bash
bilgie doctor
```

Checks, without changing anything, that the agent works: it is installed and loaded in the
scheduler of this OS (`launchctl print`, `systemctl --user is-active` or the crontab, `schtasks`),
the last run (time, tier, bytes freed, errors from `runs.jsonl`) is recent for `auto.interval`,
free space is above the floors, the 7-day free-space trend from the run history, config problems
(disabled providers, providers skipped on each of the last runs for a reason other than
"within limit", providers on protected paths) and whether the notifier program exists.
It exits non-zero when a finding needs attention; each one carries a "what to do" line.

```text
$ bilgie doctor
[ok  ] agent: installed and loaded (launchd)
[FAIL] last run: 9h ago (tier low, freed 4.1 GiB, 1 provider error(s)); failed: docker
       what to do: see /Users/me/Library/Logs/bilgie/auto.log; run: bilgie auto --dry-run --verbose to see each error
[FAIL] schedule: last run was 9h ago but the interval is 30m0s: the agent is not running
       what to do: see /Users/me/Library/Logs/bilgie/auto.log; run: bilgie install-agent to reinstall it
[ok  ] freed: 38 GiB freed by 21 deleting run(s) in the last 7 days
[warn] free space: 24 GiB free of 460 GiB (5%), tier low
       what to do: run: bilgie auto; run: bilgie status for large unmanaged directories
[warn] trend: free space -41 GiB over 6d (65 GiB -> 24 GiB, 80 run(s)); min_free is reached in about 8 day(s) at this rate
       what to do: find what grows: bilgie status shows large unmanaged directories
[note] config: 3 provider(s) disabled: docker-volumes, sail-dirs, xcode-archives
[warn] config: gradle was skipped on each of the last 5 runs: unavailable
       what to do: install the tool gradle needs, or set providers.gradle.enabled: false
[ok  ] notifier: osascript is available

5 finding(s) need attention
```

### config

```bash
bilgie config init   # Create default config
bilgie config show   # Display current config
bilgie config edit   # Open in $EDITOR
```

## Configuration

Location: `~/.config/bilgie/config.yaml`

Generate defaults with `bilgie config init`.

A saved config keeps the values it was created with. When a default changes
later, the saved value wins: configs from older versions keep `enabled: true`
for `huggingface` and `playwright`, which are now disabled by default.
`bilgie config show` ends with a note for every provider whose saved
`enabled` differs from the current default. Edit the file to change it.

```yaml
version: "1"
providers:
  go-build:
    enabled: true
    paths:
      - ~/Library/Caches/go-build
    max_size: 10G
    max_age: 30d
    clean_cmd: go clean -cache
```

| Field | Description |
|-------|-------------|
| `enabled` | Include in status/clean operations |
| `paths` | Directories to scan (supports `~` expansion) |
| `max_size` | Size limit (e.g., `10G`, `500M`) |
| `max_age` | File age threshold for smart clean (e.g., `30d`) |
| `clean_cmd` | Command for full clean (empty = file-based deletion) |
| `clean_timeout` | Max runtime of `clean_cmd` before it is cancelled and reported (default `2m`) |
| `type` | `dir-pattern` removes whole stale directories matching a glob in `paths`; `project-artifacts` removes build artifacts of idle projects found below the roots in `paths` |
| `min_idle` | `dir-pattern`: minimum idle time, from the newest mtime in the tree (default `2h`); `project-artifacts`: project idle time (default `30d`) |
| `skip_if_open` | `dir-pattern`, `project-artifacts`: skip directories with open files via `lsof +D` (default `true`) |
| `skip_if_git_worktree` | `dir-pattern`: skip directories containing a `.git` entry (default `true`) |
| `max_depth` | `project-artifacts`: directory levels searched below each root (default `4`, at most 16) |
| `scan_budget` | `project-artifacts`: time allowed for finding projects per pass (default `10s`) |
| `rust`, `node`, `python` | `project-artifacts`: per-kind switches (default `true`, `true`, `false`) |
| `skip_if_dirty` | `project-artifacts`: skip projects with uncommitted changes (default `true`) |
| `skip_prefixes` | Whole-entry providers never evict entries whose name starts with one of these prefixes; user values add to the built-in ones |

The optional top-level `auto` block configures `bilgie auto`:

```yaml
auto:
  interval: 30m      # scheduler interval, minimum 1m
  min_free: 30G      # below this, trim every enabled provider
  min_free_pct: 15   # or below this percentage of the volume (0 disables)
```

The optional top-level `protected` list names paths that `auto` never deletes from and that `status`
reports under "needs a human":

```yaml
protected:
  - ~/Documents/important
```

Entries must be literal paths, absolute or starting with `~/`: globs, `.`/`..` elements, home, its parents and top-level directories are rejected. They are added to the built-in list (`~/Downloads`,
`~/.local/share/opencode`, `/var/lib/docker/volumes`); removing a built-in entry from the file has no
effect. `auto` also skips any path that holds a `.git` entry (a git checkout or worktree), anything under a
`worktrees` directory, and never prunes Docker volumes (Docker Desktop keeps them inside its VM image).
`project-artifacts` is the one provider that works inside git checkouts and `worktrees` directories by design; it enforces the protected list, `Downloads` and `opencode` itself and cleans only the artifact directories.

### Busy tools

`clean` skips a provider while its tool is active, so a clean never breaks a
running build or waits on a held lock. The skip reason shows in `clean` output
and in `clean --json` (`"status": "skipped"`, `"reason"`). A skip is not a
failure. If the check itself fails, the provider is skipped too.

| Provider | Skipped while |
|----------|---------------|
| `go-build`, `go-mod` | a `go` process runs |
| `cargo` | a `cargo` or `rustc` process runs |
| `homebrew` | a `brew` process runs |
| `uv` | `<path>/.lock` is flock-held, or a `uv` process runs |

Busy detection errs toward skipping: any process whose command line contains
the tool name counts, including wrappers such as `sudo` or `sh -c`. The
bilgie process and its wrapper ancestors are not counted, but an
ancestor that is the tool itself, such as `cargo run -- clean cargo`, is. A hung
`clean_cmd` is killed with its whole process group, so it must not need a
terminal.

`clean --json` needs `--force` or `--dry-run` because it cannot prompt.

`dir-pattern` paths must contain a glob character. The provider ignores
`max_age` and removes every stale match whole regardless of `max_size`, which is
only used for `status`. `lsof` run as a non-root user cannot see other users'
processes.

The built-in `sail-dirs` provider (`sail*` in the OS temp dir and in the fixed system
temp dir, `/tmp` or `/private/tmp` on macOS, listed once when both resolve to the same directory)
uses `dir-pattern` and is disabled by default. The paths follow the machine and are not saved to
the config file. Enable it with `enabled: true` after reviewing
`bilgie clean sail-dirs --dry-run`, which lists each directory with its size,
idle time and the reason it would be skipped.

## Building from Source

```bash
git clone https://github.com/smykla-skalski/bilgie
cd bilgie
mise run build
```

### Mise Tasks

| Task | Description |
|------|-------------|
| `mise run build` | Build binary |
| `mise run install` | Install to $GOPATH/bin |
| `mise run test` | Run tests with race detection |
| `mise run lint` | Run golangci-lint |
| `mise run cover` | Generate coverage report |
| `mise run clean` | Remove build artifacts |
| `mise run all` | lint + test + build |

## License

MIT
