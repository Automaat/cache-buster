# cache-buster

Developer cache manager for macOS. Interactive TUI, 27 built-in providers, auto-discovery, smart LRU cleaning.

![demo](./doc/demo.gif)
<!-- Generate with: brew install vhs && vhs doc/demo.tape -->

## Install

### Homebrew

```bash
brew install Automaat/tap/cache-buster
```

### Go Install

```bash
go install github.com/Automaat/cache-buster/cmd/cache-buster@latest
```

### Binary Download

Download the archive for your platform from [releases](https://github.com/Automaat/cache-buster/releases):

| OS | Architectures | Archive |
|----|---------------|---------|
| macOS | amd64, arm64 | `.tar.gz` |
| Linux | amd64, arm64 | `.tar.gz` |
| Windows | amd64, arm64 | `.zip` |

Extract it and put `cache-buster` (`cache-buster.exe` on Windows) on your `PATH`.

## Quick Start

```bash
# Launch interactive TUI (default)
cache-buster

# Check all cache sizes
cache-buster status
```

## Interactive Mode

Running `cache-buster` with no arguments launches a full-screen TUI built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

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
cache-buster --dry-run     # Preview without deleting
cache-buster --full        # Use full clean instead of smart (default)
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
| **Toolchains** | | |
| rustup (disabled by default; once over the limit uninstalls every toolchain except `stable`, default, active and directory-override ones) | 10G | `rustup toolchain uninstall` |

## Commands

### status

```bash
cache-buster status          # Table output
cache-buster status --json   # JSON output
cache-buster status --unmanaged 20  # List more unmanaged directories (0 turns the scan off)
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
cache-buster clean go-build npm  # Specific providers
cache-buster clean --all         # All enabled
cache-buster clean --dry-run     # Preview only
cache-buster clean --force       # Skip confirmation
cache-buster clean --smart       # LRU-based trimming
```

**Clean modes:**
- **Full** (default): Runs native tool commands (e.g., `go clean -cache`) or deletes files directly
- **Smart** (`--smart`): Removes files older than `max_age`, then LRU-trims to `max_size`

### auto

```bash
cache-buster auto             # Trim by free-space tier
cache-buster auto --dry-run   # Preview only
cache-buster install-agent    # Run auto from launchd every auto.interval
cache-buster uninstall-agent  # Unload and remove the agent
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
  `~/.local/state/cache-buster/` records it, so it survives restarts and only a completed
  dry-run in the low or critical tier, where at least one provider ran, clears it, and only when no provider failed. Running `install-agent` again arms it again.

`install-agent` writes `~/Library/LaunchAgents/dev.mskalski.cache-buster.plist`
(`StartInterval` from `auto.interval`, `RunAtLoad`, low priority, a `PATH` with Homebrew,
mise, Go, Cargo and Docker) and loads it with `launchctl bootstrap`. Output goes to
`~/Library/Logs/cache-buster/auto.log`. Install from a built or installed binary, not
`go run`. The plist records the binary path, so run `install-agent` again after moving it.

Every run appends one JSON line to `~/.local/state/cache-buster/runs.jsonl`: time, tier, free space
before and after, bytes freed per provider, and skipped providers with their reasons. The log rotates
to `runs.jsonl.1` at 8 MiB.

```bash
cache-buster history           # Last 10 runs
cache-buster history -n 50     # More runs (0 shows all)
cache-buster history --json    # Full records, including per-provider detail
```

`history` skips unreadable lines and reports how many. When a real run (not a dry-run) ends with
free space still under `min_free` or `min_free_pct`, `auto` shows one macOS notification; a run that
recovered enough space stays quiet. A failed notification or log write is reported but does not fail the run.

### config

```bash
cache-buster config init   # Create default config
cache-buster config show   # Display current config
cache-buster config edit   # Open in $EDITOR
```

## Configuration

Location: `~/.config/cache-buster/config.yaml`

Generate defaults with `cache-buster config init`.

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
| `type` | `dir-pattern` removes whole stale directories matching a glob in `paths` |
| `min_idle` | `dir-pattern`: minimum idle time, from the newest mtime in the tree (default `2h`) |
| `skip_if_open` | `dir-pattern`: skip directories with open files via `lsof +D` (default `true`) |
| `skip_if_git_worktree` | `dir-pattern`: skip directories containing a `.git` entry (default `true`) |
| `skip_prefixes` | Whole-entry providers never evict entries whose name starts with one of these prefixes; user values add to the built-in ones |

The optional top-level `auto` block configures `cache-buster auto`:

```yaml
auto:
  interval: 30m      # launchd StartInterval, minimum 1m
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
the tool name counts, including wrappers such as `sudo` or `sh -c`. A hung
`clean_cmd` is killed with its whole process group, so it must not need a
terminal.

`clean --json` needs `--force` or `--dry-run` because it cannot prompt.

`dir-pattern` paths must contain a glob character. The provider ignores
`max_age` and removes every stale match whole regardless of `max_size`, which is
only used for `status`. `lsof` run as a non-root user cannot see other users'
processes.

The built-in `sail-dirs` provider (`/private/tmp/sail*`) uses `dir-pattern` and is
disabled by default. Enable it with `enabled: true` after reviewing
`cache-buster clean sail-dirs --dry-run`, which lists each directory with its size,
idle time and the reason it would be skipped.

## Building from Source

```bash
git clone https://github.com/Automaat/cache-buster
cd cache-buster
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
