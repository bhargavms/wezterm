package main

import (
	"fmt"
	"strings"

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
	Scope string
	Width int
	Plain bool
	Err   error
}

func renderSidebar(sessions []codexSession, options renderOptions) string {
	width := max(20, options.Width)
	var output strings.Builder

	output.WriteString(style("AGENTS", ansiBold+ansiCyan, options.Plain))
	output.WriteString(" · ")
	output.WriteString(truncate(sanitizeLabel(options.Scope), width-9))
	output.WriteByte('\n')
	for _, line := range wrap(fmt.Sprintf("%d active", len(sessions)), width, 2) {
		output.WriteString(line)
		output.WriteByte('\n')
	}
	output.WriteByte('\n')

	if options.Err != nil {
		output.WriteString(style("! "+truncate(sanitizeLabel(options.Err.Error()), width-2), ansiYellow, options.Plain))
		output.WriteString("\n\n")
	}

	if len(sessions) == 0 {
		output.WriteString("No Codex sessions in this window.\n\n")
		for _, line := range wrap("Open a tab and start Codex.", width, 2) {
			output.WriteString(style(line+"\n", ansiDim, options.Plain))
		}
	} else {
		for _, session := range sessions {
			output.WriteString(renderSession(session, width, options))
		}
	}

	output.WriteByte('\n')
	output.WriteString(style("q close   r refresh", ansiDim, options.Plain))
	output.WriteByte('\n')
	return output.String()
}

func renderSession(session codexSession, width int, options renderOptions) string {
	var output strings.Builder
	label := sanitizeLabel(session.Tab) + " · " + sanitizeLabel(session.Repository)

	output.WriteString(style("●", ansiGreen, options.Plain))
	output.WriteByte(' ')
	output.WriteString(style(truncate(label, width-2), ansiBold, options.Plain))
	output.WriteByte('\n')

	summary := session.Summary
	if summary == "" {
		summary = "Codex is running"
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
