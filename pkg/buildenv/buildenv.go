package buildenv

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"time"

	bkclient "github.com/moby/buildkit/client"
	"github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"
)

// Environment holds the Docker resources that form the managed build environment.
// Call Close when done to release all resources.
type Environment struct {
	BuildkitHost string

	dockerClient *dockerclient.Client
	buildkitID   string
	tmpDir       string
}

// Start provisions a Docker network and a buildkitd container, waits for
// buildkitd to be ready, and returns the environment. All resources are
// cleaned up if any step fails.
func Start(ctx context.Context, img string) (*Environment, error) {
	tmpDir, err := os.MkdirTemp("", "ci-buildkitd-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}

	cli, err := dockerclient.NewClientWithOpts(
		dockerclient.FromEnv,
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("docker client: %w", err)
	}

	teardown := func(containerID string, started bool) {
		if containerID != "" {
			if started {
				stopTimeout := 5
				_, _ = cli.ContainerStop(context.Background(), containerID, dockerclient.ContainerStopOptions{Timeout: &stopTimeout})
			}
			_, _ = cli.ContainerRemove(context.Background(), containerID, dockerclient.ContainerRemoveOptions{Force: true})
		}
		cli.Close()
		os.RemoveAll(tmpDir)
	}

	slog.Debug("pulling buildkit image", "image", img)
	pullResp, err := cli.ImagePull(ctx, img, dockerclient.ImagePullOptions{})
	if err != nil {
		teardown("", false)
		return nil, fmt.Errorf("pull buildkit image: %w", err)
	}
	if err := pullResp.Wait(ctx); err != nil {
		teardown("", false)
		return nil, fmt.Errorf("pull buildkit image: %w", err)
	}

	resp, err := cli.ContainerCreate(ctx, dockerclient.ContainerCreateOptions{
		Config: &container.Config{
			Image: img,
			Cmd: []string{
				"--group", strconv.Itoa(os.Getgid()),
				"--allow-insecure-entitlement", "security.insecure",
				"--oci-worker-no-process-sandbox",
			},
		},
		HostConfig: &container.HostConfig{
			Privileged: true,
			Binds:      []string{tmpDir + ":/run/buildkit"},
			ExtraHosts: []string{"host.docker.internal:host-gateway"},
		},
	})
	if err != nil {
		teardown("", false)
		return nil, fmt.Errorf("create buildkitd container: %w", err)
	}

	if _, err := cli.ContainerStart(ctx, resp.ID, dockerclient.ContainerStartOptions{}); err != nil {
		teardown(resp.ID, false)
		return nil, fmt.Errorf("start buildkitd container: %w", err)
	}

	socketHost := "unix://" + tmpDir + "/buildkitd.sock"
	slog.Debug("waiting for buildkitd", "host", socketHost)

	if err := waitForBuildkitd(ctx, socketHost); err != nil {
		logs := fetchContainerLogs(resp.ID, cli)
		teardown(resp.ID, true)
		return nil, fmt.Errorf("wait for buildkitd: %w\nbuildkitd logs:\n%s", err, logs)
	}
	slog.Debug("buildkitd ready", "host", socketHost)

	return &Environment{
		BuildkitHost: socketHost,
		dockerClient: cli,
		buildkitID:   resp.ID,
		tmpDir:       tmpDir,
	}, nil
}

func (e *Environment) Close() {
	stopTimeout := 5
	if _, err := e.dockerClient.ContainerStop(context.Background(), e.buildkitID, dockerclient.ContainerStopOptions{Timeout: &stopTimeout}); err != nil {
		slog.Warn("stop buildkitd container", "error", err)
	}
	e.dockerClient.Close()
	os.RemoveAll(e.tmpDir)
}

func fetchContainerLogs(containerID string, cli *dockerclient.Client) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := cli.ContainerLogs(ctx, containerID, dockerclient.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		return "(could not fetch logs: " + err.Error() + ")"
	}
	defer r.Close()
	// Docker log stream: 8-byte header (1 byte type, 3 padding, 4 byte size) + payload per frame.
	var buf bytes.Buffer
	hdr := make([]byte, 8)
	for {
		if _, err := io.ReadFull(r, hdr); err != nil {
			break
		}
		size := binary.BigEndian.Uint32(hdr[4:])
		if _, err := io.CopyN(&buf, r, int64(size)); err != nil {
			break
		}
	}
	return buf.String()
}

func waitForBuildkitd(ctx context.Context, host string) error {
	c, err := bkclient.New(ctx, host)
	if err != nil {
		return err
	}
	defer c.Close()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := c.Info(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
