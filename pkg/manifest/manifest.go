package manifest

// Manifest is the parsed representation of build.toml.
type Manifest struct {
	AbsPath      string // absolute path to the module directory
	Name         string
	Image        string
	Dependencies []string // sibling module paths, resolved to absolute at parse time
	Tasks        map[string]*Task
	Outputs      map[string]Output
	Env          map[string]string
}

// Task represents one build step.
type Task struct {
	ID         string
	Name       string
	Cmd        string
	Type       string // "exec" (default) or "docker"
	Dockerfile string // docker tasks only
	Inputs     []TaskInput
}

// TaskInput wires a path from an upstream task's /out scratch into this task's container.
type TaskInput struct {
	Task *Task
	Path string // path within the upstream task's /out scratch (e.g. "/packages")
	Dest string // mount destination in this container (e.g. "/packages")
}

// Output declares what gets copied from a task's /out scratch back to the host.
// Outputs are written to --output-dir/{taskname}/{file}
type Output struct {
	TaskName string
	SrcPath  string // path within task's /out scratch (e.g. "/payments-service")
}
