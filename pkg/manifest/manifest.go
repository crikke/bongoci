package manifest

import "github.com/crikke/ci/pkg/manifest/types"

// Type aliases so callers continue to use manifest.Manifest, manifest.Task, etc.
// while pkg/manifest/parser can import pkg/manifest/types without a cycle.

type Manifest = types.Manifest
type Module = types.Module
type Export = types.Export
type Task = types.Task
type Input = types.Input
type Output = types.Output
