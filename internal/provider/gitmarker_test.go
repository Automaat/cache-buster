package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsGitMarkerName(t *testing.T) {
	for _, name := range []string{".git", ".GIT", ".Git", ".gIt"} {
		assert.True(t, IsGitMarkerName(name), name)
	}
	for _, name := range []string{".gitignore", ".github", "git", "x.git", ".git2", ""} {
		assert.False(t, IsGitMarkerName(name), name)
	}
}
