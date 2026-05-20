package manifest

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/crikke/ci/pkg/manifest/parser"
)

// Parse reads build.bongo at filePath and returns a Manifest.
func Parse(filePath string) (*Manifest, error) {
	abs, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("resolve path %s: %w", filePath, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", abs, err)
	}
	return parser.Parse(string(data), filepath.Dir(abs))
}

// ParseContent parses .bongo source with dir as the module's absolute path.
func ParseContent(content string, dir string) (*Manifest, error) {
	return parser.Parse(content, dir)
}
