package compiler

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/crikke/ci/pkg/manifest"
	"github.com/crikke/ci/pkg/manifest/types"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/client/llb/imagemetaresolver"
)

const buildkitRootlessImage = "moby/buildkit:rootless"

// Result holds the compiled LLB state and metadata needed by the runner.
type Result struct {
	State     llb.State
	LocalDirs map[string]string
}

// Compile converts targetTaskName (and its transitive inputs) into an LLB state.
// Each exec task runs in the base image with:
//   - context + dependencies mounted read-only at their absolute host paths
//   - upstream task outputs mounted read-only at the declared Dest paths
//   - a scratch volume at /out as the sole writable output
func Compile(m *manifest.Manifest, targetTaskName string) (*Result, error) {
	if _, ok := m.Tasks[targetTaskName]; !ok {
		return nil, fmt.Errorf("unknown task %q", targetTaskName)
	}

	sorted, err := topoSort(m.Tasks, targetTaskName)
	if err != nil {
		return nil, err
	}
	slog.Debug("topo sort", "order", sorted)

	localDirs := map[string]string{"context": m.AbsPath}

	depNames := make([]string, len(m.Module.Include))
	for i, dep := range m.Module.Include {
		name := fmt.Sprintf("dep-%d", i)
		depNames[i] = name
		localDirs[name] = dep
	}

	base := llb.Image(m.Module.BaseImage, imagemetaresolver.WithDefault, llb.WithCustomNamef("With base image: %s", m.Module.BaseImage))

	ignorePatterns, err := getIgnorePatterns(m.AbsPath)
	if err != nil {
		return nil, fmt.Errorf("get ignore patterns: %w", err)
	}
	contextMount := llb.AddMount(m.AbsPath, llb.Local("context", llb.ExcludePatterns(ignorePatterns)))
	depMounts := make([]llb.RunOption, len(m.Module.Include))
	for i, dep := range m.Module.Include {
		depMounts[i] = llb.AddMount(dep, llb.Local(depNames[i]))
	}

	compiled := make(map[string]llb.State) // task name → /out scratch state

	for _, name := range sorted {
		task := m.Tasks[name]
		slog.Debug("compiling task", "task", task.Name)

		if task.Cmd == nil && task.Dockerfile == nil {
			return nil, fmt.Errorf("task %q has no CMD or DOCKERFILE", task.Name)
		}

		taskEnv := effectiveEnv(m.Module.Env, task.Env)

		if task.Dockerfile != nil {
			st, err := compileDockerfileTask(task, contextMount, depMounts, compiled, m.AbsPath, taskEnv)
			if err != nil {
				return nil, fmt.Errorf("task %q: %w", task.Name, err)
			}
			compiled[name] = st
			continue
		}

		if task.Cmd != nil {
			st, err := compileCmdTask(base, task, contextMount, depMounts, compiled, m.AbsPath, taskEnv)
			if err != nil {
				return nil, fmt.Errorf("task %q: %w", task.Name, err)
			}
			compiled[name] = st
			continue
		}
		opts := []llb.RunOption{
			llb.Args([]string{"/bin/sh", "-c", *task.Cmd}),
			llb.WithCustomNamef("Running task: %s", task.Name),
			llb.WithDescription(map[string]string{"foo": "bar"}),
			contextMount,
			llb.Dir(m.AbsPath),
		}
		opts = append(opts, depMounts...)

		st, err := copyIhputs(base, task.Inputs, compiled)
		if err != nil {
			return nil, fmt.Errorf("task %q: %w", task.Name, err)
		}

		exec := st.Run(opts...)

		compiled[name] = exec.State
	}

	output, err := createExportState(targetTaskName, m, compiled)
	if err != nil {
		return nil, fmt.Errorf("create output state: %w", err)
	}

	return &Result{
		State:     output,
		LocalDirs: localDirs,
	}, nil
}

// Return patterns to ignore for a local directory.
// If a .bongoignore file exists it takes precedence over .dockerignore, but if neither exists then an empty list is returned (i.e. no ignores).
func getIgnorePatterns(contextPath string) ([]string, error) {
	readIgnoreFile := func(path string) ([]string, error) {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("read ignore file: %w", err)
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		var patterns []string
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			patterns = append(patterns, line)
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("scan ignore file: %w", err)
		}
		return patterns, nil
	}

	if _, err := os.Stat(path.Join(contextPath, ".bongoignore")); err == nil {
		patterns, err := readIgnoreFile(path.Join(contextPath, ".bongoignore"))
		if err != nil {
			return []string{}, fmt.Errorf("Error reading .bongoignore: %v", err)
		}
		return patterns, nil
	}

	if _, err := os.Stat(path.Join(contextPath, ".dockerignore")); err == nil {
		patterns, err := readIgnoreFile(path.Join(contextPath, ".dockerignore"))
		if err != nil {
			return []string{}, fmt.Errorf("Error reading .dockerignore: %v", err)
		}
		return patterns, nil
	}

	return []string{}, nil
}
func createExportState(targetTaskName string, m *manifest.Manifest, compiled map[string]llb.State) (llb.State, error) {
	output := llb.Scratch()
	targetIncluded := false
	inputs := make([]types.Input, 0)

	for _, exp := range m.Module.Exports {

		if _, ok := compiled[exp.TaskName]; !ok {
			continue
		}

		fromTask := m.Tasks[exp.TaskName]
		var outputPath string
		for _, taskOutput := range fromTask.Outputs {
			if taskOutput.Name == exp.OutputName {
				outputPath = path.Base(taskOutput.Path)
				break
			}
		}

		dest := path.Join("out", exp.TaskName, exp.OutputName, outputPath)
		inputs = append(inputs, types.Input{
			Task:       fromTask,
			OutputName: exp.OutputName,
			Dest:       dest,
		})

		if exp.TaskName == targetTaskName {
			targetIncluded = true
		}
	}

	var err error
	output, err = copyIhputs(output, inputs, compiled)
	if err != nil {
		return llb.State{}, fmt.Errorf("copying to output %q: %w", targetTaskName, err)
	}

	// TODO: Instead of creating this state the compiled[targetTaskName] can just be returned in the calling fuction.
	// So add a check for that.

	if !targetIncluded {
		output = output.File(llb.Copy(compiled[targetTaskName], "/", "/", &llb.CopyInfo{CreateDestPath: true}), llb.WithCustomName("Copy to output"))
	}

	return output, nil
}

func copyIhputs(base llb.State, inputs []types.Input, compiled map[string]llb.State) (llb.State, error) {

	resultTask := base

	for _, input := range inputs {

		upstreamTask, ok := compiled[input.Task.Name]

		if !ok {
			return llb.State{}, fmt.Errorf("task '%s' is not compiled, how the fk this happend is unclear", input.Task.Name)
		}

		for _, out := range input.Task.Outputs {
			if out.Name == input.OutputName {

				resultTask = resultTask.File(llb.Copy(upstreamTask, out.Path, input.Dest, &llb.CopyInfo{CreateDestPath: true}), llb.WithCustomNamef("Copying file '%s' from task '%s'", out.Name, input.Task.Name))
				break
			}
		}

	}

	return resultTask, nil
}

func compileCmdTask(base llb.State, task *manifest.Task, contextMount llb.RunOption, depMounts []llb.RunOption, compiled map[string]llb.State, absPath string, env map[string]string) (llb.State, error) {
	opts := []llb.RunOption{
		llb.Args([]string{"/bin/sh", "-c", *task.Cmd}),
		llb.WithCustomNamef("Running task: %s", task.Name),
		llb.WithDescription(map[string]string{"foo": "bar"}),
		contextMount,
		llb.Dir(absPath),
	}
	if !task.Cache {
		opts = append(opts, llb.IgnoreCache)
	}
	opts = append(opts, depMounts...)

	st, err := copyIhputs(base, task.Inputs, compiled)
	if err != nil {
		return llb.State{}, fmt.Errorf("task %q: %w", task.Name, err)
	}
	st = st.With(envStateOptions(env)...)

	return st.Run(opts...).State, nil
}

// compileDockerfileTask compiles a DOCKERFILE task using buildctl-daemonless.sh inside
// moby/buildkit:rootless, producing an OCI archive at /out/<basename>.
// No security.insecure entitlement is required; nested user namespaces on the host suffice.
func compileDockerfileTask(task *manifest.Task, contextMount llb.RunOption, depMounts []llb.RunOption, compiled map[string]llb.State, absPath string, env map[string]string) (llb.State, error) {
	base := llb.Image(buildkitRootlessImage, imagemetaresolver.WithDefault, llb.WithCustomNamef("Building image: %s", task.Name))
	base = base.With(envStateOptions(env)...)

	outFile := path.Join("/out", path.Base(*task.DockerfileOutput))

	opts := []llb.RunOption{
		llb.Args([]string{
			"buildctl-daemonless.sh", "build",
			"--frontend", "dockerfile.v0",
			"--local", "context=" + absPath,
			"--local", "dockerfile=" + absPath,
			"--opt", "filename=" + *task.Dockerfile,
			"--output", "type=oci,dest=" + outFile,
		}),
		llb.AddEnv("BUILDKITD_FLAGS", "--oci-worker-no-process-sandbox"),
		llb.WithCustomNamef("Building image: %s", task.Name),
		llb.AddMount("/out", llb.Scratch()),
		contextMount,
		llb.Dir(absPath),
	}
	if !task.Cache {
		opts = append(opts, llb.IgnoreCache)
	}
	opts = append(opts, depMounts...)

	st, err := copyIhputs(base, task.Inputs, compiled)
	if err != nil {
		return llb.State{}, fmt.Errorf("task %q: %w", task.Name, err)
	}

	exec := st.Run(opts...)
	return exec.GetMount("/out"), nil
}

// effectiveEnv merges moduleEnv with taskEnv; taskEnv values override moduleEnv on key collision.
func effectiveEnv(moduleEnv, taskEnv map[string]string) map[string]string {
	if len(moduleEnv) == 0 && len(taskEnv) == 0 {
		return nil
	}
	out := make(map[string]string, len(moduleEnv)+len(taskEnv))
	for k, v := range moduleEnv {
		out[k] = v
	}
	for k, v := range taskEnv {
		out[k] = v
	}
	return out
}

// envStateOptions converts an env map into a slice of llb.StateOptions ready for State.With.
func envStateOptions(env map[string]string) []llb.StateOption {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	opts := make([]llb.StateOption, 0, len(keys))
	for _, k := range keys {
		opts = append(opts, llb.AddEnv(k, env[k]))
	}
	return opts
}

// topoSort returns task names in dependency-first order for targetTask's subgraph.
func topoSort(tasks map[string]*manifest.Task, target string) ([]string, error) {
	var result []string
	visited := make(map[string]bool)
	inStack := make(map[string]bool)

	var visit func(name string) error
	visit = func(name string) error {
		if inStack[name] {
			return fmt.Errorf("cycle at task %q", name)
		}
		if visited[name] {
			return nil
		}
		t, ok := tasks[name]
		if !ok {
			return fmt.Errorf("task %q not found in manifest", name)
		}
		inStack[name] = true
		for _, inp := range t.Inputs {
			if err := visit(inp.Task.Name); err != nil {
				return err
			}
		}
		inStack[name] = false
		visited[name] = true
		result = append(result, name)
		return nil
	}

	return result, visit(target)
}
