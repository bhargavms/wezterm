package main

import (
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-runewidth"
)

func TestRenderSidebarShowsStatusSummaryAndNoANSIInPlainMode(t *testing.T) {
	now := time.Now().UTC()
	output := renderSidebar([]agent{
		{
			Name:      "review authentication",
			Nickname:  "Dalton",
			Summary:   "Tracing the authentication boundary and its focused unit tests",
			Status:    statusRunning,
			UpdatedAt: now.Add(-2 * time.Minute),
		},
		{
			Name:      "stale worker",
			Summary:   "Waiting for another update",
			Status:    statusStale,
			UpdatedAt: now.Add(-31 * time.Minute),
		},
		{
			Name:        "documentation",
			Summary:     "Completed",
			Status:      statusDone,
			CompletedAt: now.Add(-time.Minute),
		},
	}, renderOptions{
		RepoRoot: "/tmp/example-repository",
		Width:    36,
		Now:      now,
		Plain:    true,
	})

	for _, expected := range []string{
		"AGENTS · example-repository",
		"1 active · 1 stale · 1 recent",
		"● review authentication · Dalton 2m",
		"Tracing the authentication",
		"! stale worker 31m",
		"✓ documentation 1m",
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
	now := time.Now().UTC()
	output := renderSidebar([]agent{{
		Name:      "設計レビュー 🧭",
		Summary:   "認証フローを確認しています 🚀",
		Status:    statusRunning,
		UpdatedAt: now,
	}}, renderOptions{
		RepoRoot: "/tmp/日本語-project",
		Width:    24,
		Now:      now,
		Plain:    true,
	})

	for _, line := range strings.Split(output, "\n") {
		if runewidth.StringWidth(line) > 24 {
			t.Errorf("line exceeds 24 terminal cells: %q (%d)", line, runewidth.StringWidth(line))
		}
	}
}

func TestRenderSidebarStripsControlSequencesFromLabels(t *testing.T) {
	now := time.Now().UTC()
	output := renderSidebar([]agent{{
		Name:      "review\x1b]2;owned\a",
		Nickname:  "helper\x1b[31m",
		Summary:   "Safe summary",
		Status:    statusRunning,
		UpdatedAt: now,
	}}, renderOptions{
		RepoRoot: "/tmp/project\x1b]2;owned\a",
		Width:    42,
		Now:      now,
		Plain:    true,
		Err:      errTest("bad\x1b[31m"),
	})

	if strings.ContainsAny(output, "\x1b\a") {
		t.Fatalf("rendered labels contain terminal control characters: %q", output)
	}
}

func TestRenderSidebarExplainsEmptyStateAndErrors(t *testing.T) {
	output := renderSidebar(nil, renderOptions{
		RepoRoot: "/tmp/project",
		Width:    42,
		Now:      time.Now(),
		Plain:    true,
		Err:      errTest("session directory unavailable"),
	})

	for _, expected := range []string{
		"session directory unavailable",
		"No active subagents.",
		"Use /agent to inspect threads.",
	} {
		if !strings.Contains(output, expected) {
			t.Errorf("output does not contain %q:\n%s", expected, output)
		}
	}
}

type errTest string

func (err errTest) Error() string {
	return string(err)
}
