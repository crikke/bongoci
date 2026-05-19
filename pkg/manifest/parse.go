package manifest

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/crikke/ci/pkg/manifest/parser"
)

// Parse reads build.bongo at filePath and returns a Manifest.
func Parse(filePath string) (*Manifest, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", filePath, err)
	}
	return parser.Parse(string(data), filepath.Dir(filePath))
}

// ParseContent parses .bongo source with dir as the module's absolute path.
func ParseContent(content string, dir string) (*Manifest, error) {
	return parser.Parse(content, dir)
}
