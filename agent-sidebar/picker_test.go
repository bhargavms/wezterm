package main

import "testing"

func TestSidebarPickerStartsOnCurrentTabAndMovesWithVimOrArrowKeys(t *testing.T) {
	picker := sidebarPicker{}
	picker.updateSessions([]codexSession{
		{PaneID: 10, TabPosition: 1},
		{PaneID: 20, TabPosition: 3, IsCurrent: true},
		{PaneID: 30, TabPosition: 4},
	})

	if got := picker.selectedPaneID(); got != 20 {
		t.Fatalf("initial selected pane = %d, want current pane 20", got)
	}

	if result := picker.handleByte('j'); result.command != pickerRedraw {
		t.Fatalf("j result = %#v, want redraw", result)
	}
	if got := picker.selectedPaneID(); got != 30 {
		t.Fatalf("selected pane after j = %d, want 30", got)
	}

	for _, key := range []byte{27, '[', 'A'} {
		picker.handleByte(key)
	}
	if got := picker.selectedPaneID(); got != 20 {
		t.Fatalf("selected pane after up arrow = %d, want 20", got)
	}

	picker.handleByte('k')
	if got := picker.selectedPaneID(); got != 10 {
		t.Fatalf("selected pane after k = %d, want wrapped pane 10", got)
	}
}

func TestSidebarPickerActivatesSelectedOrNumberedTab(t *testing.T) {
	picker := sidebarPicker{}
	picker.updateSessions([]codexSession{
		{PaneID: 10, TabPosition: 1, IsCurrent: true},
		{PaneID: 30, TabPosition: 3},
	})

	picker.handleByte('j')
	if result := picker.handleByte('\r'); result.command != pickerActivate || result.paneID != 30 {
		t.Fatalf("enter result = %#v, want activation of pane 30", result)
	}

	if result := picker.handleByte('1'); result.command != pickerActivate || result.paneID != 10 {
		t.Fatalf("1 result = %#v, want activation of pane 10", result)
	}
	if result := picker.handleByte('2'); result.command != pickerNone {
		t.Fatalf("2 result = %#v, want no action without a Codex session in tab 2", result)
	}
}

func TestSidebarPickerPreservesCloseAndRefreshKeys(t *testing.T) {
	picker := sidebarPicker{}
	if result := picker.handleByte('q'); result.command != pickerClose {
		t.Fatalf("q result = %#v, want close", result)
	}
	if result := picker.handleByte(3); result.command != pickerClose {
		t.Fatalf("ctrl-c result = %#v, want close", result)
	}
	if result := picker.handleByte('r'); result.command != pickerRefresh {
		t.Fatalf("r result = %#v, want refresh", result)
	}
}

func TestSidebarPickerClosesWhenStandaloneEscapeExpires(t *testing.T) {
	picker := sidebarPicker{}
	if result := picker.handleByte(27); result.command != pickerNone || !picker.escapePending() {
		t.Fatalf("escape result = %#v, pending = %t", result, picker.escapePending())
	}
	if result := picker.expireEscape(); result.command != pickerClose || picker.escapePending() {
		t.Fatalf("expired escape result = %#v, pending = %t", result, picker.escapePending())
	}
}
