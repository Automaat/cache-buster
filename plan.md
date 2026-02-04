# cache-buster - Implementation Plan

macOS developer cache manager with size limits.

---

## Phase 1: Project Setup

- [x] `go mod init github.com/marcinc/cache-buster`
- [x] Add dependencies:
  - `github.com/spf13/cobra`
  - `github.com/spf13/viper`
  - `github.com/dustin/go-humanize`
  - `github.com/charmbracelet/lipgloss`
- [x] Create directory structure:
  ```
  cmd/cache-buster/main.go
  internal/cli/
  internal/config/
  internal/provider/
  internal/cache/
  pkg/size/
  ```
- [x] Implement root Cobra command

---

## Phase 2: Config System

- [x] Define config structs in `internal/config/config.go`:
  ```go
  type Config struct {
      Version   string
      Providers map[string]Provider
  }
  type Provider struct {
      Enabled  bool
      Paths    []string
      MaxSize  string
      CleanCmd string
  }
  ```
- [x] Implement path expansion (`~`, globs)
- [x] Load/save YAML config via Viper
- [x] Create default config with all providers
- [x] Config location: `~/.config/cache-buster/config.yaml`

---

## Phase 3: Cache Scanner

- [ ] Implement `internal/cache/scanner.go`:
  - `CalculateSize(paths []string) (int64, error)` - parallel walk
  - `ListFiles(paths []string) ([]FileInfo, error)` - with mtime
  - `ExpandPaths(patterns []string) ([]string, error)`
- [ ] Implement `pkg/size/size.go`:
  - `ParseSize("10G") int64`
  - `FormatSize(int64) string`

---

## Phase 4: Status Command

- [ ] Implement `cache-buster status` in `internal/cli/status.go`
- [ ] Table output with lipgloss:
  ```
  │ Provider │ Current │ Max │ Status │
  ```
- [ ] Color coding: red=over, green=ok
- [ ] Add `--json` flag for machine output
- [ ] Show totals at bottom

---

## Phase 5: Provider Interface

- [ ] Define interface in `internal/provider/provider.go`:
  ```go
  type Provider interface {
      Name() string
      Paths() []string
      CurrentSize() (int64, error)
      MaxSize() int64
      Clean(ctx, opts) (CleanResult, error)
      Available() bool  // for Docker check
  }
  ```
- [ ] Implement registry to load providers from config
- [ ] MVP providers:
  - [ ] go-build (`go clean -cache`)
  - [ ] go-mod (`go clean -modcache`)
  - [ ] npm (`npm cache clean --force`)
  - [ ] yarn (`yarn cache clean`)
  - [ ] homebrew (`brew cleanup`)
  - [ ] mise (`mise prune`)
  - [ ] uv (file-based)
  - [ ] jetbrains (file-based)
  - [ ] docker (`docker system prune -af`, skip if not running)

---

## Phase 6: Cleaner

- [ ] Implement `internal/cache/cleaner.go`:
  - Sort files by mtime (oldest first)
  - Delete until under target size
  - Return bytes cleaned, files deleted
- [ ] External command execution with output capture
- [ ] Dry-run support (show what would be deleted)

---

## Phase 7: Clean Command

- [ ] Implement `cache-buster clean` in `internal/cli/clean.go`
- [ ] Flags:
  - `--all` - non-interactive, all providers
  - `--dry-run` - preview only
  - `--force` - skip confirmation
  - `--quiet` - minimal output
- [ ] Args: specific providers (`cache-buster clean go-build npm`)
- [ ] Default: interactive mode (select providers)

---

## Phase 8: Config Command

- [ ] `cache-buster config show` - display current config
- [ ] `cache-buster config init` - create default config
- [ ] `cache-buster config edit` - open in $EDITOR

---

## Phase 9: Interactive Mode (Optional)

- [ ] Add `github.com/charmbracelet/bubbletea`
- [ ] Provider selection with checkboxes
- [ ] Confirmation before cleaning
- [ ] Progress bar during clean

---

## Phase 10: Polish

- [ ] Makefile with build/install/test targets
- [ ] goreleaser config for releases
- [ ] README with usage examples
- [ ] Error handling for permission issues, locked files

---

## Verification Checklist

- [ ] `cache-buster status` matches `du -sh` output
- [ ] `cache-buster clean --dry-run` shows expected files
- [ ] `cache-buster clean go-build` actually frees space
- [ ] Docker skipped gracefully when not running
- [ ] Config changes take effect

---

## Provider Reference

| Provider | Path | Command |
|----------|------|---------|
| go-build | ~/Library/Caches/go-build | `go clean -cache` |
| go-mod | ~/go/pkg/mod | `go clean -modcache` |
| npm | ~/.npm | `npm cache clean --force` |
| yarn | ~/Library/Caches/Yarn | `yarn cache clean` |
| homebrew | ~/Library/Caches/Homebrew | `brew cleanup` |
| mise | ~/.local/share/mise | `mise prune` |
| uv | ~/.cache/uv | file-based |
| jetbrains | ~/Library/Caches/JetBrains | file-based |
| docker | ~/Library/Containers/com.docker.docker | `docker system prune -af` |

---

## Default Size Limits

```yaml
go-build: 10G
go-mod: 5G
npm: 3G
yarn: 2G
homebrew: 5G
mise: 8G
uv: 4G
jetbrains: 3G
docker: 50G
```
