package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Automaat/cache-buster/internal/cache"
	"github.com/Automaat/cache-buster/internal/config"
	"github.com/Automaat/cache-buster/pkg/size"
)

var versionDirPattern = regexp.MustCompile(`^([a-zA-Z]+)(\d{4}\.\d+)$`)

// JetBrainsProvider cleans old JetBrains version directories while keeping the latest per product.
type JetBrainsProvider struct {
	*BaseProvider
}

// NewJetBrainsProvider creates a JetBrains version-aware provider.
func NewJetBrainsProvider(name string, cfg config.Provider) (*JetBrainsProvider, error) {
	base, err := NewBaseProvider(name, cfg)
	if err != nil {
		return nil, err
	}

	return &JetBrainsProvider{
		BaseProvider: base,
	}, nil
}

type versionDir struct {
	product string
	version string
	path    string
}

// Clean implements Provider.
func (p *JetBrainsProvider) Clean(ctx context.Context, opts CleanOptions) (CleanResult, error) {
	removable, err := p.findRemovableDirs()
	if err != nil {
		return CleanResult{}, err
	}

	if len(removable) == 0 {
		return CleanResult{Output: "no old versions to clean"}, nil
	}

	var (
		bytesTotal int64
		output     strings.Builder
	)

	for _, dir := range removable {
		select {
		case <-ctx.Done():
			return CleanResult{
				BytesCleaned: bytesTotal,
				Output:       "interrupted",
			}, ctx.Err()
		default:
		}

		dirSize, _ := cache.CalculateSize([]string{dir})

		if opts.DryRun {
			fmt.Fprintf(&output, "would remove: %s (%s)\n", filepath.Base(dir), size.FormatSize(dirSize.Size))
			bytesTotal += dirSize.Size
			continue
		}

		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintf(&output, "error removing %s: %v\n", filepath.Base(dir), err)
			continue
		}

		bytesTotal += dirSize.Size
	}

	if opts.DryRun {
		return CleanResult{
			BytesCleaned: bytesTotal,
			Output:       strings.TrimSpace(output.String()),
		}, nil
	}

	result := CleanResult{
		BytesCleaned: bytesTotal,
		Output:       fmt.Sprintf("removed %d old version directories", len(removable)),
	}
	if output.Len() > 0 {
		result.Output = strings.TrimSpace(output.String())
	}

	return result, nil
}

// findRemovableDirs returns paths of old version directories that can be removed.
func (p *JetBrainsProvider) findRemovableDirs() ([]string, error) {
	products := make(map[string][]versionDir)

	for _, basePath := range p.paths {
		entries, err := os.ReadDir(basePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			matches := versionDirPattern.FindStringSubmatch(entry.Name())
			if matches == nil {
				continue
			}

			vd := versionDir{
				product: matches[1],
				version: matches[2],
				path:    filepath.Join(basePath, entry.Name()),
			}
			products[vd.product] = append(products[vd.product], vd)
		}
	}

	var removable []string
	for _, versions := range products {
		if len(versions) <= 1 {
			continue
		}

		sort.Slice(versions, func(i, j int) bool {
			return versions[i].version < versions[j].version
		})

		// Remove all but the latest version
		for _, vd := range versions[:len(versions)-1] {
			removable = append(removable, vd.path)
		}
	}

	return removable, nil
}
