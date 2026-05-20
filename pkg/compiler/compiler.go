package compiler

import (
	"fmt"
	"log/slog"
	"path"

	"github.com/crikke/ci/pkg/manifest"
	"github.com/crikke/ci/pkg/manifest/types"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/client/llb/imagemetaresolver"
	"github.com/moby/buildkit/solver/pb"
)

const buildahImage = "quay.io/buildah/stable"

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
	contextMount := llb.AddMount(m.AbsPath, llb.Local("context"))
	depMounts := make([]llb.RunOption, len(m.Module.Include))
	for i, dep := range m.Module.Include {
		depMounts[i] = llb.AddMount(dep, llb.Local(depNames[i]))
	}

	// TODO: handle envs
	envs := []llb.StateOption{}

	// for k, v := range m.Env {
	// 	envs = append(envs, llb.AddEnv(k, v))
	// }

	base = base.With(envs...)
	compiled := make(map[string]llb.State) // task name → /out scratch state

	for _, name := range sorted {
		task := m.Tasks[name]
		slog.Debug("compiling task", "task", task.Name)

		if task.Cmd == nil && task.Dockerfile == nil {
			return nil, fmt.Errorf("task %q has no CMD or DOCKERFILE", task.Name)
		}

		if task.Dockerfile != nil {
			st, err := compileBuildahTask(task, contextMount, depMounts, compiled, m.AbsPath)
			if err != nil {
				return nil, fmt.Errorf("task %q: %w", task.Name, err)
			}
			compiled[name] = st
			continue
		}

		if task.Cmd != nil {
			st, err := compileCmdTask(base, task, contextMount, depMounts, compiled, m.AbsPath)
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

func compileCmdTask(base llb.State, task *manifest.Task, contextMount llb.RunOption, depMounts []llb.RunOption, compiled map[string]llb.State, absPath string) (llb.State, error) {
	opts := []llb.RunOption{
		llb.Args([]string{"/bin/sh", "-c", *task.Cmd}),
		llb.WithCustomNamef("Running task: %s", task.Name),
		llb.WithDescription(map[string]string{"foo": "bar"}),
		contextMount,
		llb.Dir(absPath),
	}
	opts = append(opts, depMounts...)

	st, err := copyIhputs(base, task.Inputs, compiled)
	if err != nil {
		return llb.State{}, fmt.Errorf("task %q: %w", task.Name, err)
	}

	return st.Run(opts...).State, nil
}

// compileBuildahTask compiles a docker task as a buildah exec, producing /out/image.tar.
// Downstream tasks can access the tarball at the declared inp.Dest path.
func compileBuildahTask(task *manifest.Task, contextMount llb.RunOption, depMounts []llb.RunOption, compiled map[string]llb.State, absPath string) (llb.State, error) {
	base := llb.Image(buildahImage, imagemetaresolver.WithDefault, llb.WithCustomNamef("Building image: %s", task.Name))

	outFile := path.Join("/out", path.Base(*task.DockerfileOutput))
	cmd := fmt.Sprintf(
		"buildah --storage-driver=vfs build -f %s -t ci-build . && buildah --storage-driver=vfs push ci-build oci-archive://%s",
		*task.Dockerfile, outFile,
	)

	opts := []llb.RunOption{
		llb.Args([]string{"/bin/sh", "-c", cmd}),
		llb.WithCustomNamef("Building image: %s", task.Name),
		llb.Security(pb.SecurityMode_INSECURE),
		llb.AddMount("/out", llb.Scratch()),
		contextMount,
		llb.Dir(absPath),
	}
	opts = append(opts, depMounts...)

	st, err := copyIhputs(base, task.Inputs, compiled)
	if err != nil {
		return llb.State{}, fmt.Errorf("task %q: %w", task.Name, err)
	}

	exec := st.Run(opts...)
	return exec.GetMount("/out"), nil
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
