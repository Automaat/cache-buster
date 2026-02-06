# cache-buster

Developer cache manager for macOS. Interactive TUI, 16 built-in providers, auto-discovery, smart LRU cleaning.

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

Download from [releases](https://github.com/Automaat/cache-buster/releases).

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
| homebrew | 5G | `brew cleanup` |
| mise | 8G | `mise prune` |
| docker | 50G | `docker system prune -af` |
| jetbrains | 3G | file-based |

## Commands

### status

```bash
cache-buster status          # Table output
cache-buster status --json   # JSON output
```

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
