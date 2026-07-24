package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWindowSidebarShowsTopLevelCodexPanesInCurrentWindow(t *testing.T) {
	var commands []string
	var commandsMutex sync.Mutex
	execute := func(_ context.Context, name string, arguments ...string) ([]byte, error) {
		command := name + " " + strings.Join(arguments, " ")
		commandsMutex.Lock()
		commands = append(commands, command)
		commandsMutex.Unlock()
		switch command {
		case "wezterm cli list --format json":
			return []byte(`[
				{"window_id":7,"tab_id":11,"pane_id":1,"title":"alpha","tab_title":"","cwd":"file://host/work/alpha","tty_name":"/dev/ttys001"},
				{"window_id":7,"tab_id":12,"pane_id":2,"title":"shell","tab_title":"","cwd":"file://host/work/shell","tty_name":"/dev/ttys002"},
				{"window_id":7,"tab_id":11,"pane_id":99,"title":"Agents","tab_title":"","cwd":"file://host/work/alpha","tty_name":"/dev/ttys099"},
				{"window_id":8,"tab_id":21,"pane_id":3,"title":"other","tab_title":"","cwd":"file://host/work/other","tty_name":"/dev/ttys003"}
			]`), nil
		case "ps -axo tty=,comm=,args=":
			return []byte(strings.Join([]string{
				"ttys001 codex codex --dangerously-bypass-approvals-and-sandbox",
				"ttys002 /bin/zsh zsh",
				"ttys003 codex codex",
				"ttys001 /Applications/ChatGPT.app/Contents/Resources/codex codex app-server --listen stdio://",
			}, "\n")), nil
		case "wezterm cli get-text --pane-id 1 --start-line -500":
			return []byte(strings.Join([]string{
				"• I’m reviewing the authentication boundary and its tests.",
				"",
				"• Ran go test ./...",
				"  └ ok",
				"",
				"• Working (9s • esc to interrupt)",
			}, "\n")), nil
		default:
			return nil, fmt.Errorf("unexpected command: %s", command)
		}
	}

	collector := newWindowCollector(windowCollectorOptions{
		Execute:       execute,
		WezTerm:       "wezterm",
		SidebarPaneID: 99,
	})
	sessions, err := collector.refresh(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].PaneID != 1 || sessions[0].TabPosition != 1 || !sessions[0].IsCurrent {
		t.Fatalf("session identity = %#v", sessions)
	}
	output := renderSidebar(sessions, renderOptions{
		Scope: "this window",
		Width: 48,
		Plain: true,
	})

	for _, expected := range []string{
		"1 active",
		"● tab 1 · alpha",
		"reviewing the authentication boundary",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("output does not contain %q:\n%s", expected, output)
		}
	}
	for _, excluded := range []string{"shell · tab 12", "other · tab 21", "Ran go test"} {
		if strings.Contains(output, excluded) {
			t.Errorf("output unexpectedly contains %q:\n%s", excluded, output)
		}
	}
	if slices.Contains(commands, "wezterm cli get-text --pane-id 3 --start-line -500") {
		t.Fatalf("collector inspected a pane from another window: %#v", commands)
	}
}

func TestSummarizePaneActivitySkipsUIAndNestedToolActivity(t *testing.T) {
	text := strings.Join([]string{
		"• I’m checking the final implementation and its focused tests.",
		"  The visible summary may wrap onto another line.",
		"",
		"• Called",
		"• Calling",
		"• Context compacted",
		"• Searching the web",
		"• Interacted with /root/spec_review",
		"• Waiting for agents",
		"• Finished waiting",
		"• Updated Plan",
		"• Edited agent-sidebar/window.go (+4 -2)",
		"• Waiting for background terminal (14s • esc to interrupt)",
		"  • A nested bullet emitted by tool output",
		"• Working (9s • esc to interrupt)",
	}, "\n")

	got := summarizePaneActivity(text)
	want := "I’m checking the final implementation and its focused tests. The visible summary may wrap onto another line."
	if got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

func TestWindowCollectorSurfacesPaneTextFailures(t *testing.T) {
	execute := func(_ context.Context, name string, arguments ...string) ([]byte, error) {
		command := name + " " + strings.Join(arguments, " ")
		switch command {
		case "wezterm cli list --format json":
			return []byte(`[
				{"window_id":7,"tab_id":11,"pane_id":1,"cwd":"file://host/work/alpha","tty_name":"/dev/ttys001"},
				{"window_id":7,"tab_id":12,"pane_id":99,"cwd":"file://host/work/alpha","tty_name":"/dev/ttys099"}
			]`), nil
		case "ps -axo tty=,comm=,args=":
			return []byte("ttys001 codex codex"), nil
		case "wezterm cli get-text --pane-id 1 --start-line -500":
			return nil, errors.New("pane disappeared")
		default:
			return nil, fmt.Errorf("unexpected command: %s", command)
		}
	}

	collector := newWindowCollector(windowCollectorOptions{
		Execute:       execute,
		WezTerm:       "wezterm",
		SidebarPaneID: 99,
	})
	sessions, err := collector.refresh(context.Background())
	if len(sessions) != 1 || sessions[0].Summary != "Codex is running" {
		t.Fatalf("sessions = %#v", sessions)
	}
	if err == nil || !strings.Contains(err.Error(), "pane 1") {
		t.Fatalf("error = %v, want pane-specific diagnostic", err)
	}
}

func TestWindowCollectorBoundsExternalCommands(t *testing.T) {
	execute := func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	collector := newWindowCollector(windowCollectorOptions{
		Execute:        execute,
		WezTerm:        "wezterm",
		SidebarPaneID:  99,
		CommandTimeout: 10 * time.Millisecond,
	})

	started := time.Now()
	_, err := collector.refresh(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("refresh took %s despite command timeout", elapsed)
	}
}

func TestWindowCollectorActivatesCodexPane(t *testing.T) {
	var command string
	execute := func(_ context.Context, name string, arguments ...string) ([]byte, error) {
		command = name + " " + strings.Join(arguments, " ")
		return nil, nil
	}
	collector := newWindowCollector(windowCollectorOptions{
		Execute: execute,
		WezTerm: "wezterm",
	})

	if err := collector.activatePane(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	if want := "wezterm cli activate-pane --pane-id 42"; command != want {
		t.Fatalf("activation command = %q, want %q", command, want)
	}
}
