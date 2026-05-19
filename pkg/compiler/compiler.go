package compiler

import (
	"fmt"
	"log/slog"
	"path"
	"strings"

	"github.com/crikke/ci/pkg/manifest"
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
	depNames := make([]string, len(m.Dependencies))
	for i, dep := range m.Dependencies {
		name := fmt.Sprintf("dep-%d", i)
		depNames[i] = name
		localDirs[name] = dep
	}

	base := llb.Image(m.Image, imagemetaresolver.WithDefault, llb.WithCustomNamef("With base image: %s", m.Image))
	contextMount := llb.AddMount(m.AbsPath, llb.Local("context"))
	depMounts := make([]llb.RunOption, len(m.Dependencies))
	for i, dep := range m.Dependencies {
		depMounts[i] = llb.AddMount(dep, llb.Local(depNames[i]))
	}

	envs := []llb.StateOption{}

	for k, v := range m.Env {
		envs = append(envs, llb.AddEnv(k, v))
	}

	base = base.With(envs...)
	compiled := make(map[string]llb.State) // task name → /out scratch state

	for _, name := range sorted {
		task := m.Tasks[name]
		slog.Debug("compiling task", "task", task.Name)

		if task.Type == "docker" {
			compiled[name] = compileBuildahTask(task, contextMount, depMounts, compiled, m.AbsPath)
			continue
		}

		opts := []llb.RunOption{
			llb.Args([]string{"/bin/sh", "-c", task.Cmd}),
			llb.WithCustomNamef("Running task: %s", task.Name),
			llb.WithDescription(map[string]string{"foo": "bar"}),
			contextMount,
			llb.Dir(m.AbsPath),
		}
		opts = append(opts, depMounts...)

		for _, inp := range task.Inputs {
			srcPath := strings.TrimPrefix(inp.Path, "/out")
			upstreamOut := compiled[inp.Task.Name]
			var mountOpts []llb.MountOption
			if srcPath != "" {
				mountOpts = []llb.MountOption{llb.SourcePath(srcPath), llb.Readonly}
			} else {
				mountOpts = []llb.MountOption{llb.Readonly}
			}
			opts = append(opts, llb.AddMount(inp.Dest, upstreamOut, mountOpts...))
		}

		scratch := llb.AddMount("/out", llb.Scratch())
		opts = append(opts, scratch)
		exec := base.Run(opts...)
		compiled[name] = exec.GetMount("/out")
	}

	output := llb.Scratch()

	targetIncluded := false
	for _, v := range m.Outputs {
		if st, exists := compiled[v.TaskName]; exists {
			srcPath := strings.TrimPrefix(v.SrcPath, "/out")
			if srcPath == "" {
				srcPath = "/"
			}

			filename := path.Base(v.SrcPath)
			dest := path.Join("out", v.TaskName, filename)
			output = output.File(llb.Copy(st, srcPath, dest, &llb.CopyInfo{CreateDestPath: true}), llb.WithCustomNamef("Copy %s to output", srcPath))
			if v.TaskName == targetTaskName {
				targetIncluded = true
			}
		}
	}

	debug := llb.Image("alpine:latest").
		Run(
			llb.Shlex("ls -la /out"),
			llb.WithCustomName("List output files"),
		)
	debug.AddMount("/out", output, llb.Readonly)

	if !targetIncluded {
		output = output.File(llb.Copy(compiled[targetTaskName], "/", "/", &llb.CopyInfo{CreateDestPath: true}), llb.WithCustomName("Copy to output"))
	}

	return &Result{
		State:     output,
		LocalDirs: localDirs,
	}, nil
}

// compileBuildahTask compiles a docker task as a buildah exec, producing /out/image.tar.
// Downstream tasks can access the tarball at the declared inp.Dest path.
func compileBuildahTask(task *manifest.Task, contextMount llb.RunOption, depMounts []llb.RunOption, compiled map[string]llb.State, absPath string) llb.State {
	base := llb.Image(buildahImage, imagemetaresolver.WithDefault, llb.WithCustomNamef("Building image: %s", task.Name))

	cmd := fmt.Sprintf(
		"buildah --storage-driver=vfs build -f %s -t ci-build . && buildah --storage-driver=vfs push ci-build oci-archive:///out/%s.tar",
		task.Dockerfile, task.Dockerfile,
	)

	opts := []llb.RunOption{
		llb.Args([]string{"/bin/sh", "-c", cmd}),
		llb.WithCustomNamef("Building image: %s", task.Name),
		llb.Security(pb.SecurityMode_INSECURE),
		contextMount,
		llb.Dir(absPath),
	}
	opts = append(opts, depMounts...)

	for _, inp := range task.Inputs {
		srcPath := strings.TrimPrefix(inp.Path, "/out")
		upstreamOut := compiled[inp.Task.Name]
		var mountOpts []llb.MountOption
		if srcPath != "" {
			mountOpts = []llb.MountOption{llb.SourcePath(srcPath), llb.Readonly}
		} else {
			mountOpts = []llb.MountOption{llb.Readonly}
		}
		opts = append(opts, llb.AddMount(inp.Dest, upstreamOut, mountOpts...))
	}

	scratch := llb.AddMount("/out", llb.Scratch())
	opts = append(opts, scratch)
	exec := base.Run(opts...)
	return exec.GetMount("/out")
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
