// SPDX-License-Identifier: Apache-2.0

// Package docker implements the sandbox.Sandbox interface against a
// local Docker daemon.
//
// Lifecycle of a single Run:
//
//  1. Ensure the image is present (ImageInspect -> ImagePull on miss).
//  2. ContainerCreate with the user's image + command + env, resource
//     limits derived from RunSpec.CPU + MemoryGB, network mode mapped
//     from RunSpec.Network. The container is started in detached mode.
//  3. ContainerStart, then ContainerLogs(Follow=true, ShowStdout=true,
//     ShowStderr=true) — stdcopy.StdCopy demultiplexes the multiplexed
//     attach stream into separate stdout/stderr byte buffers.
//  4. ContainerWait(WaitConditionNotRunning) blocks until the container
//     terminates. A peak-memory probe runs concurrently via
//     ContainerStats(stream=true) so we can record memory_peak_mb.
//  5. The captured stdout / stderr buffers are uploaded to object
//     storage at sandbox/runs/<id>/stdout.log + stderr.log; the
//     returned RunResult carries the storage keys (callers can mint
//     signed URLs as needed).
//  6. ContainerRemove(Force=true) cleans up regardless of exit status.
//
// Network mode handling
//
//	open       -> bridge (default)
//	restricted -> bridge with AllowEgress as a TODO note for the
//	              upstream iptables work (v1 keeps bridge for parity)
//	isolated   -> none
//
// The Phase 9.1 spec acknowledges the simplification: building a custom
// network with per-host iptables rules is a sizeable side-project; v1
// ships with the safe-by-default isolated mode honoured and the others
// documented.
package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
	"github.com/Agent-Field/backai/services/runtime/internal/storage"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// ErrAdapterNotImplemented preserves backward compatibility with the
// Phase 9.1 scaffold that surfaced this error before the real adapter
// landed. Live callers no longer see it; the gvisor adapter unit tests
// import it indirectly via the package.
var ErrAdapterNotImplemented = errors.New("sandbox/docker: not implemented")

// Config holds adapter settings.
//
// Runtime selects the OCI runtime Docker uses when creating containers.
// Empty = the daemon default (runc); "runsc" -> gVisor (the gvisor
// wrapper sets this).
type Config struct {
	// Host is the Docker daemon socket (e.g. "unix:///var/run/docker.sock"
	// or "tcp://docker:2375"). Empty = pick up DOCKER_HOST + DOCKER_CERT_PATH
	// from env via client.FromEnv.
	Host string

	// Runtime is the OCI runtime name passed in HostConfig.Runtime on
	// container create. "" -> daemon default; "runsc" -> gVisor.
	Runtime string

	// AdapterName overrides the label used in Capabilities().Adapter.
	// Defaults to "docker"; the gvisor wrapper sets "gvisor".
	AdapterName string

	// Storage is the object-storage backend used to persist
	// stdout/stderr after a run terminates. nil = the adapter still
	// captures logs in memory but skips the upload step (RunResult
	// URLs will be empty).
	Storage storage.Storage

	// Logger is the slog logger. nil -> slog.Default().
	Logger *slog.Logger
}

// Adapter implements sandbox.Sandbox against Docker.
type Adapter struct {
	cfg Config
	cli *client.Client
	log *slog.Logger

	// mu guards running.
	mu      sync.Mutex
	running map[string]string // run id -> container id
}

// compile-time interface check.
var _ sandbox.Sandbox = (*Adapter)(nil)

// New constructs the adapter. The Docker client connection is
// established lazily — New does NOT dial the daemon, so callers can
// construct an adapter on machines without Docker installed; the first
// Run call surfaces any connection error.
func New(cfg Config) (*Adapter, error) {
	if cfg.AdapterName == "" {
		cfg.AdapterName = "docker"
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	opts := []client.Opt{client.WithAPIVersionNegotiation()}
	if cfg.Host != "" {
		opts = append(opts, client.WithHost(cfg.Host))
	} else {
		opts = append(opts, client.FromEnv)
	}
	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("sandbox/docker: new client: %w", err)
	}
	return &Adapter{
		cfg:     cfg,
		cli:     cli,
		log:     log,
		running: map[string]string{},
	}, nil
}

// Config exposes the resolved config (mostly for adapter-wrapper tests
// like the gvisor adapter, which needs to assert Runtime="runsc" was
// set).
func (a *Adapter) Config() Config {
	return a.cfg
}

// Capabilities reports Docker-adapter limits. The gvisor wrapper
// overrides the Adapter label via Config.AdapterName.
func (a *Adapter) Capabilities() sandbox.Capabilities {
	return sandbox.Capabilities{
		Adapter:         a.cfg.AdapterName,
		MaxTimeoutS:     86400, // 24h
		SupportsGPU:     false, // Phase 9.1: not exposed (requires nvidia-container-runtime)
		SupportsNetwork: true,
		SupportsMounts:  true,
		ColdStartMS:     1000, // image cached; first pull is much higher
	}
}

// Ping verifies the Docker daemon is reachable. Used by main.go at
// startup to decide whether to enable the sandbox module at all.
func (a *Adapter) Ping(ctx context.Context) error {
	_, err := a.cli.Ping(ctx)
	return err
}

// ─── Run ─────────────────────────────────────────────────────────────────

// Run executes the spec to completion and returns the RunResult.
//
// On error (anything that prevents an attempt: image pull failure,
// container create failure, daemon unreachable) we return (nil, err).
// On a normal completion — including a container that exited non-zero
// — we return (*RunResult, nil) with Status set accordingly.
func (a *Adapter) Run(ctx context.Context, spec sandbox.RunSpec) (*sandbox.RunResult, error) {
	res := &sandbox.RunResult{}
	startedAt := time.Now().UTC()
	res.StartedAt = startedAt

	// Bound the whole run by the requested timeout. The adapter
	// converts a timeout into the Status="timeout" outcome rather than
	// surfacing context.DeadlineExceeded.
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(spec.TimeoutS)*time.Second)
	defer cancel()

	// Pull image if missing.
	if err := a.ensureImage(runCtx, spec.Image); err != nil {
		return nil, fmt.Errorf("ensure image: %w", err)
	}

	// Create container.
	containerID, err := a.createContainer(runCtx, spec)
	if err != nil {
		return nil, fmt.Errorf("container create: %w", err)
	}
	defer a.cleanup(containerID)
	a.trackRun(spec.ID, containerID)
	defer a.untrackRun(spec.ID)

	// Start container.
	if err := a.cli.ContainerStart(runCtx, containerID, container.StartOptions{}); err != nil {
		return nil, fmt.Errorf("container start: %w", err)
	}

	// Concurrent peak-memory probe via the streaming stats API. Stops
	// when the wait goroutine signals completion (statsDone).
	statsCtx, statsCancel := context.WithCancel(runCtx)
	defer statsCancel()
	memPeakCh := make(chan int, 1)
	netInCh := make(chan int64, 1)
	netOutCh := make(chan int64, 1)
	cpuSecCh := make(chan float64, 1)
	go a.observeStats(statsCtx, containerID, memPeakCh, netInCh, netOutCh, cpuSecCh)

	// Attach to logs (stdout + stderr multiplexed).
	logsReader, logsErr := a.cli.ContainerLogs(runCtx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if logsErr != nil {
		return nil, fmt.Errorf("container logs: %w", logsErr)
	}

	var (
		stdoutBuf bytes.Buffer
		stderrBuf bytes.Buffer
	)
	copyDone := make(chan error, 1)
	go func() {
		_, copyErr := stdcopy.StdCopy(&stdoutBuf, &stderrBuf, logsReader)
		_ = logsReader.Close()
		copyDone <- copyErr
	}()

	// Wait for terminal state.
	waitOK, waitErr := a.cli.ContainerWait(runCtx, containerID, container.WaitConditionNotRunning)
	timedOut := false
	exitCode := int64(0)
	select {
	case w := <-waitOK:
		if w.Error != nil {
			res.Error = errors.New(w.Error.Message)
		}
		exitCode = w.StatusCode
	case e := <-waitErr:
		// DeadlineExceeded from runCtx -> timeout. Anything else is an
		// adapter error (daemon went away). We still try to capture
		// whatever logs streamed before the failure.
		if errors.Is(e, context.DeadlineExceeded) || errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			timedOut = true
			_ = a.cli.ContainerStop(context.Background(), containerID, container.StopOptions{})
		} else {
			res.Error = e
		}
	}

	// Wait for the log copier to drain.
	statsCancel()
	<-copyDone

	// Final stats. We drain best-effort: if the channels haven't
	// produced anything yet, treat as zero.
	select {
	case v := <-memPeakCh:
		res.MemoryPeakMB = v
	default:
	}
	select {
	case v := <-netInCh:
		res.NetworkBytesIn = v
	default:
	}
	select {
	case v := <-netOutCh:
		res.NetworkBytesOut = v
	default:
	}
	select {
	case v := <-cpuSecCh:
		res.CPUSeconds = v
	default:
	}

	endedAt := time.Now().UTC()
	res.EndedAt = endedAt
	res.DurationS = int(endedAt.Sub(startedAt).Seconds() + 0.5)
	res.ExitCode = int(exitCode)

	switch {
	case timedOut:
		res.Status = sandbox.StatusTimeout
	case res.Error != nil:
		res.Status = sandbox.StatusFailed
	case exitCode == 0:
		res.Status = sandbox.StatusDone
	default:
		res.Status = sandbox.StatusFailed
	}

	// Persist logs.
	a.persistLogs(ctx, spec.ID, stdoutBuf.Bytes(), stderrBuf.Bytes(), res)

	return res, nil
}

// ensureImage pulls the image if a local copy is missing. We don't
// always pull (re-pull every call) — that would add 200ms+ per
// invocation even when the image hasn't changed.
func (a *Adapter) ensureImage(ctx context.Context, ref string) error {
	if _, err := a.cli.ImageInspect(ctx, ref); err == nil {
		return nil
	} else if !client.IsErrNotFound(err) {
		// Inspect failed for a non-NotFound reason — try the pull
		// anyway; the daemon will return a meaningful error there.
		a.log.Debug("docker: image inspect failed; trying pull", "image", ref, "error", err)
	}
	rc, err := a.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return err
	}
	defer rc.Close()
	// Drain the pull stream so the daemon completes the pull before we
	// continue. We don't surface progress events (Phase 9.1 keeps this
	// simple); just block until EOF or context cancellation.
	if _, err := io.Copy(io.Discard, rc); err != nil {
		return err
	}
	return nil
}

// createContainer builds the Config/HostConfig and asks the daemon for
// a new container id.
func (a *Adapter) createContainer(ctx context.Context, spec sandbox.RunSpec) (string, error) {
	env := make([]string, 0, len(spec.Env))
	for k, v := range spec.Env {
		env = append(env, k+"="+v)
	}

	cfg := &container.Config{
		Image: spec.Image,
		Cmd:   spec.Command,
		Env:   env,
	}

	netMode := container.NetworkMode(network.NetworkBridge)
	switch spec.Network {
	case sandbox.NetworkOpen, sandbox.NetworkRestricted:
		netMode = container.NetworkMode(network.NetworkBridge)
	case sandbox.NetworkIsolated:
		netMode = container.NetworkMode(network.NetworkNone)
	}

	hostCfg := &container.HostConfig{
		Resources: container.Resources{
			// Memory in bytes; MemoryGB validated at 1..64.
			Memory: int64(spec.MemoryGB) * 1024 * 1024 * 1024,
			// NanoCPUs is 1e9 per core; CPU validated at 1..32.
			NanoCPUs: int64(spec.CPU) * 1_000_000_000,
		},
		NetworkMode: netMode,
		AutoRemove:  false, // we remove explicitly in cleanup
	}
	if a.cfg.Runtime != "" {
		hostCfg.Runtime = a.cfg.Runtime
	}

	name := "" // let docker assign — we track via map keyed on RunSpec.ID
	if spec.ID != "" {
		name = "afsandbox-" + sanitizeName(spec.ID)
	}
	resp, err := a.cli.ContainerCreate(ctx, cfg, hostCfg, nil, nil, name)
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

// observeStats samples ContainerStats until ctx is cancelled and
// extracts the peak memory + total network bytes + CPU seconds. Sends
// the final values on the provided channels (which the caller drains
// once).
func (a *Adapter) observeStats(
	ctx context.Context,
	containerID string,
	memPeakCh chan<- int,
	netInCh, netOutCh chan<- int64,
	cpuSecCh chan<- float64,
) {
	rc, err := a.cli.ContainerStats(ctx, containerID, true)
	if err != nil {
		// Daemon went away; surface zeros.
		select {
		case memPeakCh <- 0:
		default:
		}
		return
	}
	defer rc.Body.Close()

	var (
		memPeakMB    int
		netIn        int64
		netOut       int64
		lastTotalCPU uint64
	)
	dec := json.NewDecoder(rc.Body)
	for {
		var stat container.StatsResponse
		if err := dec.Decode(&stat); err != nil {
			break
		}
		// Peak memory: MaxUsage is the high-water mark since container
		// start. Some kernels don't populate it — fall back to Usage.
		mem := stat.MemoryStats.MaxUsage
		if mem == 0 {
			mem = stat.MemoryStats.Usage
		}
		mbi := int(mem / (1024 * 1024))
		if mbi > memPeakMB {
			memPeakMB = mbi
		}
		// Network bytes — sum across all interfaces.
		var rx, tx uint64
		for _, n := range stat.Networks {
			rx += n.RxBytes
			tx += n.TxBytes
		}
		if int64(rx) > netIn {
			netIn = int64(rx)
		}
		if int64(tx) > netOut {
			netOut = int64(tx)
		}
		lastTotalCPU = stat.CPUStats.CPUUsage.TotalUsage
	}

	// Convert CPU nanoseconds to seconds (CPUUsage.TotalUsage is total
	// across all cores).
	cpuSec := float64(lastTotalCPU) / 1e9

	select {
	case memPeakCh <- memPeakMB:
	default:
	}
	select {
	case netInCh <- netIn:
	default:
	}
	select {
	case netOutCh <- netOut:
	default:
	}
	select {
	case cpuSecCh <- cpuSec:
	default:
	}
}

// persistLogs uploads stdout + stderr to object storage and stamps the
// resulting keys onto the RunResult.
func (a *Adapter) persistLogs(ctx context.Context, runID string, stdout, stderr []byte, res *sandbox.RunResult) {
	if a.cfg.Storage == nil || runID == "" {
		return
	}
	uploadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if obj, err := a.cfg.Storage.Upload(uploadCtx,
		"sandbox/runs/"+runID+"/stdout.log",
		bytes.NewReader(stdout),
		storage.UploadOpts{ContentType: "text/plain"},
	); err == nil && obj != nil {
		res.StdoutURL = obj.Key
	} else if err != nil {
		a.log.Warn("docker: stdout upload failed", "run_id", runID, "error", err)
	}
	if obj, err := a.cfg.Storage.Upload(uploadCtx,
		"sandbox/runs/"+runID+"/stderr.log",
		bytes.NewReader(stderr),
		storage.UploadOpts{ContentType: "text/plain"},
	); err == nil && obj != nil {
		res.StderrURL = obj.Key
	} else if err != nil {
		a.log.Warn("docker: stderr upload failed", "run_id", runID, "error", err)
	}
}

// cleanup removes the container regardless of exit status. Uses a
// short background context so an already-cancelled run still gets its
// container reaped.
func (a *Adapter) cleanup(containerID string) {
	if containerID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force: true,
	}); err != nil && !client.IsErrNotFound(err) {
		a.log.Warn("docker: cleanup failed", "container", containerID, "error", err)
	}
}

// trackRun + untrackRun maintain the run id -> container id map used by
// Stop to find the right container without re-asking the daemon.
func (a *Adapter) trackRun(runID, containerID string) {
	if runID == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.running[runID] = containerID
}

func (a *Adapter) untrackRun(runID string) {
	if runID == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.running, runID)
}

// Stop signals a running container to terminate. id is RunSpec.ID.
func (a *Adapter) Stop(ctx context.Context, runID string) error {
	a.mu.Lock()
	containerID, ok := a.running[runID]
	a.mu.Unlock()
	if !ok {
		return sandbox.ErrNotFound
	}
	timeout := 5
	return a.cli.ContainerStop(ctx, containerID, container.StopOptions{
		Timeout: &timeout,
	})
}

// ─── Stream ──────────────────────────────────────────────────────────────

// Stream runs the spec and emits LogLines on the returned channel as
// the container produces output. The final RunResult is delivered on
// the result channel (one value, then closed).
//
// We implement Stream as a thin wrapper around Run + a tee of the
// container's log stream. The implementation reuses the same flow as
// Run but tees stdout/stderr through a line-splitting goroutine.
func (a *Adapter) Stream(ctx context.Context, spec sandbox.RunSpec) (<-chan sandbox.LogLine, <-chan *sandbox.RunResult, error) {
	linesCh := make(chan sandbox.LogLine, 256)
	resultCh := make(chan *sandbox.RunResult, 1)

	startedAt := time.Now().UTC()
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(spec.TimeoutS)*time.Second)

	if err := a.ensureImage(runCtx, spec.Image); err != nil {
		cancel()
		close(linesCh)
		return nil, nil, fmt.Errorf("ensure image: %w", err)
	}
	containerID, err := a.createContainer(runCtx, spec)
	if err != nil {
		cancel()
		close(linesCh)
		return nil, nil, fmt.Errorf("container create: %w", err)
	}
	a.trackRun(spec.ID, containerID)

	if err := a.cli.ContainerStart(runCtx, containerID, container.StartOptions{}); err != nil {
		cancel()
		close(linesCh)
		a.untrackRun(spec.ID)
		a.cleanup(containerID)
		return nil, nil, fmt.Errorf("container start: %w", err)
	}

	// Background: drive Run-like lifecycle and emit lines.
	go func() {
		defer cancel()
		defer close(linesCh)
		defer close(resultCh)
		defer a.untrackRun(spec.ID)
		defer a.cleanup(containerID)

		logsReader, err := a.cli.ContainerLogs(runCtx, containerID, container.LogsOptions{
			ShowStdout: true, ShowStderr: true, Follow: true,
		})
		if err != nil {
			resultCh <- &sandbox.RunResult{Status: sandbox.StatusFailed, Error: err}
			return
		}
		stdoutPipeR, stdoutPipeW := io.Pipe()
		stderrPipeR, stderrPipeW := io.Pipe()
		copyDone := make(chan struct{})
		go func() {
			_, _ = stdcopy.StdCopy(stdoutPipeW, stderrPipeW, logsReader)
			_ = stdoutPipeW.Close()
			_ = stderrPipeW.Close()
			_ = logsReader.Close()
			close(copyDone)
		}()
		var (
			stdoutBuf bytes.Buffer
			stderrBuf bytes.Buffer
			wg        sync.WaitGroup
		)
		wg.Add(2)
		go func() {
			defer wg.Done()
			sc := bufio.NewScanner(io.TeeReader(stdoutPipeR, &stdoutBuf))
			sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
			for sc.Scan() {
				linesCh <- sandbox.LogLine{TS: time.Now().UTC(), Stream: "stdout", Text: sc.Text()}
			}
		}()
		go func() {
			defer wg.Done()
			sc := bufio.NewScanner(io.TeeReader(stderrPipeR, &stderrBuf))
			sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
			for sc.Scan() {
				linesCh <- sandbox.LogLine{TS: time.Now().UTC(), Stream: "stderr", Text: sc.Text()}
			}
		}()

		waitOK, waitErr := a.cli.ContainerWait(runCtx, containerID, container.WaitConditionNotRunning)
		exitCode := int64(0)
		status := sandbox.StatusDone
		var runErr error
		select {
		case w := <-waitOK:
			exitCode = w.StatusCode
			if w.Error != nil {
				runErr = errors.New(w.Error.Message)
				status = sandbox.StatusFailed
			} else if exitCode != 0 {
				status = sandbox.StatusFailed
			}
		case e := <-waitErr:
			if errors.Is(e, context.DeadlineExceeded) || errors.Is(runCtx.Err(), context.DeadlineExceeded) {
				status = sandbox.StatusTimeout
				_ = a.cli.ContainerStop(context.Background(), containerID, container.StopOptions{})
			} else {
				runErr = e
				status = sandbox.StatusFailed
			}
		}

		<-copyDone
		wg.Wait()

		endedAt := time.Now().UTC()
		res := &sandbox.RunResult{
			Status:    status,
			ExitCode:  int(exitCode),
			DurationS: int(endedAt.Sub(startedAt).Seconds() + 0.5),
			StartedAt: startedAt,
			EndedAt:   endedAt,
			Error:     runErr,
		}
		a.persistLogs(ctx, spec.ID, stdoutBuf.Bytes(), stderrBuf.Bytes(), res)
		resultCh <- res
	}()

	return linesCh, resultCh, nil
}

// sanitizeName scrubs a string to docker-name-safe ([a-zA-Z0-9_.-]).
func sanitizeName(s string) string {
	b := strings.Builder{}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	if b.Len() > 64 {
		return b.String()[:64]
	}
	return b.String()
}