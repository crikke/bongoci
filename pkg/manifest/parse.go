package manifest

import "fmt"

// Parse reads build.bongo at filePath and returns a Manifest.
func Parse(filePath string) (*Manifest, error) {
	return nil, fmt.Errorf("not implemented")
}

// ParseContent parses .bongo source with dir as the module's absolute path.
func ParseContent(content string, dir string) (*Manifest, error) {
	return nil, fmt.Errorf("not implemented")
}
