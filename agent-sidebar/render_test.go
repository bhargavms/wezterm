package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestRenderSidebarShowsSessionsAndNoANSIInPlainMode(t *testing.T) {
	output := renderSidebar([]codexSession{
		{
			PaneID:      42,
			TabPosition: 3,
			Repository:  "review-authentication",
			Summary:     "Tracing the authentication boundary and its focused unit tests",
		},
	}, renderOptions{
		Scope:          "this window",
		Width:          36,
		Plain:          true,
		SelectedPaneID: 42,
	})

	for _, expected := range []string{
		"AGENTS · this window",
		"1 active",
		"› tab 3 · review-authentication",
		"Tracing the authentication",
		"j/k move · enter switch",
		"1-9 jump · q close",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("output does not contain %q:\n%s", expected, output)
		}
	}
	if strings.Contains(output, "\x1b") {
		t.Fatalf("plain output contains ANSI escapes: %q", output)
	}

	for _, line := range strings.Split(output, "\n") {
		if runewidth.StringWidth(line) > 36 {
			t.Errorf("line exceeds render width: %q", line)
		}
	}
}

func TestRenderSidebarRespectsTerminalCellWidth(t *testing.T) {
	output := renderSidebar([]codexSession{{
		TabPosition: 8,
		Repository:  "設計レビュー 🧭",
		Summary:     "認証フローを確認しています 🚀",
	}}, renderOptions{
		Scope: "this window",
		Width: 24,
		Plain: true,
	})

	for _, line := range strings.Split(output, "\n") {
		if runewidth.StringWidth(line) > 24 {
			t.Errorf("line exceeds 24 terminal cells: %q (%d)", line, runewidth.StringWidth(line))
		}
	}
}

func TestRenderSidebarStripsControlSequencesFromLabels(t *testing.T) {
	output := renderSidebar([]codexSession{{
		TabPosition: 1,
		Repository:  "review\x1b]2;owned\a",
		Summary:     "Safe summary",
	}}, renderOptions{
		Scope: "this window\x1b]2;owned\a",
		Width: 42,
		Plain: true,
		Err:   errTest("bad\x1b[31m"),
	})

	if strings.ContainsAny(output, "\x1b\a") {
		t.Fatalf("rendered labels contain terminal control characters: %q", output)
	}
}

func TestRenderSidebarExplainsEmptyStateAndErrors(t *testing.T) {
	output := renderSidebar(nil, renderOptions{
		Scope: "this window",
		Width: 42,
		Plain: true,
		Err:   errTest("pane list unavailable"),
	})

	for _, expected := range []string{
		"pane list unavailable",
		"No Codex sessions in this window.",
		"Open a tab and start Codex.",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("output does not contain %q:\n%s", expected, output)
		}
	}
}

func TestRenderSidebarKeepsTabVisibleForLongRepositoryNames(t *testing.T) {
	output := renderSidebar([]codexSession{{
		TabPosition: 12,
		Repository:  "growthbook-experiment-tracker",
		Summary:     "Reviewing the rollout",
	}}, renderOptions{
		Scope: "this window",
		Width: 30,
		Plain: true,
	})

	if !strings.Contains(output, "● tab 12 · growthbook") {
		t.Fatalf("tab position or repository prefix disappeared:\n%s", output)
	}
}

func TestRenderSidebarFitsViewportAndKeepsSelectedTabVisible(t *testing.T) {
	sessions := make([]codexSession, 0, 9)
	for tab := 1; tab <= 9; tab++ {
		sessions = append(sessions, codexSession{
			PaneID:      tab,
			TabPosition: tab,
			Repository:  fmt.Sprintf("repository-%d", tab),
			Summary:     "This intentionally long activity summary wraps onto a second terminal line",
		})
	}

	output := renderSidebar(sessions, renderOptions{
		Scope:          "this window",
		Width:          40,
		Height:         37,
		Plain:          true,
		SelectedPaneID: 1,
	})

	if rows := renderedRows(output); rows > 37 {
		t.Fatalf("rendered %d rows into a 37-row viewport:\n%s", rows, output)
	}
	for _, expected := range []string{"AGENTS · this window", "› tab 1 · repository-1", "↓ 2 more"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output does not contain %q:\n%s", expected, output)
		}
	}

	output = renderSidebar(sessions, renderOptions{
		Scope:          "this window",
		Width:          40,
		Height:         37,
		Plain:          true,
		SelectedPaneID: 9,
	})
	for _, expected := range []string{"↑ 2 more", "› tab 9 · repository-9"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output does not contain %q with the last tab selected:\n%s", expected, output)
		}
	}

	for _, height := range []int{24, 8} {
		output = renderSidebar(sessions, renderOptions{
			Scope:          "this window",
			Width:          40,
			Height:         height,
			Plain:          true,
			SelectedPaneID: 5,
		})
		if rows := renderedRows(output); rows > height {
			t.Fatalf("rendered %d rows into a %d-row viewport:\n%s", rows, height, output)
		}
		for _, expected := range []string{"AGENTS · this window", "› tab 5 · repository-5"} {
			if !strings.Contains(output, expected) {
				t.Fatalf("%d-row output does not contain %q:\n%s", height, expected, output)
			}
		}
	}

	output = renderSidebar(sessions, renderOptions{
		Scope:          "this window",
		Width:          40,
		Height:         13,
		Plain:          true,
		SelectedPaneID: 5,
		Err:            errTest("pane refresh failed"),
	})
	if rows := renderedRows(output); rows > 13 {
		t.Fatalf("rendered %d rows into a 13-row viewport with an error:\n%s", rows, output)
	}
	for _, expected := range []string{"AGENTS · this window", "› tab 5 · repository-5"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("13-row error output does not contain %q:\n%s", expected, output)
		}
	}
}

func renderedRows(output string) int {
	if output == "" {
		return 0
	}
	return strings.Count(output, "\n") + 1
}

type errTest string

func (err errTest) Error() string {
	return string(err)
}
