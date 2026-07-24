package main

type pickerCommand int

const (
	pickerNone pickerCommand = iota
	pickerRedraw
	pickerActivate
	pickerClose
	pickerRefresh
)

type pickerResult struct {
	command pickerCommand
	paneID  int
}

type escapeSequenceState uint8

const (
	escapeNone escapeSequenceState = iota
	escapeStarted
	escapeCSI
)

type sidebarPicker struct {
	sessions   []codexSession
	selected   int
	inputState escapeSequenceState
}

func (picker *sidebarPicker) updateSessions(sessions []codexSession) {
	selectedPaneID := picker.selectedPaneID()
	picker.sessions = sessions
	picker.selected = 0

	for index, session := range sessions {
		if session.PaneID == selectedPaneID {
			picker.selected = index
			return
		}
	}
	for index, session := range sessions {
		if session.IsCurrent {
			picker.selected = index
			return
		}
	}
}

func (picker *sidebarPicker) selectedPaneID() int {
	if picker.selected < 0 || picker.selected >= len(picker.sessions) {
		return 0
	}
	return picker.sessions[picker.selected].PaneID
}

func (picker *sidebarPicker) move(delta int) {
	if len(picker.sessions) == 0 {
		return
	}
	picker.selected = (picker.selected + delta + len(picker.sessions)) % len(picker.sessions)
}

func (picker *sidebarPicker) handleByte(key byte) pickerResult {
	switch picker.inputState {
	case escapeStarted:
		picker.inputState = escapeNone
		if key == '[' {
			picker.inputState = escapeCSI
			return pickerResult{}
		}
	case escapeCSI:
		picker.inputState = escapeNone
		switch key {
		case 'A':
			picker.move(-1)
			return pickerResult{command: pickerRedraw}
		case 'B':
			picker.move(1)
			return pickerResult{command: pickerRedraw}
		default:
			return pickerResult{}
		}
	}

	switch key {
	case 27:
		picker.inputState = escapeStarted
	case 'q', 3:
		return pickerResult{command: pickerClose}
	case 'r':
		return pickerResult{command: pickerRefresh}
	case '\r', '\n':
		if paneID := picker.selectedPaneID(); paneID != 0 {
			return pickerResult{command: pickerActivate, paneID: paneID}
		}
	case 'j':
		picker.move(1)
		return pickerResult{command: pickerRedraw}
	case 'k':
		picker.move(-1)
		return pickerResult{command: pickerRedraw}
	}
	if key >= '1' && key <= '9' {
		tabPosition := int(key - '0')
		for _, session := range picker.sessions {
			if session.TabPosition == tabPosition {
				return pickerResult{command: pickerActivate, paneID: session.PaneID}
			}
		}
	}
	return pickerResult{command: pickerNone}
}

func (picker *sidebarPicker) escapePending() bool {
	return picker.inputState != escapeNone
}

func (picker *sidebarPicker) expireEscape() pickerResult {
	if !picker.escapePending() {
		return pickerResult{command: pickerNone}
	}
	picker.inputState = escapeNone
	return pickerResult{command: pickerClose}
}
