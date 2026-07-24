package main

import (
	"fmt"
	"strings"

	"github.com/mattn/go-runewidth"
)

const (
	ansiReset              = "\x1b[0m"
	ansiBold               = "\x1b[1m"
	ansiDim                = "\x1b[2m"
	ansiCyan               = "\x1b[36m"
	ansiGreen              = "\x1b[32m"
	ansiYellow             = "\x1b[33m"
	fixedSidebarLines      = 6
	errorBannerLines       = 2
	maxSessionLines        = 4
	overflowIndicatorLines = 2
)

type renderOptions struct {
	Scope          string
	Width          int
	Height         int
	Plain          bool
	Err            error
	SelectedPaneID int
}

func renderSidebar(sessions []codexSession, options renderOptions) string {
	width := max(20, options.Width)
	minimumNormalHeight := fixedSidebarLines + maxSessionLines + overflowIndicatorLines
	if options.Err != nil {
		minimumNormalHeight += errorBannerLines
	}
	if options.Height > 0 && options.Height < minimumNormalHeight {
		return renderCompactSidebar(sessions, options)
	}
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
		start, end := visibleSessionRange(sessions, options)
		if start > 0 {
			output.WriteString(style(fmt.Sprintf("↑ %d more\n", start), ansiDim, options.Plain))
		}
		for _, session := range sessions[start:end] {
			output.WriteString(renderSession(session, width, options))
		}
		if remaining := len(sessions) - end; remaining > 0 {
			output.WriteString(style(fmt.Sprintf("↓ %d more\n", remaining), ansiDim, options.Plain))
		}
	}

	output.WriteByte('\n')
	for _, help := range []string{
		"↑↓/j/k move · enter switch",
		"1-9 jump · q close · r refresh",
	} {
		output.WriteString(style(truncate(help, width), ansiDim, options.Plain))
		output.WriteByte('\n')
	}
	return strings.TrimSuffix(output.String(), "\n")
}

func renderCompactSidebar(sessions []codexSession, options renderOptions) string {
	width := max(20, options.Width)
	lines := []string{
		style("AGENTS", ansiBold+ansiCyan, options.Plain) +
			" · " + truncate(sanitizeLabel(options.Scope), width-9),
	}

	if len(sessions) == 0 {
		lines = append(lines, "No Codex sessions in this window.")
	} else {
		selected := selectedSessionIndex(sessions, options.SelectedPaneID)
		session := sessions[selected]
		label := fmt.Sprintf("tab %d · %s", session.TabPosition, sanitizeLabel(session.Repository))
		lines = append(lines,
			style("›", ansiCyan, options.Plain)+" "+
				style(truncate(label, width-2), ansiBold, options.Plain),
			fmt.Sprintf("%d active", len(sessions)),
		)

		summary := session.Summary
		if summary == "" {
			summary = "Codex is running"
		}
		if wrapped := wrap(summary, max(8, width-2), 1); len(wrapped) > 0 {
			lines = append(lines, "  "+wrapped[0])
		}
		if options.Err != nil {
			lines = append(lines, style("! "+truncate(sanitizeLabel(options.Err.Error()), width-2), ansiYellow, options.Plain))
		}

		var overflow []string
		if selected > 0 {
			overflow = append(overflow, fmt.Sprintf("↑ %d", selected))
		}
		if below := len(sessions) - selected - 1; below > 0 {
			overflow = append(overflow, fmt.Sprintf("↓ %d", below))
		}
		if len(overflow) > 0 {
			lines = append(lines, style(strings.Join(overflow, " · ")+" more", ansiDim, options.Plain))
		}
	}
	lines = append(lines, style(truncate("enter switch · q close", width), ansiDim, options.Plain))

	if len(lines) > options.Height {
		lines = lines[:options.Height]
	}
	return strings.Join(lines, "\n")
}

func visibleSessionRange(sessions []codexSession, options renderOptions) (int, int) {
	if options.Height <= 0 || len(sessions) == 0 {
		return 0, len(sessions)
	}

	fixedLines := fixedSidebarLines
	if options.Err != nil {
		fixedLines += errorBannerLines
	}
	if fixedLines+len(sessions)*maxSessionLines <= options.Height {
		return 0, len(sessions)
	}

	capacity := max(1, (options.Height-fixedLines-overflowIndicatorLines)/maxSessionLines)
	capacity = min(capacity, len(sessions))
	selected := selectedSessionIndex(sessions, options.SelectedPaneID)

	start := selected - capacity/2
	start = max(0, min(start, len(sessions)-capacity))
	return start, start + capacity
}

func selectedSessionIndex(sessions []codexSession, selectedPaneID int) int {
	for index, session := range sessions {
		if session.PaneID == selectedPaneID {
			return index
		}
	}
	return 0
}

func renderSession(session codexSession, width int, options renderOptions) string {
	var output strings.Builder
	label := fmt.Sprintf("tab %d · %s", session.TabPosition, sanitizeLabel(session.Repository))
	marker := "●"
	markerStyle := ansiGreen
	if session.PaneID != 0 && session.PaneID == options.SelectedPaneID {
		marker = "›"
		markerStyle = ansiCyan
	}

	output.WriteString(style(marker, markerStyle, options.Plain))
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
