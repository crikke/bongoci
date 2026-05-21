package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/crikke/ci/pkg/buildenv"
	"github.com/crikke/ci/pkg/compiler"
	"github.com/crikke/ci/pkg/manifest"
	"github.com/crikke/ci/pkg/runner"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "in main error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("ci", flag.ContinueOnError)
	verbose := fs.Bool("verbose", false, "enable debug logging")
	fs.BoolVar(verbose, "v", false, "shorthand for -verbose")
	useHostBuildkitDaemon := fs.Bool("use-host-buildkit-daemon", false, "connect to a buildkitd already running on the host instead of starting one")
	cacheFrom := fs.String("cache-from", "", "registry ref to import build cache from (e.g. myregistry/cache)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("fs parsing error: %w", err)
	}
	args = fs.Args()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	if len(args) < 2 || args[0] != "run" {
		return fmt.Errorf("usage: ci [-v] [--cache-from=<ref>] run <task> [<task>...]")
	}
	taskNames := args[1:]

	tomlPath, found := findBuildToml(mustCwd())
	if !found {
		return fmt.Errorf("build.toml not found (searched up from current directory)")
	}

	m, err := manifest.Parse(tomlPath)
	if err != nil {
		return err
	}
	slog.Debug("manifest parsed", "path", tomlPath, "tasks", len(m.Tasks))

	ctx := context.Background()

	var host string
	if !*useHostBuildkitDaemon {
		startCtx, startCancel := context.WithTimeout(ctx, 2*time.Minute)
		defer startCancel()
		env, startErr := buildenv.Start(startCtx)
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

	opts := runner.RunOptions{Host: host, CacheFrom: *cacheFrom}

	for _, taskName := range taskNames {
		if _, ok := m.Tasks[taskName]; !ok {
			names := make([]string, 0, len(m.Tasks))
			for n := range m.Tasks {
				names = append(names, n)
			}
			sort.Strings(names)
			return fmt.Errorf("unknown task %q; available: %v", taskName, names)
		}

		result, err := compiler.Compile(m, taskName)
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

func mustCwd() string {
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: could not determine working directory:", err)
		os.Exit(1)
	}
	return dir
}
