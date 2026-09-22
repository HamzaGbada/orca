package cli

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"orca/internal/extensions/engine"
)

func unavailable(context.Context, Options) (*Services, error) {
	return nil, fmt.Errorf("%w: dial unix /var/run/docker.sock: connect: no such file or directory", engine.ErrUnavailable)
}

func mustNotConnect(t *testing.T) Connector {
	return func(context.Context, Options) (*Services, error) {
		t.Fatal("connect must not be called")
		return nil, nil
	}
}

func run(connect Connector, args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = Run(context.Background(), args, &out, &errOut, connect)
	return code, out.String(), errOut.String()
}

func TestRunUsageErrors(t *testing.T) {
	tests := [][]string{
		{},
		{"bogus"},
		{"images", "--bogus-flag"},
		{"images", "extra-arg"},
	}
	for _, args := range tests {
		if code, _, _ := run(mustNotConnect(t), args...); code != exitUsage {
			t.Errorf("orca %v: exit %d, want %d", args, code, exitUsage)
		}
	}
}

func TestRunConflictingFlags(t *testing.T) {
	svc := &Services{Close: func() error { return nil }}
	connect := func(context.Context, Options) (*Services, error) { return svc, nil }

	for _, args := range [][]string{
		{"containers", "--running", "--stopped"},
		{"images", "--used", "--unused"},
	} {
		code, _, stderr := run(connect, args...)
		if code != exitUsage || !strings.Contains(stderr, "mutually exclusive") {
			t.Errorf("orca %v: exit %d stderr %q, want usage error", args, code, stderr)
		}
	}
}

func TestRunHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"--help"}, {"version"}, {"images", "-h"}} {
		if code, _, _ := run(mustNotConnect(t), args...); code != exitOK {
			t.Errorf("orca %v: exit %d, want 0", args, code)
		}
	}
	if _, stdout, _ := run(mustNotConnect(t), "version"); !strings.HasPrefix(stdout, "orca ") {
		t.Errorf("version output %q", stdout)
	}
}

func TestRunDaemonUnavailable(t *testing.T) {
	code, stdout, stderr := run(unavailable, "info")
	if code != exitError {
		t.Errorf("exit %d, want %d", code, exitError)
	}
	if stdout != "" {
		t.Errorf("stdout %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "Docker daemon running") {
		t.Errorf("stderr %q should explain how to fix the connection", stderr)
	}
}

func TestRunPassesConnectionFlags(t *testing.T) {
	var got Options
	connect := func(_ context.Context, opts Options) (*Services, error) {
		got = opts
		return nil, engine.ErrUnavailable
	}

	run(connect, "info", "--host", "tcp://10.0.0.5:2375", "--timeout", "2s", "--config", "/etc/orca.yaml")
	want := Options{Host: "tcp://10.0.0.5:2375", Timeout: 2 * time.Second, ConfigPath: "/etc/orca.yaml"}
	if got != want {
		t.Errorf("options = %+v, want %+v", got, want)
	}
}

func TestRunInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	connect := func(ctx context.Context, _ Options) (*Services, error) {
		return nil, ctx.Err()
	}

	var out, errOut bytes.Buffer
	if code := Run(ctx, []string{"info"}, &out, &errOut, connect); code != exitInterrupted {
		t.Errorf("exit %d, want %d", code, exitInterrupted)
	}
}
