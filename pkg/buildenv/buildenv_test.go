//go:build integration

package buildenv_test

import (
	"context"
	"strings"
	"testing"
	"time"

	bkclient "github.com/moby/buildkit/client"

	"github.com/crikke/ci/pkg/buildenv"
)

func TestStart(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	env, err := buildenv.Start(ctx)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer env.Close()

	if !strings.HasPrefix(env.BuildkitHost, "unix://") {
		t.Fatalf("expected unix:// host, got %q", env.BuildkitHost)
	}

	c, err := bkclient.New(ctx, env.BuildkitHost)
	if err != nil {
		t.Fatalf("connect to buildkit: %v", err)
	}
	defer c.Close()

	if _, err := c.Info(ctx); err != nil {
		t.Fatalf("buildkit info: %v", err)
	}
}
