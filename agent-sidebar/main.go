package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type appConfig struct {
	cwd           string
	sessionsDir   string
	width         int
	refreshEvery  time.Duration
	discoverEvery time.Duration
	doneFor       time.Duration
	staleAfter    time.Duration
	scanWindow    time.Duration
	once          bool
	plain         bool
}

func main() {
	config, err := parseFlags()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agent sidebar:", err)
		os.Exit(2)
	}

	repoRoot := findRepositoryRoot(config.cwd)
	collector := newCollector(collectorOptions{
		SessionsDir:   config.sessionsDir,
		RepoRoot:      repoRoot,
		DoneFor:       config.doneFor,
		StaleAfter:    config.staleAfter,
		ScanWindow:    config.scanWindow,
		DiscoverEvery: config.discoverEvery,
	})

	if config.once {
		agents, refreshErr := collector.refresh()
		fmt.Print(renderSidebar(agents, renderOptions{
			RepoRoot: repoRoot,
			Width:    config.width,
			Now:      time.Now(),
			Plain:    config.plain,
			Err:      refreshErr,
		}))
		if refreshErr != nil {
			os.Exit(1)
		}
		return
	}

	runInteractive(config, repoRoot, collector)
}

func parseFlags() (appConfig, error) {
	var config appConfig
	defaultCWD, err := os.Getwd()
	if err != nil {
		return config, err
	}

	defaultSessionsDir, err := codexSessionsDir()
	if err != nil {
		return config, err
	}

	flag.StringVar(&config.cwd, "cwd", defaultCWD, "working directory used to select the repository")
	flag.StringVar(&config.sessionsDir, "sessions-dir", defaultSessionsDir, "Codex sessions directory")
	flag.IntVar(&config.width, "width", 42, "render width in terminal cells")
	flag.DurationVar(&config.refreshEvery, "refresh", time.Second, "agent refresh interval")
	flag.DurationVar(&config.discoverEvery, "discover-every", 5*time.Second, "session discovery interval")
	flag.DurationVar(&config.doneFor, "done-for", 10*time.Minute, "how long completed agents remain visible")
	flag.DurationVar(&config.staleAfter, "stale-after", 30*time.Minute, "when an unfinished agent is marked stale")
	flag.DurationVar(&config.scanWindow, "scan-window", 48*time.Hour, "age window for discovering session files")
	flag.BoolVar(&config.once, "once", false, "render once and exit")
	flag.BoolVar(&config.plain, "plain", false, "disable ANSI styling")
	flag.Parse()

	absoluteCWD, err := filepath.Abs(config.cwd)
	if err != nil {
		return config, err
	}
	config.cwd = filepath.Clean(absoluteCWD)
	config.width = max(20, config.width)
	if config.refreshEvery <= 0 {
		return config, fmt.Errorf("refresh must be positive")
	}
	if config.discoverEvery <= 0 {
		return config, fmt.Errorf("discover-every must be positive")
	}

	return config, nil
}

func codexSessionsDir() (string, error) {
	if codexHome := os.Getenv("CODEX_HOME"); codexHome != "" {
		return filepath.Join(codexHome, "sessions"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "sessions"), nil
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

func runInteractive(config appConfig, repoRoot string, collector *collector) {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer cancel()

	restoreTerminal := enterCharacterMode()
	defer restoreTerminal()

	fmt.Print("\x1b[?1049h\x1b[?25l")
	defer fmt.Print("\x1b[?25h\x1b[?1049l")

	input := make(chan byte, 1)
	go readInput(input)

	refresh := make(chan struct{}, 1)
	refresh <- struct{}{}
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
				collector.forceDiscovery()
				select {
				case refresh <- struct{}{}:
				default:
				}
			}
		case <-ticker.C:
			select {
			case refresh <- struct{}{}:
			default:
			}
		case <-resize:
			width = terminalWidth(config.width)
			select {
			case refresh <- struct{}{}:
			default:
			}
		case <-refresh:
			agents, err := collector.refresh()
			output := renderSidebar(agents, renderOptions{
				RepoRoot: repoRoot,
				Width:    width,
				Now:      time.Now(),
				Err:      err,
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
