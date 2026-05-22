// Package types defines the data model for a parsed .bongo manifest.
// It is a separate package so that both pkg/manifest and pkg/manifest/parser
// can import it without creating an import cycle.
package types

// Manifest is the parsed representation of a build.bongo file.
type Manifest struct {
	AbsPath string
	Version int
	Module  Module
	Tasks   map[string]*Task
}

// Module holds module-level metadata.
type Module struct {
	Name      string
	BaseImage string
	Include   []string // dependency paths, resolved to absolute at parse time
	Exports   []Export // task outputs to be written back to the host after the build
}

// Export references a named output from a task that should be
// materialized on the host filesystem after the build completes.
type Export struct {
	TaskName   string
	OutputName string
}

// Task is a single build step.
type Task struct {
	Name             string
	Cmd              *string
	Dockerfile       *string
	DockerfileOutput *string
	Cache            bool
	Inputs           []Input
	Outputs          []Output
}

// Input wires a named output from an upstream task into this task.
type Input struct {
	Task       *Task
	OutputName string
	Dest       string // mount destination inside the container
}

// Output is a named artifact produced by a task.
type Output struct {
	Name string
	Path string
}
