package main

import (
	"strings"
	"unicode"
)

type codexSession struct {
	Repository string
	Tab        string
	Summary    string
}

func sanitizeLabel(value string) string {
	return strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return -1
		}
		return character
	}, value)
}

func summarize(message string) string {
	message = stripControlCharacters(message)
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "#>*-•` ")
		line = strings.Join(strings.Fields(line), " ")
		if line != "" {
			return line
		}
	}
	return ""
}

func stripControlCharacters(value string) string {
	return strings.Map(func(character rune) rune {
		if character == '\n' || character == '\t' || !unicode.IsControl(character) {
			return character
		}
		return -1
	}, value)
}
