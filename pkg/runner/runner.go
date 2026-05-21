package runner

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"

	"github.com/crikke/ci/pkg/compiler"
	bkclient "github.com/moby/buildkit/client"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/tonistiigi/fsutil"
)

// ExportedOutput maps a path inside the /out scratch to a host destination.
// Exported for use in tests.
type ExportedOutput struct {
	SrcPath  string // path within the exported tmpDir (e.g. "/binary")
	DestPath string // host destination (e.g. "./out/binary")
}

// localMounts converts a map of name→hostPath into a map of name→fsutil.FS
// suitable for bkclient.SolveOpt.LocalMounts.
func localMounts(dirs map[string]string) (map[string]fsutil.FS, error) {
	mounts := make(map[string]fsutil.FS, len(dirs))
	for name, path := range dirs {
		fs, err := fsutil.NewFS(path)
		if err != nil {
			return nil, fmt.Errorf("create FS for local %q (%s): %w", name, path, err)
		}
		mounts[name] = fs
	}
	return mounts, nil
}

// Run solves the compiled LLB graph via buildkitd and copies declared outputs to the host.
func Run(ctx context.Context, host string, result *compiler.Result, outputs []ExportedOutput) error {
	c, err := bkclient.New(ctx, host)
	if err != nil {
		return fmt.Errorf("connect to buildkit at %q: %w\nhint: set BUILDKIT_HOST or ensure buildkitd is running", host, err)
	}
	slog.Debug("connected to buildkit", "host", host)
	defer c.Close()

	return solveExec(ctx, c, result, outputs)
}

func solveExec(ctx context.Context, c *bkclient.Client, result *compiler.Result, outputs []ExportedOutput) error {
	tmpDir, err := os.MkdirTemp("", "ci-export-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}

	defer os.RemoveAll(tmpDir)

	mounts, err := localMounts(result.LocalDirs)
	if err != nil {
		return err
	}

	solveOpt := bkclient.SolveOpt{
		Exports: []bkclient.ExportEntry{
			{Type: bkclient.ExporterLocal, OutputDir: tmpDir},
		},

		LocalMounts:         mounts,
		AllowedEntitlements: []string{"security.insecure"},
	}

	solveOpt = withCacheOpt(solveOpt, "")

	if err := solve(ctx, c, result, solveOpt); err != nil {
		return err
	}

	return CopyOutputs(tmpDir, outputs)
}

// https://github.com/moby/buildkit#export-cache
func withCacheOpt(opt bkclient.SolveOpt, cacheFrom string) bkclient.SolveOpt {
	if cacheFrom == "" {
		return opt
	}
	opt.CacheImports = append(opt.CacheImports, bkclient.CacheOptionsEntry{
		Type:  "registry",
		Attrs: map[string]string{"ref": cacheFrom},
	})
	return opt
}

func solve(ctx context.Context, c *bkclient.Client, result *compiler.Result, solveOpt bkclient.SolveOpt) error {
	ch := make(chan *bkclient.SolveStatus)
	done := make(chan struct{})
	go func() {
		defer close(done)
		printStatus(ctx, "Building", ch)
	}()

	_, err := c.Build(ctx, solveOpt, "", func(ctx context.Context, gwc gateway.Client) (*gateway.Result, error) {
		def, err := result.State.Marshal(ctx)
		if err != nil {
			return nil, fmt.Errorf("marshal LLB: %w", err)
		}

		return gwc.Solve(ctx, gateway.SolveRequest{
			Definition: def.ToPB(),
		})
	}, ch)

	<-done
	return err
}

// CopyOutputs copies each declared output from exportDir to its host DestPath.
// Exported for use in tests.
func CopyOutputs(exportDir string, outputs []ExportedOutput) error {

	var log func(dir string)
	log = func(dir string) {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			fullPath := path.Join(dir, e.Name())
			if e.IsDir() {
				log(fullPath)
			} else {
				slog.Info(fullPath, "IsDir", e.IsDir())
			}
		}
	}

	log(exportDir)

	for _, out := range outputs {
		srcDir := filepath.Join(exportDir, out.SrcPath)
		slog.Debug("copying output", "src", srcDir, "dest", out.DestPath)
		if err := os.MkdirAll(filepath.Dir(out.DestPath), 0o755); err != nil {
			return fmt.Errorf("mkdir %q: %w", filepath.Dir(out.DestPath), err)
		}
		if err := copyPath(srcDir, out.DestPath); err != nil {
			return fmt.Errorf("copy %q -> %q: %w", srcDir, out.DestPath, err)
		}
	}
	return nil
}

func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}

	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := copyPath(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func printStatus(ctx context.Context, phase string, ch chan *bkclient.SolveStatus) {
	d, err := progressui.NewDisplay(os.Stdout, progressui.AutoMode, progressui.WithPhase(phase))
	if err != nil {
		slog.Error(err.Error())
		return
	}
	d.UpdateFrom(ctx, ch)
}
