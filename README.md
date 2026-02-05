# cache-buster

Developer cache manager with configurable size limits.

Monitors and cleans caches for Go, npm, Docker, Homebrew, and other dev tools.

## Installation

### Homebrew (macOS)

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
# Check cache sizes
cache-buster status

# Interactive mode (default when no command)
cache-buster

# Clean specific providers
cache-buster clean go-build npm

# Clean all enabled providers
cache-buster clean --all

# Preview what would be cleaned
cache-buster clean --dry-run --all

# Smart clean (LRU trimming to stay under limits)
cache-buster clean --smart --all
```

## Commands

### status

Show cache sizes for all providers.

```bash
cache-buster status          # table output
cache-buster status --json   # JSON output
```

### clean

Clean caches to free disk space.

```bash
cache-buster clean [providers...]  # specific providers
cache-buster clean --all           # all enabled
cache-buster clean --dry-run       # preview only
cache-buster clean --force         # skip confirmation
cache-buster clean --quiet         # minimal output
cache-buster clean --smart         # LRU-based trimming
```

**Clean modes:**
- **Full** (default): Uses native tool commands (e.g., `go clean -cache`)
- **Smart** (`--smart`): Removes files older than `max_age`, then LRU-trims to `max_size`

### config

Manage configuration.

```bash
cache-buster config show   # display current config
cache-buster config init   # create default config
cache-buster config edit   # open in $EDITOR
```

### interactive

TUI mode with provider selection and progress display.

```bash
cache-buster interactive
cache-buster              # also launches interactive mode
```

## Configuration

Location: `~/.config/cache-buster/config.yaml`

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
  npm:
    enabled: true
    paths:
      - ~/.npm
    max_size: 3G
    max_age: 30d
    clean_cmd: npm cache clean --force
```

### Provider Options

| Field | Description |
|-------|-------------|
| `enabled` | Include in status/clean operations |
| `paths` | Directories to scan (supports `~` expansion) |
| `max_size` | Size limit (e.g., `10G`, `500M`) |
| `max_age` | File age threshold for smart clean (e.g., `30d`) |
| `clean_cmd` | Command for full clean (empty = file-based) |

## Default Providers

| Provider | Path | Default Limit | Clean Method |
|----------|------|---------------|--------------|
| go-build | ~/Library/Caches/go-build | 10G | `go clean -cache` |
| go-mod | ~/go/pkg/mod | 5G | `go clean -modcache` |
| npm | ~/.npm | 3G | `npm cache clean --force` |
| yarn | ~/Library/Caches/Yarn | 2G | `yarn cache clean` |
| homebrew | ~/Library/Caches/Homebrew | 5G | `brew cleanup` |
| mise | ~/.local/share/mise | 8G | `mise prune` |
| uv | ~/.cache/uv | 4G | file-based |
| jetbrains | ~/Library/Caches/JetBrains | 3G | file-based |
| docker | ~/Library/Containers/com.docker.docker | 50G | `docker system prune -af` |

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
