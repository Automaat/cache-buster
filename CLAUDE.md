# cache-buster

macOS developer cache manager with configurable size limits. Monitors and cleans caches for Go, npm, Docker, Homebrew, and other dev tools.

## Project Structure

```
cmd/cache-buster/     - CLI entrypoint
internal/
  cache/              - Cache size scanning
  cli/                - Cobra command implementations (status, clean, config)
  config/             - Config loading, defaults, validation
  provider/           - Provider interface + implementations (command, file, docker)
pkg/size/             - Human-readable size parsing/formatting
```

## Tech Stack

**Language:** Go 1.25
**CLI Framework:** Cobra + Viper
**Terminal UI:** Lipgloss (tables, styling)
**Testing:** testify (assert/require)
**Linting:** golangci-lint (errcheck, govet, staticcheck, gosec, gocritic)
**Dependency Mgmt:** mise

## Development Workflow

### Adding New Command

1. Create `internal/cli/{command}.go` with Cobra command
2. Add flags in `init()` function
3. Create `run{Command}WithLoader()` for testability
4. Register in `cmd/cache-buster/main.go`
5. Add tests in `internal/cli/{command}_test.go`
6. Test: `go run ./cmd/cache-buster {command} --help`

### Adding New Provider

1. Add default config in `internal/config/defaults.go`
2. For command-based providers: set `clean_cmd` field
3. For file-based providers (no CLI): add to `fileBasedProviders` in `registry.go`
4. Test size calculation and clean operation
5. Verify `Available()` returns correct result

Command-based provider pattern (most common):
```go
// In defaults.go
"tool-name": {
    Enabled:  true,
    Paths:    []string{"~/Library/Caches/tool-name"},
    MaxSize:  "5G",
    CleanCmd: "tool-name clean-cache",
},
```

### Running Tests

```bash
go test ./...                       # all tests
go test ./internal/cli/...          # CLI tests only
go test -run TestScanProvider ./... # specific test
go test -v -race ./...              # with race detection
```

## Architecture

### Provider Types

| Type | Clean Method | Example |
|------|--------------|---------|
| Command | Executes `clean_cmd` | go, npm, homebrew |
| File | Deletes files directly | uv, jetbrains |
| Docker | Special docker prune | docker (checks daemon) |

### Provider Interface

```go
type Provider interface {
    Name() string
    Paths() []string
    CurrentSize() (int64, error)
    MaxSize() int64
    Clean(ctx context.Context, opts CleanOptions) (CleanResult, error)
    Available() bool
}
```

### Config Structure

Location: `~/.config/cache-buster/config.yaml`

```yaml
version: "1"
providers:
  go-build:
    enabled: true
    paths:
      - ~/Library/Caches/go-build
    max_size: 10G
    clean_cmd: go clean -cache
```

Loading order: config file > defaults (merged)

## Output Formats

### Status Command

**Table output (default):**
```
┌──────────────┬─────────┬─────────┬────────┐
│ Provider     │ Current │ Max     │ Status │
├──────────────┼─────────┼─────────┼────────┤
│ go-build     │ 2.1 GiB │ 10 GiB  │ ok     │
│ npm          │ 4.5 GiB │ 3.0 GiB │ OVER   │
└──────────────┴─────────┴─────────┴────────┘

Total: 6.6 GiB
```

**JSON output (`--json`):**
```json
{
  "total": "6.6 GiB",
  "total_bytes": 7088545792,
  "providers": [
    {
      "name": "go-build",
      "current": "2.1 GiB",
      "current_bytes": 2254857830,
      "max": "10 GiB",
      "max_bytes": 10737418240,
      "over_limit": false
    }
  ]
}
```

### Clean Command

```bash
cache-buster clean go-build npm  # specific providers
cache-buster clean --all         # all enabled
cache-buster clean --dry-run     # preview only
cache-buster clean --force       # skip confirmation
cache-buster clean --quiet       # minimal output
```

## Quality Gates

Before committing:

- [ ] `golangci-lint run` passes
- [ ] `go test -race ./...` passes
- [ ] `go build ./cmd/cache-buster` succeeds
- [ ] Manual test of changed commands

```bash
golangci-lint run
go test -race ./...
go build -o cache-buster ./cmd/cache-buster
```

## Common Commands

```bash
# Build and run
go build -o cache-buster ./cmd/cache-buster
./cache-buster status
./cache-buster status --json

# Development
go run ./cmd/cache-buster status
go run ./cmd/cache-buster clean --dry-run --all

# Testing
go test ./...
go test -v -run TestClean ./internal/cli/...
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Linting
golangci-lint run
golangci-lint run --fix
```

## Testing Patterns

### Command Tests

Use `WithLoader` pattern for dependency injection:
```go
func TestCleanCmd(t *testing.T) {
    tmpDir := t.TempDir()
    cfgPath := filepath.Join(tmpDir, "config.yaml")
    // write test config...

    loader := config.NewLoader()
    loader.SetConfigPath(cfgPath)

    err := runCleanWithLoader(loader, args, flags...)
    require.NoError(t, err)
}
```

### Stdout Capture

```go
func captureStdout(t *testing.T, fn func()) string {
    old := os.Stdout
    r, w, _ := os.Pipe()
    os.Stdout = w
    fn()
    w.Close()
    os.Stdout = old
    var buf bytes.Buffer
    io.Copy(&buf, r)
    return buf.String()
}
```

## Anti-Patterns

**AVOID:**

- Skipping `Available()` check before cleaning (docker may not be running)
- Hardcoding paths without `~` expansion (use `config.ExpandPaths`)
- Forgetting to test both table and JSON output
- Adding providers without integration tests
- Using `os.Exit` directly (return errors instead)
- Silent failures in provider Clean methods

**REASON:** Providers interact with external tools and filesystems. Always verify availability and handle errors gracefully for good UX.

## Size Format Reference

Parsing accepts: `5G`, `5GB`, `5GiB`, `500M`, `500MB`, `500MiB`, `100K`, `100B`

Output uses binary units: `GiB`, `MiB`, `KiB`, `B`

## Error Handling

Errors bubble up through commands and display to stderr:
```
Error: load config: open ~/.config/cache-buster/config.yaml: no such file or directory
```

Exit codes:
- 0: Success
- 1: General error (config, provider, filesystem)
