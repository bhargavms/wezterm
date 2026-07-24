package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
)

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiCyan   = "\x1b[36m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
)

type renderOptions struct {
	RepoRoot string
	Width    int
	Now      time.Time
	Plain    bool
	Err      error
}

func renderSidebar(agents []agent, options renderOptions) string {
	width := max(20, options.Width)
	var output strings.Builder

	repository := sanitizeLabel(filepath.Base(options.RepoRoot))
	active := 0
	stale := 0
	recent := 0
	for _, current := range agents {
		switch current.Status {
		case statusDone:
			recent++
		case statusStale:
			stale++
		default:
			active++
		}
	}

	output.WriteString(style("AGENTS", ansiBold+ansiCyan, options.Plain))
	output.WriteString(" · ")
	output.WriteString(truncate(repository, width-9))
	output.WriteByte('\n')
	for _, line := range wrap(fmt.Sprintf("%d active · %d stale · %d recent", active, stale, recent), width, 2) {
		output.WriteString(line)
		output.WriteByte('\n')
	}
	output.WriteByte('\n')

	if options.Err != nil {
		output.WriteString(style("! "+truncate(sanitizeLabel(options.Err.Error()), width-2), ansiYellow, options.Plain))
		output.WriteString("\n\n")
	}

	if len(agents) == 0 {
		output.WriteString("No active subagents.\n\n")
		for _, line := range wrap("Ask Codex to delegate work.", width, 2) {
			output.WriteString(style(line+"\n", ansiDim, options.Plain))
		}
		for _, line := range wrap("Use /agent to inspect threads.", width, 2) {
			output.WriteString(style(line+"\n", ansiDim, options.Plain))
		}
	} else {
		for _, current := range agents {
			output.WriteString(renderAgent(current, width, options))
		}
	}

	output.WriteByte('\n')
	output.WriteString(style("q close   r refresh", ansiDim, options.Plain))
	output.WriteByte('\n')
	return output.String()
}

func renderAgent(current agent, width int, options renderOptions) string {
	var output strings.Builder
	marker := "●"
	markerStyle := ansiGreen
	if current.Status == statusStale {
		marker = "!"
		markerStyle = ansiYellow
	} else if current.Status == statusDone {
		marker = "✓"
		markerStyle = ansiDim
	}

	ageFrom := current.UpdatedAt
	if current.Status == statusDone {
		ageFrom = current.CompletedAt
	}
	age := relativeAge(options.Now, ageFrom)

	label := sanitizeLabel(current.Name)
	nickname := sanitizeLabel(current.Nickname)
	if nickname != "" && !strings.EqualFold(label, nickname) {
		label += " · " + nickname
	}
	labelWidth := max(4, width-runewidth.StringWidth(age)-4)

	output.WriteString(style(marker, markerStyle, options.Plain))
	output.WriteByte(' ')
	output.WriteString(style(truncate(label, labelWidth), ansiBold, options.Plain))
	output.WriteByte(' ')
	output.WriteString(style(age, ansiDim, options.Plain))
	output.WriteByte('\n')

	summary := current.Summary
	if summary == "" {
		if current.Status == statusDone {
			summary = "Completed"
		} else {
			summary = "Starting…"
		}
	}
	for _, line := range wrap(summary, max(8, width-2), 2) {
		output.WriteString("  ")
		output.WriteString(line)
		output.WriteByte('\n')
	}
	output.WriteByte('\n')
	return output.String()
}

func style(value, code string, plain bool) string {
	if plain {
		return value
	}
	return code + value + ansiReset
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if runewidth.StringWidth(value) <= width {
		return value
	}
	return runewidth.Truncate(value, width, "…")
}

func wrap(value string, width, maxLines int) []string {
	words := strings.Fields(value)
	if len(words) == 0 {
		return nil
	}

	lines := make([]string, 0, maxLines)
	current := ""
	for _, word := range words {
		if runewidth.StringWidth(word) > width {
			word = truncate(word, width)
		}
		candidate := word
		if current != "" {
			candidate = current + " " + word
		}
		if runewidth.StringWidth(candidate) <= width {
			current = candidate
			continue
		}

		lines = append(lines, current)
		current = word
		if len(lines) == maxLines {
			lines[maxLines-1] = truncate(lines[maxLines-1], width)
			return lines
		}
	}

	if current != "" && len(lines) < maxLines {
		lines = append(lines, current)
	}
	return lines
}

func relativeAge(now, then time.Time) string {
	if then.IsZero() {
		return "now"
	}
	elapsed := now.Sub(then)
	if elapsed < 0 {
		elapsed = 0
	}
	switch {
	case elapsed < time.Minute:
		return "now"
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh", int(elapsed.Hours()))
	default:
		return fmt.Sprintf("%dd", int(elapsed.Hours()/24))
	}
}
