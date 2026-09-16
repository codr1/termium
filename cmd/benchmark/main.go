// Run from the repository root after npm run build. This uses the shipped
// client pipeline and its normal private Unix socket, not a parallel renderer.
package main

import (
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"termium/internal/perf"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

//go:embed fixture.html
var fixture string

type options struct {
	Label, Renderer, Format, Palette, Scenes, Out, Note string
	Duration, Warmup                                    time.Duration
	Repeats, Rows, Cols, TerminalPID                    int
	Display                                             bool
}
type resourcePoint struct {
	At        time.Time       `json:"at"`
	Processes []processSample `json:"processes"`
}
type runResult struct {
	Scene           string                     `json:"scene"`
	Repeat          int                        `json:"repeat"`
	Client          perf.Client                `json:"client"`
	Resources       map[string]resourceSummary `json:"resources"`
	ResourceSeconds float64                    `json:"resource_seconds"`
	Samples         []resourcePoint            `json:"resource_samples"`
}
type report struct {
	Schema      int               `json:"schema"`
	Complete    bool              `json:"complete"`
	Created     time.Time         `json:"created"`
	Options     options           `json:"options"`
	Environment map[string]string `json:"environment"`
	Runs        []runResult       `json:"runs"`
}

func main() {
	o := options{}
	flag.StringVar(&o.Label, "label", "baseline", "Label saved with these results")
	flag.StringVar(&o.Renderer, "renderer", "both", "kitty, sixel, or both")
	flag.StringVar(&o.Format, "capture-format", "auto", "auto, png, or jpeg")
	flag.StringVar(&o.Palette, "palette", "websafe", "websafe, plan9, or adaptive")
	flag.StringVar(&o.Scenes, "scenes", "idle,patch,scroll,canvas", "Comma-separated local workloads")
	flag.StringVar(&o.Out, "out", "", "New output directory (default: dist/performance/<timestamp>-<label>)")
	flag.StringVar(&o.Note, "note", "", "Power mode, terminal version, GPU, scaling, or other conditions")
	flag.DurationVar(&o.Duration, "duration", 30*time.Second, "Measured time per run")
	flag.DurationVar(&o.Warmup, "warmup", 5*time.Second, "Warmup after page and first frame are ready")
	flag.IntVar(&o.Repeats, "repeats", 3, "Repeats, reversing configuration order on alternate repeats")
	flag.IntVar(&o.Rows, "rows", 49, "PTY rows (16-pixel default cells)")
	flag.IntVar(&o.Cols, "cols", 162, "PTY columns (8-pixel default cells)")
	flag.IntVar(&o.TerminalPID, "terminal-pid", 0, "Optional real terminal process PID for CPU/RSS sampling")
	flag.BoolVar(&o.Display, "display", false, "Use this real terminal instead of a drained PTY")
	compare := flag.Bool("compare", false, "Compare two result.json files supplied as positional arguments")
	flag.Parse()
	var err error
	if *compare {
		err = compareReports(flag.Args())
	} else {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		err = run(ctx, o)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "benchmark:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, o options) error {
	if o.Duration < time.Second || o.Duration > 10*time.Minute || o.Warmup < 0 || o.Repeats < 1 || o.Repeats > 20 || o.Rows < 8 || o.Rows > 256 || o.Cols < 20 || o.Cols > 512 {
		return fmt.Errorf("invalid duration, warmup, repeat count, or terminal dimensions")
	}
	if o.Renderer != "both" && o.Renderer != "kitty" && o.Renderer != "sixel" {
		return fmt.Errorf("renderer must be kitty, sixel, or both")
	}
	if o.Format != "auto" && o.Format != "png" && o.Format != "jpeg" {
		return fmt.Errorf("invalid capture format")
	}
	if o.Palette != "websafe" && o.Palette != "plan9" && o.Palette != "adaptive" {
		return fmt.Errorf("invalid palette")
	}
	scenes := strings.Split(o.Scenes, ",")
	for _, scene := range scenes {
		if scene != "idle" && scene != "patch" && scene != "scroll" && scene != "canvas" {
			return fmt.Errorf("unknown scene %q", scene)
		}
	}
	if o.Display && (!term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd()))) {
		return fmt.Errorf("--display requires a real terminal")
	}
	if o.TerminalPID != 0 && (!o.Display || o.TerminalPID < 2) {
		return fmt.Errorf("--terminal-pid requires --display and a valid PID")
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	binary := filepath.Join(root, "client/termium")
	info, err := buildinfo.ReadFile(binary)
	if err != nil {
		return fmt.Errorf("build client first: %w", err)
	}
	for _, s := range info.Settings {
		if s.Key == "-race" && s.Value == "true" {
			return fmt.Errorf("race build detected; run npm run build:client before benchmarking")
		}
	}
	if o.Out == "" {
		o.Out = filepath.Join("dist/performance", time.Now().UTC().Format("20060102T150405.000000000Z")+"-"+safeLabel(o.Label))
	}
	o.Out, err = filepath.Abs(o.Out)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(o.Out), 0755); err != nil {
		return err
	}
	if err = os.Mkdir(o.Out, 0755); err != nil {
		return fmt.Errorf("output directory must be new: %w", err)
	}
	env, err := environment(ctx, root, binary)
	if err != nil {
		return err
	}
	r := report{Schema: perf.Schema, Created: time.Now().UTC(), Options: o, Environment: env}
	renderers := []string{o.Renderer}
	if o.Renderer == "both" {
		renderers = []string{"sixel", "kitty"}
	}
	for repeat := 0; repeat < o.Repeats; repeat++ {
		for _, scene := range scenes {
			for index := range renderers {
				i := index
				if repeat%2 == 1 {
					i = len(renderers) - 1 - index
				}
				renderer := renderers[i]
				fmt.Printf("%s / %s / repeat %d: warmup %s + measure %s\n", scene, renderer, repeat+1, o.Warmup, o.Duration)
				result, err := runOne(ctx, root, o, scene, renderer, repeat+1, env["chromium_path"])
				if err != nil {
					return fmt.Errorf("%s/%s: %w (artifacts: %s)", scene, renderer, err, o.Out)
				}
				r.Runs = append(r.Runs, result)
				if err := perf.WriteJSON(filepath.Join(o.Out, "result.json"), r); err != nil {
					return err
				}
				printRun(result)
			}
		}
	}
	r.Complete = true
	if err := perf.WriteJSON(filepath.Join(o.Out, "result.json"), r); err != nil {
		return err
	}
	fmt.Println("Saved:", filepath.Join(o.Out, "result.json"))
	return nil
}

func safeLabel(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, s)
}

func commandText(ctx context.Context, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", name, err, data)
	}
	return strings.TrimSpace(string(data)), nil
}

func environment(ctx context.Context, root, binary string) (map[string]string, error) {
	env := map[string]string{"os": runtime.GOOS, "arch": runtime.GOARCH, "terminal": os.Getenv("TERM"), "terminal_program": os.Getenv("TERM_PROGRAM"), "fixture_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(fixture)))}
	env["cpu_accounting"] = "ps-fractional-seconds"
	if runtime.GOOS == "linux" {
		ticks, err := clockTicks(ctx)
		if err != nil {
			return nil, err
		}
		env["cpu_accounting"] = fmt.Sprintf("proc-stat ticks/s=%g; ps fallback for exited processes", ticks)
	}
	for key, args := range map[string][]string{"revision": {"git", "rev-parse", "HEAD"}, "worktree": {"git", "status", "--porcelain"}, "node": {"node", "--version"}, "os_release": {"uname", "-sr"}} {
		value, err := commandText(ctx, args[0], args[1:]...)
		if err != nil {
			return nil, err
		}
		env[key] = value
	}
	for key, file := range map[string]string{"client_sha256": binary, "server_sha256": filepath.Join(root, "server/dist/src/server.js"), "lock_sha256": filepath.Join(root, "package-lock.json")} {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		env[key] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	diff, err := commandText(ctx, "git", "diff", "--binary", "HEAD")
	if err != nil {
		return nil, err
	}
	env["diff_sha256"] = fmt.Sprintf("%x", sha256.Sum256([]byte(diff)))
	info, err := buildinfo.ReadFile(binary)
	if err != nil {
		return nil, err
	}
	env["go"] = info.GoVersion
	if runtime.GOOS == "darwin" {
		env["cpu"], _ = commandText(ctx, "sysctl", "-n", "machdep.cpu.brand_string")
	} else {
		data, _ := os.ReadFile("/proc/cpuinfo")
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "model name") {
				_, env["cpu"], _ = strings.Cut(line, ":")
				env["cpu"] = strings.TrimSpace(env["cpu"])
				break
			}
		}
	}
	chrome, err := commandText(ctx, "node", "--input-type=module", "-e", "import {executablePath} from 'puppeteer'; process.stdout.write(await executablePath())")
	if err != nil {
		return nil, err
	}
	env["chromium"], err = commandText(ctx, chrome, "--version")
	if err != nil {
		return nil, err
	}
	env["chromium_path"] = chrome
	return env, nil
}

func runOne(ctx context.Context, root string, o options, scene, renderer string, repeat int, chrome string) (result runResult, retErr error) {
	dir := filepath.Join(o.Out, fmt.Sprintf("%s-%s-%d", scene, renderer, repeat))
	if err := os.Mkdir(dir, 0755); err != nil {
		return result, err
	}
	ready := make(chan struct{})
	var once sync.Once
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		io.WriteString(w, fixture)
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) { once.Do(func() { close(ready) }); w.WriteHeader(204) })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return result, err
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go server.Serve(listener)
	defer server.Close()
	metricPath := filepath.Join(dir, "client.json")
	// Record the actual browser process before exec. Chromium has its own
	// process group, which must also be reclaimed if client shutdown fails.
	pidFile := filepath.Join(dir, "browser.pid")
	wrapper := filepath.Join(dir, "chrome.sh")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	script := "#!/bin/sh\nprintf '%s\\n' \"$$\" >> " + quote(pidFile) + "\nexec " + quote(chrome) + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		return result, err
	}
	defer func() {
		data, err := os.ReadFile(pidFile)
		if os.IsNotExist(err) {
			return
		}
		if err != nil {
			if retErr == nil {
				retErr = err
			}
			return
		}
		for _, field := range strings.Fields(string(data)) {
			var pid int
			if _, err := fmt.Sscan(field, &pid); err != nil || pid <= 1 {
				continue
			}
			if syscall.Kill(pid, 0) == syscall.ESRCH {
				continue
			}
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			if retErr == nil {
				retErr = fmt.Errorf("browser process %d survived client shutdown", pid)
			}
		}
	}()
	log, err := os.Create(filepath.Join(dir, "client.log"))
	if err != nil {
		return result, err
	}
	defer log.Close()
	cmd := exec.Command(filepath.Join(root, "client/termium"), "--renderer", renderer, "--capture-format", o.Format, "--palette", o.Palette, "--splash", "NONE", "--homepage", "about:blank", "--url", "http://"+listener.Addr().String()+"/?scene="+scene)
	// Keep the checkout's server and fresh browser profile, regardless of a
	// user's installed runtime overrides. The client still owns normal launch.
	cmd.Env = append(os.Environ(), "TERMIUM_SERVER="+filepath.Join(root, "server/dist/src/server.js"), "TERMIUM_PERF_REPORT="+metricPath, "PUPPETEER_TMP_DIR="+dir, "PUPPETEER_EXECUTABLE_PATH="+wrapper)
	cmd.Stderr = log
	var terminal *os.File
	if o.Display {
		state, stateErr := term.GetState(int(os.Stdin.Fd()))
		if stateErr != nil {
			return result, stateErr
		}
		defer term.Restore(int(os.Stdin.Fd()), state)
		cmd.Stdin, cmd.Stdout = os.Stdin, os.Stdout
		// Inherit the foreground process group so terminal reads cannot stop
		// the client with SIGTTIN. The runner never kills this shared group.
		err = cmd.Start()
	} else {
		cmd.Env = append(cmd.Env, "TERM=xterm-256color", "TERM_PROGRAM=")
		terminal, err = pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(o.Rows), Cols: uint16(o.Cols), X: uint16(o.Cols * 8), Y: uint16(o.Rows * 16)})
	}
	if err != nil {
		return result, err
	}
	if terminal != nil {
		defer terminal.Close()
		// Drain continuously without buffering graphics or decoding them. This
		// is a throughput sink, not a simulated graphics renderer.
		go io.Copy(io.Discard, terminal)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	exited := false
	defer func() {
		if exited {
			return
		}
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case err := <-done:
			if err != nil && retErr == nil {
				retErr = fmt.Errorf("client shutdown: %w", err)
			}
		case <-time.After(8 * time.Second):
			if o.Display {
				if all, err := processSnapshot(context.Background()); err == nil {
					owned := newTracker(cmd.Process.Pid, 0).sample(all, true)
					for _, p := range owned {
						_ = syscall.Kill(p.PID, syscall.SIGKILL)
					}
				}
				_ = cmd.Process.Kill()
			} else {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
			<-done
			if retErr == nil {
				retErr = fmt.Errorf("client shutdown timed out")
			}
		}
	}()
	wait := func(until time.Time, predicate func() bool) error {
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for !predicate() {
			if time.Now().After(until) {
				return fmt.Errorf("readiness/report deadline exceeded")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case err := <-done:
				exited = true
				return fmt.Errorf("client exited before report: %v", err)
			case <-tick.C:
			}
		}
		return nil
	}
	exists := func(name string) bool { _, err := os.Stat(name); return err == nil }
	if err = wait(time.Now().Add(45*time.Second), func() bool {
		select {
		case <-ready:
			return exists(metricPath + ".ready")
		default:
			return false
		}
	}); err != nil {
		return result, err
	}
	warmEnd := time.Now().Add(o.Warmup)
	if err = wait(warmEnd.Add(time.Second), func() bool { return !time.Now().Before(warmEnd) }); err != nil {
		return result, err
	}
	start := time.Now().Add(300 * time.Millisecond)
	if err = perf.WriteJSON(metricPath+".start", perf.Control{Start: start, Duration: o.Duration}); err != nil {
		return result, err
	}
	if err = wait(start.Add(time.Second), func() bool { return !time.Now().Before(start) }); err != nil {
		return result, err
	}
	tracker := newTracker(cmd.Process.Pid, o.TerminalPID)
	result = runResult{Scene: scene, Repeat: repeat}
	sample := func(first bool) error {
		all, err := processSnapshot(ctx)
		if err != nil {
			return err
		}
		result.Samples = append(result.Samples, resourcePoint{At: time.Now(), Processes: tracker.sample(all, first)})
		return nil
	}
	if err = sample(true); err != nil {
		return result, err
	}
	if o.TerminalPID > 0 {
		found := false
		for _, s := range result.Samples[0].Processes {
			found = found || s.PID == o.TerminalPID
		}
		if !found {
			return result, fmt.Errorf("terminal PID not found")
		}
	}
	for !exists(metricPath) {
		next := time.Now().Add(max(50*time.Millisecond, min(time.Second, time.Until(start.Add(o.Duration)))))
		if err = wait(start.Add(o.Duration+5*time.Second), func() bool { return exists(metricPath) || !time.Now().Before(next) }); err != nil {
			return result, err
		}
		if err = sample(false); err != nil {
			return result, err
		}
		if time.Now().After(start.Add(o.Duration + 5*time.Second)) {
			return result, fmt.Errorf("client did not publish metrics")
		}
	}
	data, err := os.ReadFile(metricPath)
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(data, &result.Client); err != nil {
		return result, err
	}
	if result.Client.Schema != perf.Schema || result.Client.Counts["capture"] == 0 {
		return result, fmt.Errorf("no measured captures or incompatible report")
	}
	if scene != "idle" && result.Client.Counts["write"] == 0 {
		return result, fmt.Errorf("animated scene produced no frame writes")
	}
	result.ResourceSeconds = result.Samples[len(result.Samples)-1].At.Sub(result.Samples[0].At).Seconds()
	result.Resources = tracker.summary(result.ResourceSeconds)
	return result, nil
}

func printRun(r runResult) {
	c := r.Client
	var cpu float64
	for role, s := range r.Resources {
		if role != "terminal" {
			cpu += s.CPUPercent
		}
	}
	fmt.Printf("  %.2f writes/s, %.2f captures/s; capture p95 %.1f ms, prepare p95 %.1f ms; CPU %.0f%%; %.1f MiB/s Go allocations\n", float64(c.Counts["write"])/c.Seconds, float64(c.Counts["capture"])/c.Seconds, c.Latency["capture"].P95, c.Latency["prepare"].P95, cpu, float64(c.Memory.AllocatedBytes)/c.Seconds/(1024*1024))
}
