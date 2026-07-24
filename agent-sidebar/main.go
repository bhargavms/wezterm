package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type appConfig struct {
	wezterm      string
	paneID       int
	width        int
	refreshEvery time.Duration
	once         bool
	plain        bool
}

type refreshResult struct {
	sessions []codexSession
	err      error
}

func main() {
	config, err := parseFlags()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agent sidebar:", err)
		os.Exit(2)
	}

	collector := newWindowCollector(windowCollectorOptions{
		Execute:       executeCommand,
		WezTerm:       config.wezterm,
		SidebarPaneID: config.paneID,
	})

	if config.once {
		sessions, refreshErr := collector.refresh(context.Background())
		fmt.Print(renderSidebar(sessions, renderOptions{
			Scope: "this window",
			Width: config.width,
			Plain: config.plain,
			Err:   refreshErr,
		}))
		if refreshErr != nil {
			os.Exit(1)
		}
		return
	}

	runInteractive(config, collector)
}

func parseFlags() (appConfig, error) {
	var config appConfig
	defaultPaneID, err := environmentPaneID()
	if err != nil {
		return config, err
	}

	flag.StringVar(&config.wezterm, "wezterm", weztermExecutable(), "WezTerm CLI executable")
	flag.IntVar(&config.paneID, "pane-id", defaultPaneID, "sidebar pane id used to select the current window")
	flag.IntVar(&config.width, "width", 42, "render width in terminal cells")
	flag.DurationVar(&config.refreshEvery, "refresh", 2*time.Second, "window refresh interval")
	flag.BoolVar(&config.once, "once", false, "render once and exit")
	flag.BoolVar(&config.plain, "plain", false, "disable ANSI styling")
	flag.Parse()

	config.width = max(20, config.width)
	if config.refreshEvery <= 0 {
		return config, fmt.Errorf("refresh must be positive")
	}

	return config, nil
}

func environmentPaneID() (int, error) {
	value := os.Getenv("WEZTERM_PANE")
	if value == "" {
		return -1, nil
	}
	paneID, err := strconv.Atoi(value)
	if err != nil {
		return -1, fmt.Errorf("invalid WEZTERM_PANE %q", value)
	}
	return paneID, nil
}

func weztermExecutable() string {
	if directory := os.Getenv("WEZTERM_EXECUTABLE_DIR"); directory != "" {
		candidate := filepath.Join(directory, "wezterm")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if executable, err := exec.LookPath("wezterm"); err == nil {
		return executable
	}
	return "/Applications/WezTerm.app/Contents/MacOS/wezterm"
}

func findRepositoryRoot(start string) string {
	root := canonicalPath(start)
	current := root
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current
		}

		parent := filepath.Dir(current)
		if parent == current {
			return root
		}
		current = parent
	}
}

func canonicalPath(path string) string {
	cleaned := filepath.Clean(path)
	for existing := cleaned; ; existing = filepath.Dir(existing) {
		resolved, err := filepath.EvalSymlinks(existing)
		if err == nil {
			remainder, relativeErr := filepath.Rel(existing, cleaned)
			if relativeErr == nil {
				return filepath.Clean(filepath.Join(resolved, remainder))
			}
			break
		}
		if parent := filepath.Dir(existing); parent == existing {
			break
		}
	}
	return cleaned
}

func runInteractive(config appConfig, collector *windowCollector) {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()

	restoreTerminal := enterCharacterMode()
	defer restoreTerminal()

	fmt.Print("\x1b[?1049h\x1b[?25l")
	defer fmt.Print("\x1b[?25h\x1b[?1049l")

	input := make(chan byte, 1)
	go readInput(input)

	results := make(chan refreshResult, 1)
	refreshing := false
	requestRefresh := func() {
		if refreshing {
			return
		}
		refreshing = true
		go func() {
			sessions, err := collector.refresh(ctx)
			select {
			case results <- refreshResult{sessions: sessions, err: err}:
			case <-ctx.Done():
			}
		}()
	}
	requestRefresh()
	ticker := time.NewTicker(config.refreshEvery)
	defer ticker.Stop()

	width := terminalWidth(config.width)
	resize := make(chan os.Signal, 1)
	signal.Notify(resize, syscall.SIGWINCH)
	defer signal.Stop(resize)

	for {
		select {
		case <-ctx.Done():
			return
		case key := <-input:
			switch key {
			case 'q', 3, 27:
				return
			case 'r':
				requestRefresh()
			}
		case <-ticker.C:
			requestRefresh()
		case <-resize:
			width = terminalWidth(config.width)
			requestRefresh()
		case result := <-results:
			refreshing = false
			output := renderSidebar(result.sessions, renderOptions{
				Scope: "this window",
				Width: width,
				Err:   result.err,
			})
			fmt.Print("\x1b[H\x1b[2J" + output)
		}
	}
}

func enterCharacterMode() func() {
	query := exec.Command("/bin/stty", "-g")
	query.Stdin = os.Stdin
	original, err := query.Output()
	if err != nil {
		return func() {}
	}

	command := exec.Command("/bin/stty", "-echo", "-icanon", "min", "1", "time", "0")
	command.Stdin = os.Stdin
	if err := command.Run(); err != nil {
		return func() {}
	}

	state := strings.TrimSpace(string(original))
	return func() {
		restore := exec.Command("/bin/stty", state)
		restore.Stdin = os.Stdin
		_ = restore.Run()
	}
}

func terminalWidth(fallback int) int {
	command := exec.Command("/bin/stty", "size")
	command.Stdin = os.Stdin
	output, err := command.Output()
	if err != nil {
		return max(20, fallback)
	}

	var rows, columns int
	if _, err := fmt.Sscanf(string(output), "%d %d", &rows, &columns); err != nil || columns <= 0 {
		return max(20, fallback)
	}
	return max(20, columns)
}

func readInput(output chan<- byte) {
	buffer := make([]byte, 1)
	for {
		if _, err := os.Stdin.Read(buffer); err != nil {
			return
		}
		output <- buffer[0]
	}
}
