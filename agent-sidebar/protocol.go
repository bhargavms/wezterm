package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

type envelope struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type sessionMeta struct {
	ID             string          `json:"id"`
	CWD            string          `json:"cwd"`
	AgentNickname  string          `json:"agent_nickname"`
	AgentPath      string          `json:"agent_path"`
	ParentThreadID string          `json:"parent_thread_id"`
	Source         json.RawMessage `json:"source"`
}

type sessionSource struct {
	Subagent json.RawMessage `json:"subagent"`
	SubAgent json.RawMessage `json:"subAgent"`
}

type subagentSource struct {
	ThreadSpawn      *threadSpawn `json:"thread_spawn"`
	ThreadSpawnCamel *threadSpawn `json:"threadSpawn"`
}

type threadSpawn struct {
	ParentThreadID string `json:"parent_thread_id"`
	AgentPath      string `json:"agent_path"`
	AgentNickname  string `json:"agent_nickname"`
}

type eventPayload struct {
	Type             string          `json:"type"`
	Phase            string          `json:"phase"`
	Message          json.RawMessage `json:"message"`
	LastAgentMessage json.RawMessage `json:"last_agent_message"`
	StartedAt        json.RawMessage `json:"started_at"`
	CompletedAt      json.RawMessage `json:"completed_at"`
}

type turnBootstrap struct {
	foundLifecycle  bool
	waitingForStart bool
	stopped         bool
	status          agentStatus
	summary         string
	fallbackSummary string
	summaryAt       time.Time
	startedAt       time.Time
	updatedAt       time.Time
	completedAt     time.Time
}

func (state *turnBootstrap) consume(line []byte) bool {
	var item envelope
	if json.Unmarshal(line, &item) != nil || item.Type != "event_msg" {
		return false
	}

	var payload eventPayload
	if json.Unmarshal(item.Payload, &payload) != nil {
		return false
	}
	eventTime := parseEventTime(item.Timestamp)

	switch payload.Type {
	case "agent_message":
		if payload.Phase == "commentary" && state.summary == "" {
			if message := summarize(jsonString(payload.Message)); message != "" {
				state.summary = message
				state.summaryAt = eventTime
			}
		}
	case "task_complete":
		if state.foundLifecycle {
			return state.stopped
		}
		state.foundLifecycle = true
		state.waitingForStart = true
		state.status = statusDone
		state.completedAt = rawEventTime(payload.CompletedAt, eventTime)
		state.updatedAt = state.completedAt
		state.fallbackSummary = summarize(jsonString(payload.LastAgentMessage))

		if !state.summaryAt.IsZero() && state.summaryAt.After(state.completedAt) {
			state.status = statusRunning
			state.startedAt = state.summaryAt
			state.updatedAt = state.summaryAt
			state.completedAt = time.Time{}
			state.stopped = true
		}
	case "task_started":
		startedAt := rawEventTime(payload.StartedAt, eventTime)
		if !state.foundLifecycle {
			state.foundLifecycle = true
			state.status = statusRunning
			state.startedAt = startedAt
			state.updatedAt = eventTime
			if state.summaryAt.After(state.updatedAt) {
				state.updatedAt = state.summaryAt
			}
			state.stopped = true
		} else if state.waitingForStart {
			state.startedAt = startedAt
			state.stopped = true
		}
	}
	return state.stopped
}

func (state *turnBootstrap) apply(target *agent) bool {
	if !state.foundLifecycle {
		if state.summary == "" {
			return false
		}
		target.Status = statusRunning
		target.Summary = state.summary
		target.StartedAt = state.summaryAt
		target.UpdatedAt = state.summaryAt
		target.CompletedAt = time.Time{}
		return true
	}

	target.Status = state.status
	target.Summary = state.summary
	target.StartedAt = state.startedAt
	target.UpdatedAt = state.updatedAt
	target.CompletedAt = state.completedAt
	if target.Status == statusDone && target.Summary == "" {
		target.Summary = state.fallbackSummary
	}
	return true
}

func (c *collector) applyLine(file *sessionFile, line []byte) {
	var item envelope
	if err := json.Unmarshal(line, &item); err != nil {
		return
	}

	switch item.Type {
	case "session_meta":
		c.applySessionMeta(file, item.Payload)
	case "event_msg":
		c.applyEvent(file, item)
	}
}

func (c *collector) applySessionMeta(file *sessionFile, raw json.RawMessage) bool {
	var meta sessionMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return false
	}

	var source sessionSource
	if err := json.Unmarshal(meta.Source, &source); err != nil {
		return false
	}
	rawSubagent := source.Subagent
	if len(rawSubagent) == 0 {
		rawSubagent = source.SubAgent
	}
	if len(rawSubagent) == 0 || string(rawSubagent) == "null" {
		return false
	}

	var sourceKind string
	if json.Unmarshal(rawSubagent, &sourceKind) == nil && sourceKind != "" {
		file.isSubagent = true
		file.agent.ID = meta.ID
		file.agent.CWD = filepath.Clean(meta.CWD)
		file.agent.Path = firstNonEmpty(meta.AgentPath, "/root/"+sourceKind)
		file.agent.Nickname = sanitizeLabel(meta.AgentNickname)
		file.agent.ParentID = meta.ParentThreadID
		file.agent.Name = displayAgentName(file.agent.Path, file.agent.Nickname)
		file.agent.SourceFile = file.path
		file.agent.Status = statusDone
		return true
	}

	var subagent subagentSource
	if err := json.Unmarshal(rawSubagent, &subagent); err != nil {
		return false
	}
	spawn := subagent.ThreadSpawn
	if spawn == nil {
		spawn = subagent.ThreadSpawnCamel
	}
	if spawn == nil {
		return false
	}

	file.isSubagent = true
	file.agent.ID = meta.ID
	file.agent.CWD = filepath.Clean(meta.CWD)
	file.agent.Path = firstNonEmpty(meta.AgentPath, spawn.AgentPath)
	file.agent.Nickname = sanitizeLabel(firstNonEmpty(meta.AgentNickname, spawn.AgentNickname))
	file.agent.ParentID = firstNonEmpty(meta.ParentThreadID, spawn.ParentThreadID)
	file.agent.Name = displayAgentName(file.agent.Path, file.agent.Nickname)
	file.agent.SourceFile = file.path
	file.agent.Status = statusDone
	return true
}

func (c *collector) applyEvent(file *sessionFile, item envelope) {
	if !file.isSubagent {
		return
	}

	var payload eventPayload
	if err := json.Unmarshal(item.Payload, &payload); err != nil {
		return
	}

	eventTime := parseEventTime(item.Timestamp)
	switch payload.Type {
	case "task_started":
		file.agent.Status = statusRunning
		file.agent.Summary = ""
		file.agent.StartedAt = rawEventTime(payload.StartedAt, eventTime)
		file.agent.CompletedAt = time.Time{}
		file.agent.UpdatedAt = eventTime
	case "agent_message":
		if payload.Phase != "commentary" {
			return
		}
		message := summarize(jsonString(payload.Message))
		if message == "" {
			return
		}
		if file.agent.Status == statusDone || file.agent.Status == statusStale {
			file.agent.Status = statusRunning
			file.agent.StartedAt = eventTime
			file.agent.CompletedAt = time.Time{}
		}
		if file.agent.Status != statusRunning {
			return
		}
		file.agent.Summary = message
		file.agent.UpdatedAt = eventTime
	case "task_complete":
		if file.agent.Status != statusRunning && file.agent.Status != statusStale {
			return
		}
		if file.agent.Status == statusStale {
			file.agent.Summary = ""
		}
		file.agent.Status = statusDone
		file.agent.CompletedAt = rawEventTime(payload.CompletedAt, eventTime)
		file.agent.UpdatedAt = file.agent.CompletedAt
		if file.agent.Summary == "" {
			file.agent.Summary = summarize(jsonString(payload.LastAgentMessage))
		}
	}
}

func displayAgentName(path, nickname string) string {
	name := filepath.Base(path)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = nickname
	}
	name = strings.NewReplacer("_", " ", "-", " ").Replace(name)
	return strings.TrimSpace(sanitizeLabel(name))
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

func jsonString(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}

func parseEventTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

func rawEventTime(candidate json.RawMessage, fallback time.Time) time.Time {
	var text string
	if json.Unmarshal(candidate, &text) == nil {
		if parsed := parseEventTime(text); !parsed.IsZero() {
			return parsed
		}
	}

	var unixSeconds int64
	if json.Unmarshal(candidate, &unixSeconds) == nil && unixSeconds > 0 {
		return time.Unix(unixSeconds, 0)
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
