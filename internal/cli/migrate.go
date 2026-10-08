package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/smykla-skalski/bilgie/internal/migrate"
)

// MigrateLegacy moves the config and state dirs of the pre-rename name to the
// current ones before any command reads them.
func MigrateLegacy(out io.Writer) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(out, "warning: not migrating legacy dirs: %v\n", err)
		return
	}
	migrate.Legacy(home, out)
}
