package manifest

import (
	"fmt"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Parse reads build.toml from filePath and returns a Manifest.
func Parse(filePath string) (*Manifest, error) {
	var raw tomlManifest
	if _, err := toml.DecodeFile(filePath, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filePath, err)
	}
	return build(&raw, filepath.Dir(filePath))
}

// ParseContent parses TOML content with dir as the module's absolute path.
func ParseContent(content string, dir string) (*Manifest, error) {
	var raw tomlManifest
	if _, err := toml.Decode(content, &raw); err != nil {
		return nil, fmt.Errorf("parse toml: %w", err)
	}
	return build(&raw, dir)
}

func build(raw *tomlManifest, dir string) (*Manifest, error) {
	if raw.Module.Name == "" {
		return nil, fmt.Errorf("manifest is missing module.name")
	}
	if raw.Module.Image == "" {
		return nil, fmt.Errorf("manifest is missing module.image")
	}
	if raw.Version != 1 {
		return nil, fmt.Errorf("unsupported manifest version %d (expected 1)", raw.Version)
	}

	deps := make([]string, len(raw.Module.Dependencies))
	for i, d := range raw.Module.Dependencies {
		if filepath.IsAbs(d) {
			deps[i] = d
		} else {
			deps[i] = filepath.Clean(filepath.Join(dir, d))
		}
	}

	// Pass 1: create Task instances without inputs.
	taskMap := make(map[string]*Task, len(raw.Tasks))
	for name, rt := range raw.Tasks {
		typ := rt.Type
		if typ == "" {
			typ = "exec"
		}
		taskMap[name] = &Task{
			ID:         name,
			Name:       name,
			Cmd:        rt.Cmd,
			Type:       typ,
			Dockerfile: rt.Dockerfile,
		}
	}

	// Pass 2: wire inputs.
	for name, rt := range raw.Tasks {
		if len(rt.Inputs) == 0 {
			continue
		}
		inputs := make([]TaskInput, 0, len(rt.Inputs))
		for _, ri := range rt.Inputs {
			dep, ok := taskMap[ri.Task]
			if !ok {
				return nil, fmt.Errorf("task %q: unknown input task %q", name, ri.Task)
			}
			inputs = append(inputs, TaskInput{
				Task: dep,
				Path: ri.Path,
				Dest: ri.Dest,
			})
		}
		taskMap[name].Inputs = inputs
	}

	if err := checkCycles(taskMap); err != nil {
		return nil, err
	}

	outputMap := make(map[string]Output, len(raw.Outputs))
	for name, ro := range raw.Outputs {
		if _, ok := taskMap[ro.From.Task]; !ok {
			return nil, fmt.Errorf("output %q: unknown task %q", name, ro.From.Task)
		}
		outputMap[name] = Output{
			TaskName: ro.From.Task,
			SrcPath:  ro.From.Path,
		}
	}

	return &Manifest{
		AbsPath:      dir,
		Name:         raw.Module.Name,
		Image:        raw.Module.Image,
		Dependencies: deps,
		Tasks:        taskMap,
		Outputs:      outputMap,
	}, nil
}

func checkCycles(tasks map[string]*Task) error {
	visited := make(map[string]bool)
	inStack := make(map[string]bool)

	var dfs func(name string) error
	dfs = func(name string) error {
		if inStack[name] {
			return fmt.Errorf("cycle detected at task %q", name)
		}
		if visited[name] {
			return nil
		}
		inStack[name] = true
		for _, inp := range tasks[name].Inputs {
			if err := dfs(inp.Task.Name); err != nil {
				return err
			}
		}
		inStack[name] = false
		visited[name] = true
		return nil
	}

	for name := range tasks {
		if err := dfs(name); err != nil {
			return err
		}
	}
	return nil
}

// TOML wire types

type tomlManifest struct {
	Version int                   `toml:"version"`
	Module  tomlModule            `toml:"module"`
	Tasks   map[string]tomlTask   `toml:"tasks"`
	Outputs map[string]tomlOutput `toml:"outputs"`
}

type tomlModule struct {
	Name         string   `toml:"name"`
	Image        string   `toml:"image"`
	Dependencies []string `toml:"dependencies"`
}

type tomlTask struct {
	Cmd        string      `toml:"cmd"`
	Type       string      `toml:"type"`
	Dockerfile string      `toml:"dockerfile"`
	Inputs     []tomlInput `toml:"inputs"`
}

type tomlInput struct {
	Task string `toml:"task"`
	Path string `toml:"path"`
	Dest string `toml:"dest"`
}

type tomlOutput struct {
	From tomlOutputFrom `toml:"from"`
}

type tomlOutputFrom struct {
	Task string `toml:"task"`
	Path string `toml:"path"`
}
