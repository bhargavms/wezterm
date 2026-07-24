package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxConcurrentPaneReads = 8
	defaultCommandTimeout  = 3 * time.Second
	paneHistoryLines       = 500
)

type commandExecutor func(context.Context, string, ...string) ([]byte, error)

type windowCollectorOptions struct {
	Execute        commandExecutor
	WezTerm        string
	TargetWindowID int
	SourcePaneID   int
	CommandTimeout time.Duration
}

type windowCollector struct {
	options windowCollectorOptions
}

type paneSnapshot struct {
	WindowID int    `json:"window_id"`
	TabID    int    `json:"tab_id"`
	PaneID   int    `json:"pane_id"`
	CWD      string `json:"cwd"`
	TTYName  string `json:"tty_name"`
}

func newWindowCollector(options windowCollectorOptions) *windowCollector {
	if options.Execute == nil {
		options.Execute = executeCommand
	}
	if options.CommandTimeout <= 0 {
		options.CommandTimeout = defaultCommandTimeout
	}
	return &windowCollector{options: options}
}

func executeCommand(ctx context.Context, name string, arguments ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, arguments...).Output()
}

func (collector *windowCollector) execute(parent context.Context, name string, arguments ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, collector.options.CommandTimeout)
	defer cancel()
	return collector.options.Execute(ctx, name, arguments...)
}

func (collector *windowCollector) refresh(ctx context.Context) ([]codexSession, error) {
	paneJSON, err := collector.execute(ctx, collector.options.WezTerm, "cli", "list", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("list WezTerm panes: %w", err)
	}

	var panes []paneSnapshot
	if err := json.Unmarshal(paneJSON, &panes); err != nil {
		return nil, fmt.Errorf("decode WezTerm panes: %w", err)
	}

	windowID, found := resolveTargetWindow(panes, collector.options.TargetWindowID, collector.options.SourcePaneID)
	if !found {
		return nil, fmt.Errorf(
			"target window %d for source pane %d is not present in WezTerm",
			collector.options.TargetWindowID,
			collector.options.SourcePaneID,
		)
	}

	processes, err := collector.execute(ctx, "ps", "-axo", "tty=,comm=,args=")
	if err != nil {
		return nil, fmt.Errorf("list terminal processes: %w", err)
	}
	codexTTYs := codexTerminalSet(string(processes))

	tabPositions := windowTabPositions(panes, windowID)
	codexPanes := make([]paneSnapshot, 0)
	for _, pane := range panes {
		if pane.WindowID != windowID {
			continue
		}
		if _, running := codexTTYs[normalizeTTY(pane.TTYName)]; !running {
			continue
		}
		codexPanes = append(codexPanes, pane)
	}

	summaries := make([]string, len(codexPanes))
	semaphore := make(chan struct{}, maxConcurrentPaneReads)
	var waitGroup sync.WaitGroup
	var readErrors []error
	var readErrorsMutex sync.Mutex
	for index, pane := range codexPanes {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			text, textErr := collector.execute(
				ctx,
				collector.options.WezTerm,
				"cli",
				"get-text",
				"--pane-id",
				strconv.Itoa(pane.PaneID),
				"--start-line",
				strconv.Itoa(-paneHistoryLines),
			)
			summaries[index] = "Codex is running"
			if textErr != nil {
				readErrorsMutex.Lock()
				readErrors = append(readErrors, fmt.Errorf("read pane %d: %w", pane.PaneID, textErr))
				readErrorsMutex.Unlock()
				return
			}
			if current := summarizePaneActivity(string(text)); current != "" {
				summaries[index] = current
			}
		}()
	}
	waitGroup.Wait()

	sessions := make([]codexSession, 0, len(codexPanes))
	for index, pane := range codexPanes {
		cwd := fileURIPath(pane.CWD)
		sessions = append(sessions, codexSession{
			PaneID:      pane.PaneID,
			TabPosition: tabPositions[pane.TabID],
			IsCurrent:   pane.PaneID == collector.options.SourcePaneID,
			Repository:  repositoryName(cwd),
			Summary:     summaries[index],
		})
	}
	return sessions, errors.Join(readErrors...)
}

func (collector *windowCollector) activatePane(ctx context.Context, paneID int) error {
	_, err := collector.execute(
		ctx,
		collector.options.WezTerm,
		"cli",
		"activate-pane",
		"--pane-id",
		strconv.Itoa(paneID),
	)
	if err != nil {
		return fmt.Errorf("activate pane %d: %w", paneID, err)
	}
	return nil
}

func windowTabPositions(panes []paneSnapshot, windowID int) map[int]int {
	positions := make(map[int]int)
	for _, pane := range panes {
		if pane.WindowID != windowID {
			continue
		}
		if _, exists := positions[pane.TabID]; !exists {
			positions[pane.TabID] = len(positions) + 1
		}
	}
	return positions
}

func resolveTargetWindow(panes []paneSnapshot, targetWindowID, sourcePaneID int) (windowID int, found bool) {
	if targetWindowID >= 0 {
		for _, pane := range panes {
			if pane.WindowID == targetWindowID {
				return targetWindowID, true
			}
		}
		return 0, false
	}

	for _, pane := range panes {
		if pane.PaneID == sourcePaneID {
			return pane.WindowID, true
		}
	}
	return 0, false
}

func codexTerminalSet(processList string) map[string]struct{} {
	terminals := make(map[string]struct{})
	for _, line := range strings.Split(processList, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || filepath.Base(fields[1]) != "codex" {
			continue
		}

		isAppServer := false
		for _, argument := range fields[2:] {
			if argument == "app-server" {
				isAppServer = true
				break
			}
		}
		if !isAppServer {
			terminals[normalizeTTY(fields[0])] = struct{}{}
		}
	}
	return terminals
}

func normalizeTTY(tty string) string {
	return strings.TrimPrefix(strings.TrimSpace(tty), "/dev/")
}

func fileURIPath(value string) string {
	parsed, err := url.Parse(value)
	if err == nil && parsed.Scheme == "file" && parsed.Path != "" {
		return parsed.Path
	}
	return value
}

func repositoryName(cwd string) string {
	if cwd == "" {
		return "unknown repo"
	}
	name := filepath.Base(findRepositoryRoot(cwd))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "unknown repo"
	}
	return name
}

func summarizePaneActivity(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r", ""), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		current := strings.TrimRight(lines[index], " \t")
		if !strings.HasPrefix(current, "• ") {
			continue
		}

		summary := strings.TrimSpace(strings.TrimPrefix(current, "• "))
		if isToolActivity(summary) {
			continue
		}

		parts := []string{summary}
		for continuation := index + 1; continuation < len(lines); continuation++ {
			raw := lines[continuation]
			trimmed := strings.TrimSpace(raw)
			if trimmed == "" || strings.HasPrefix(trimmed, "• ") || strings.HasPrefix(trimmed, "─") ||
				strings.HasPrefix(trimmed, "└") || strings.HasPrefix(trimmed, "│") ||
				strings.HasPrefix(trimmed, "…") || !strings.HasPrefix(raw, "  ") {
				break
			}
			parts = append(parts, trimmed)
		}
		return summarize(strings.Join(parts, " "))
	}
	return ""
}

func isToolActivity(summary string) bool {
	if strings.Contains(summary, "esc to interrupt") {
		return true
	}
	for _, activity := range []string{
		"Called",
		"Calling",
		"Context compacted",
		"Edited",
		"Explored",
		"Finished waiting",
		"Interacted with",
		"Ran",
		"Read",
		"Searched",
		"Searching the web",
		"Updated Plan",
		"Viewed",
		"Waited for",
		"Waiting for agents",
		"Working",
	} {
		if summary == activity || strings.HasPrefix(summary, activity+" ") {
			return true
		}
	}
	return false
}
