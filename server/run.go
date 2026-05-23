package server

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

type runProcess struct {
	id        string
	profile   string
	cmdPath   string
	configRel string

	mu          sync.Mutex
	lines       []string
	done        bool
	exitErr     error
	startedAt   time.Time
	subscribers map[chan string]struct{}
}

type runData struct {
	Title       string
	Active      string
	Latest      *runProcess
	Configs     []string
	BinaryPath  string
}

func (s *Server) handleRunPage(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	latest := s.currentRun
	s.mu.Unlock()

	s.render(w, "run", runData{
		Title:      "Run",
		Active:     "run",
		Latest:     latest,
		Configs:    []string{"config.yaml", "config.weekend.yaml"},
		BinaryPath: s.BinaryPath,
	})
}

func (s *Server) handleRunStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	configName := r.FormValue("config")
	if configName == "" {
		http.Error(w, "missing config", http.StatusBadRequest)
		return
	}
	// Allowlist to prevent passing arbitrary paths.
	allowed := false
	for _, candidate := range []string{"config.yaml", "config.weekend.yaml"} {
		if candidate == configName {
			allowed = true
			break
		}
	}
	if !allowed {
		http.Error(w, "config not in allowlist", http.StatusBadRequest)
		return
	}

	mode := r.FormValue("mode")
	if mode == "" {
		mode = "dry-run"
	}
	var extraArgs []string
	var modeLabel string
	switch mode {
	case "dry-run":
		extraArgs = []string{"--ignore-release-window"}
		modeLabel = "dry-run"
	case "live-hunt":
		// Real money path: scan all eligible dates +14 → +3, override dry_run.
		extraArgs = []string{"--scan", "--ignore-release-window", "--live"}
		modeLabel = "LIVE HUNT"
	default:
		http.Error(w, "unknown mode (want dry-run or live-hunt)", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	if s.currentRun != nil && !s.currentRun.isDone() {
		s.mu.Unlock()
		http.Error(w, "a run is already in progress", http.StatusConflict)
		return
	}

	configPath := filepath.Join(s.ConfigDir, configName)
	if _, err := exec.LookPath(s.BinaryPath); err != nil {
		// Tolerate: BinaryPath may be absolute. exec.Cmd surfaces any error itself.
	}
	args := append([]string{"auto-book", "--config", configPath}, extraArgs...)
	cmd := exec.Command(s.BinaryPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.mu.Unlock()
		http.Error(w, "stdout pipe: "+err.Error(), http.StatusInternalServerError)
		return
	}
	cmd.Stderr = cmd.Stdout

	run := &runProcess{
		id:          fmt.Sprintf("run_%d", time.Now().UnixNano()),
		profile:     configName,
		cmdPath:     s.BinaryPath,
		configRel:   configName,
		startedAt:   time.Now(),
		subscribers: make(map[chan string]struct{}),
	}
	if err := cmd.Start(); err != nil {
		s.mu.Unlock()
		http.Error(w, "start: "+err.Error(), http.StatusInternalServerError)
		return
	}
	s.currentRun = run
	s.mu.Unlock()

	go s.pumpRunOutput(run, cmd, stdout)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<div id="run-status">Started <strong>%s</strong> run <span class="mono">%s</span> using <span class="mono">%s</span>. Streaming…</div>`, modeLabel, run.id, configName)
}

func (s *Server) pumpRunOutput(run *runProcess, cmd *exec.Cmd, stdout io.ReadCloser) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		run.appendLine(line)
	}
	err := cmd.Wait()
	run.finish(err)
}

func (run *runProcess) appendLine(line string) {
	run.mu.Lock()
	run.lines = append(run.lines, line)
	subs := make([]chan string, 0, len(run.subscribers))
	for ch := range run.subscribers {
		subs = append(subs, ch)
	}
	run.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- line:
		default:
			// Drop on backpressure — late readers will only see history.
		}
	}
}

func (run *runProcess) finish(err error) {
	run.mu.Lock()
	run.done = true
	run.exitErr = err
	subs := make([]chan string, 0, len(run.subscribers))
	for ch := range run.subscribers {
		subs = append(subs, ch)
	}
	run.mu.Unlock()
	for _, ch := range subs {
		close(ch)
	}
}

func (run *runProcess) isDone() bool {
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.done
}

// ID, Profile, and Active are exported method-form accessors so html/template
// can read the underlying fields safely without exposing the mutable internals.
func (run *runProcess) ID() string      { return run.id }
func (run *runProcess) Profile() string { return run.profile }
func (run *runProcess) Active() bool {
	run.mu.Lock()
	defer run.mu.Unlock()
	return !run.done
}

func (run *runProcess) subscribe() (history []string, ch chan string, done bool) {
	run.mu.Lock()
	defer run.mu.Unlock()
	history = append([]string(nil), run.lines...)
	if run.done {
		return history, nil, true
	}
	ch = make(chan string, 32)
	run.subscribers[ch] = struct{}{}
	return history, ch, false
}

func (run *runProcess) unsubscribe(ch chan string) {
	run.mu.Lock()
	defer run.mu.Unlock()
	delete(run.subscribers, ch)
}

func (s *Server) handleRunStream(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	run := s.currentRun
	s.mu.Unlock()
	if run == nil {
		http.Error(w, "no run yet", http.StatusNotFound)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	history, ch, alreadyDone := run.subscribe()
	for _, line := range history {
		fmt.Fprintf(w, "data: %s\n\n", line)
	}
	if alreadyDone {
		fmt.Fprintf(w, "event: done\ndata: finished\n\n")
		flusher.Flush()
		return
	}
	flusher.Flush()

	notify := r.Context().Done()
	defer run.unsubscribe(ch)
	for {
		select {
		case line, ok := <-ch:
			if !ok {
				fmt.Fprintf(w, "event: done\ndata: finished\n\n")
				flusher.Flush()
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", line)
			flusher.Flush()
		case <-notify:
			return
		}
	}
}
