package main

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestRenderSidebarShowsSessionsAndNoANSIInPlainMode(t *testing.T) {
	output := renderSidebar([]codexSession{
		{
			Repository: "review-authentication",
			Tab:        "tab 3",
			Summary:    "Tracing the authentication boundary and its focused unit tests",
		},
	}, renderOptions{
		Scope: "this window",
		Width: 36,
		Plain: true,
	})

	for _, expected := range []string{
		"AGENTS · this window",
		"1 active",
		"● tab 3 · review-authentication",
		"Tracing the authentication",
		"q close   r refresh",
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
		Repository: "設計レビュー 🧭",
		Tab:        "tab 8",
		Summary:    "認証フローを確認しています 🚀",
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
		Repository: "review\x1b]2;owned\a",
		Tab:        "tab 1\x1b[31m",
		Summary:    "Safe summary",
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
		Repository: "growthbook-experiment-tracker",
		Tab:        "tab 12",
		Summary:    "Reviewing the rollout",
	}}, renderOptions{
		Scope: "this window",
		Width: 30,
		Plain: true,
	})

	if !strings.Contains(output, "● tab 12 · growthbook") {
		t.Fatalf("tab position or repository prefix disappeared:\n%s", output)
	}
}

type errTest string

func (err errTest) Error() string {
	return string(err)
}
