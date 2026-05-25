package buildenv

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	bkclient "github.com/moby/buildkit/client"
	"github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"
)

const builderImage = "docker.io/moby/buildkit:rootless"

// Environment holds the Docker resources that form the managed build environment.
// Call Close when done to release all resources.
type Environment struct {
	BuildkitHost string

	dockerClient *dockerclient.Client
	networkID    string
	buildkitID   string
	tmpDir       string
}

// Start provisions a Docker network and a buildkitd container, waits for
// buildkitd to be ready, and returns the environment. All resources are
// cleaned up if any step fails.
func Start(ctx context.Context) (*Environment, error) {
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

	networkName := "ci-build-" + filepath.Base(tmpDir)
	netResult, err := cli.NetworkCreate(ctx, networkName, dockerclient.NetworkCreateOptions{
		Driver: "bridge", Internal: true,
	})
	if err != nil {
		cli.Close()
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("create docker network: %w", err)
	}
	networkID := netResult.ID

	teardown := func(containerID string, started bool) {
		if containerID != "" {
			if started {
				stopTimeout := 5
				_, _ = cli.ContainerStop(context.Background(), containerID, dockerclient.ContainerStopOptions{Timeout: &stopTimeout})
			}
			_, _ = cli.ContainerRemove(context.Background(), containerID, dockerclient.ContainerRemoveOptions{Force: true})
		}
		_, _ = cli.NetworkRemove(context.Background(), networkID, dockerclient.NetworkRemoveOptions{})
		cli.Close()
		os.RemoveAll(tmpDir)
	}

	slog.Debug("pulling builder image", "image", builderImage)
	pullResp, err := cli.ImagePull(ctx, builderImage, dockerclient.ImagePullOptions{})
	if err != nil {
		teardown("", false)
		return nil, fmt.Errorf("pull %s: %w", builderImage, err)
	}
	if err := pullResp.Wait(ctx); err != nil {
		pullResp.Close()
		teardown("", false)
		return nil, fmt.Errorf("pull %s: %w", builderImage, err)
	}
	pullResp.Close()

	resp, err := cli.ContainerCreate(ctx, dockerclient.ContainerCreateOptions{
		Config: &container.Config{
			Image: builderImage,
			Cmd:   []string{"--addr", "unix:///run/user/1000/buildkit/buildkitd.sock", "--oci-worker-no-process-sandbox"},
		},
		HostConfig: &container.HostConfig{
			Binds:       []string{tmpDir + ":/run/user/1000/buildkit"},
			SecurityOpt: []string{"seccomp=unconfined"},
			Resources:   container.Resources{Devices: []container.DeviceMapping{{PathOnHost: "/dev/fuse", PathInContainer: "/dev/fuse", CgroupPermissions: "rwm"}}},
			ExtraHosts:  []string{"host.docker.internal:host-gateway"},
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
		teardown(resp.ID, true)
		return nil, fmt.Errorf("wait for buildkitd: %w", err)
	}
	slog.Debug("buildkitd ready", "host", socketHost)

	return &Environment{
		BuildkitHost: socketHost,
		dockerClient: cli,
		networkID:    networkID,
		buildkitID:   resp.ID,
		tmpDir:       tmpDir,
	}, nil
}

func (e *Environment) Close() {
	stopTimeout := 5
	if _, err := e.dockerClient.ContainerStop(context.Background(), e.buildkitID, dockerclient.ContainerStopOptions{Timeout: &stopTimeout}); err != nil {
		slog.Warn("stop buildkitd container", "error", err)
	}
	if _, err := e.dockerClient.NetworkRemove(context.Background(), e.networkID, dockerclient.NetworkRemoveOptions{}); err != nil {
		slog.Warn("remove docker network", "error", err)
	}
	e.dockerClient.Close()
	os.RemoveAll(e.tmpDir)
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
