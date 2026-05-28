package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/crikke/ci/pkg/buildenv"
	"github.com/crikke/ci/pkg/compiler"
	"github.com/crikke/ci/pkg/manifest"
	"github.com/crikke/ci/pkg/runner"
	"github.com/spf13/cobra"
)

func newRunCmd() *cobra.Command {
	var (
		useHostBuildkitDaemon bool
		cacheFrom             string
		cacheInsecure         bool
		buildkitImage         string
		buildahImage          string
	)

	cmd := &cobra.Command{
		Use:   "run <task> [<task>...]",
		Short: "Run one or more tasks from build.bongo",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTasks(args, useHostBuildkitDaemon, cacheFrom, cacheInsecure, buildkitImage, buildahImage)
		},
	}

	cmd.Flags().BoolVar(&useHostBuildkitDaemon, "use-host-buildkit-daemon", false, "connect to a buildkitd already running on the host instead of starting one")
	cmd.Flags().StringVar(&cacheFrom, "cache-from", "", "registry ref to import build cache from (e.g. myregistry/cache)")
	cmd.Flags().BoolVar(&cacheInsecure, "cache-insecure", false, "allow plain-HTTP registry for cache (needed for local registries)")
	cmd.Flags().StringVar(&buildkitImage, "buildkit-image", "moby/buildkit:v0.29.0-ubuntu", "use a different buildkit image")
	cmd.Flags().StringVar(&buildahImage, "buildah-image", "quay.io/buildah/stable:v1.43.1", "use a different buildah image")

	return cmd
}

func runTasks(taskNames []string, useHostBuildkitDaemon bool, cacheFrom string, cacheInsecure bool, buildkitImage, buildahImage string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}
	tomlPath, found := findBuildToml(cwd)
	if !found {
		return fmt.Errorf("build.bongo not found (searched up from current directory)")
	}

	m, err := manifest.Parse(tomlPath)
	if err != nil {
		return err
	}
	slog.Debug("manifest parsed", "path", tomlPath, "tasks", len(m.Tasks))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var host string
	if !useHostBuildkitDaemon {
		startCtx, startCancel := context.WithTimeout(ctx, 2*time.Minute)
		defer startCancel()
		env, startErr := buildenv.Start(startCtx, buildkitImage)
		if startErr != nil {
			return fmt.Errorf("start build environment: %w", startErr)
		}
		defer env.Close()
		host = env.BuildkitHost
	} else {
		host = os.Getenv("BUILDKIT_HOST")
		if host == "" {
			host = "unix:///run/buildkit/buildkitd.sock"
		}
	}

	slog.Debug("buildkit host", "host", host)

	opts := runner.RunOptions{Host: host, CacheFrom: cacheFrom, InsecureCache: cacheInsecure}

	for _, taskName := range taskNames {
		if _, ok := m.Tasks[taskName]; !ok {
			names := make([]string, 0, len(m.Tasks))
			for n := range m.Tasks {
				names = append(names, n)
			}
			sort.Strings(names)
			return fmt.Errorf("unknown task %q; available: %v", taskName, names)
		}

		result, err := compiler.Compile(m, taskName, buildahImage)
		if err != nil {
			return fmt.Errorf("compile %q: %w", taskName, err)
		}

		var taskOutputs []runner.ExportedOutput
		task := m.Tasks[taskName]
		for _, exp := range m.Module.Exports {
			if exp.TaskName != taskName {
				continue
			}
			for _, out := range task.Outputs {
				if out.Name == exp.OutputName {
					taskOutputs = append(taskOutputs, runner.ExportedOutput{
						SrcPath:  filepath.Join("out", taskName, out.Name, filepath.Base(out.Path)),
						DestPath: filepath.Join("out", taskName, out.Name, filepath.Base(out.Path)),
					})
					break
				}
			}
		}

		slog.Info("running task", "task", taskName)
		if err := runner.Run(ctx, opts, result, taskOutputs); err != nil {
			return fmt.Errorf("task %q failed: %w", taskName, err)
		}
	}
	return nil
}

func findBuildToml(dir string) (string, bool) {
	for {
		candidate := filepath.Join(dir, "build.bongo")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}
