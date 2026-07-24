package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCollectorTracksSubagentLifecycleIncrementally(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	repoRoot := t.TempDir()
	sessionsDir := t.TempDir()
	sessionPath := filepath.Join(sessionsDir, "2026", "07", "24", "subagent.jsonl")

	writeSessionLines(t, sessionPath, []string{
		sessionMetaLine(now.Add(-3*time.Minute), "child-1", repoRoot, "/root/review_auth", "Dalton", "parent-1", true),
		sessionMetaLine(now.Add(-3*time.Minute), "parent-1", repoRoot, "", "", "", false),
		eventLine(now.Add(-3*time.Minute), map[string]any{"type": "task_started"}),
		eventLine(now.Add(-3*time.Minute), map[string]any{
			"type": "agent_message", "phase": "commentary", "message": "Copied parent commentary",
		}),
		eventLine(now.Add(-2*time.Minute), map[string]any{
			"type": "task_started", "started_at": now.Add(-2 * time.Minute).Unix(),
		}),
		"{not valid json}",
		eventLine(now.Add(-time.Minute), map[string]any{
			"type": "agent_message", "phase": "commentary", "message": "## Tracing the authentication boundary\nMore detail",
		}),
	}, true)

	collector := newCollector(collectorOptions{
		SessionsDir: sessionsDir,
		RepoRoot:    repoRoot,
		DoneFor:     10 * time.Minute,
		StaleAfter:  30 * time.Minute,
		ScanWindow:  48 * time.Hour,
		Now:         func() time.Time { return now },
	})

	agents, err := collector.refresh()
	if err != nil {
		t.Fatalf("refresh returned an error: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("got %d agents, want 1", len(agents))
	}
	if agents[0].Status != statusRunning {
		t.Fatalf("status = %v, want running", agents[0].Status)
	}
	if agents[0].Name != "review auth" {
		t.Fatalf("name = %q, want %q", agents[0].Name, "review auth")
	}
	if agents[0].Summary != "Tracing the authentication boundary" {
		t.Fatalf("summary = %q", agents[0].Summary)
	}

	partial := eventLine(now.Add(-30*time.Second), map[string]any{
		"type": "agent_message", "phase": "commentary", "message": "Running focused tests",
	})
	appendText(t, sessionPath, partial)
	agents, err = collector.refresh()
	if err != nil {
		t.Fatalf("refresh with a partial line returned an error: %v", err)
	}
	if agents[0].Summary != "Tracing the authentication boundary" {
		t.Fatalf("partial JSONL changed the summary to %q", agents[0].Summary)
	}

	appendText(t, sessionPath, "\n")
	agents, err = collector.refresh()
	if err != nil {
		t.Fatalf("refresh after completing the line returned an error: %v", err)
	}
	if agents[0].Summary != "Running focused tests" {
		t.Fatalf("summary = %q, want appended commentary", agents[0].Summary)
	}

	appendText(t, sessionPath, eventLine(now, map[string]any{
		"type":               "task_complete",
		"completed_at":       now.Unix(),
		"last_agent_message": "Finished reviewing authentication.",
	})+"\n")
	agents, err = collector.refresh()
	if err != nil {
		t.Fatalf("completion refresh returned an error: %v", err)
	}
	if agents[0].Status != statusDone {
		t.Fatalf("status = %v, want done", agents[0].Status)
	}
	if agents[0].Summary != "Running focused tests" {
		t.Fatalf("completion replaced useful commentary with %q", agents[0].Summary)
	}

	appendText(t, sessionPath,
		eventLine(now.Add(time.Second), map[string]any{"type": "task_started"})+"\n"+
			eventLine(now.Add(2*time.Second), map[string]any{
				"type": "agent_message", "phase": "commentary", "message": "Checking the follow-up",
			})+"\n",
	)
	agents, err = collector.refresh()
	if err != nil {
		t.Fatalf("follow-up refresh returned an error: %v", err)
	}
	if agents[0].Status != statusRunning || agents[0].Summary != "Checking the follow-up" {
		t.Fatalf("follow-up agent = %#v", agents[0])
	}
}

func TestCollectorFiltersRepositoriesAndExpiresCompletedAgents(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	currentNow := now
	repoRoot := t.TempDir()
	otherRepo := t.TempDir()
	sessionsDir := t.TempDir()

	writeSessionLines(t, filepath.Join(sessionsDir, "matching.jsonl"), []string{
		sessionMetaLine(now.Add(-2*time.Minute), "matching", filepath.Join(repoRoot, "nested"), "/root/tests", "", "parent", true),
		eventLine(now.Add(-time.Minute), map[string]any{"type": "task_started"}),
	}, true)
	writeSessionLines(t, filepath.Join(sessionsDir, "other.jsonl"), []string{
		sessionMetaLine(now.Add(-2*time.Minute), "other", otherRepo, "/root/other", "", "parent", true),
		eventLine(now.Add(-time.Minute), map[string]any{"type": "task_started"}),
	}, true)

	collector := newCollector(collectorOptions{
		SessionsDir: sessionsDir,
		RepoRoot:    repoRoot,
		DoneFor:     10 * time.Minute,
		StaleAfter:  30 * time.Minute,
		ScanWindow:  48 * time.Hour,
		Now:         func() time.Time { return currentNow },
	})

	agents, err := collector.refresh()
	if err != nil {
		t.Fatalf("refresh returned an error: %v", err)
	}
	if len(agents) != 1 || agents[0].ID != "matching" {
		t.Fatalf("agents = %#v", agents)
	}

	currentNow = now.Add(31 * time.Minute)
	agents, err = collector.refresh()
	if err != nil {
		t.Fatalf("stale refresh returned an error: %v", err)
	}
	if len(agents) != 1 || agents[0].Status != statusStale {
		t.Fatalf("stale agents = %#v", agents)
	}

	appendText(t, filepath.Join(sessionsDir, "matching.jsonl"), eventLine(currentNow, map[string]any{
		"type": "task_complete", "completed_at": currentNow.Format(time.RFC3339Nano),
	})+"\n")
	agents, err = collector.refresh()
	if err != nil {
		t.Fatalf("completion refresh returned an error: %v", err)
	}
	if len(agents) != 1 || agents[0].Status != statusDone {
		t.Fatalf("completed agents = %#v", agents)
	}

	currentNow = currentNow.Add(11 * time.Minute)
	agents, err = collector.refresh()
	if err != nil {
		t.Fatalf("expiry refresh returned an error: %v", err)
	}
	if len(agents) != 0 {
		t.Fatalf("expired agents = %#v", agents)
	}
}

func TestCollectorRetriesAnInitiallyPartialMetadataLine(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	repoRoot := t.TempDir()
	sessionsDir := t.TempDir()
	sessionPath := filepath.Join(sessionsDir, "partial.jsonl")
	meta := sessionMetaLine(now, "partial", repoRoot, "/root/partial_agent", "", "parent", true)

	writeSessionLines(t, sessionPath, []string{meta}, false)
	collector := newCollector(collectorOptions{
		SessionsDir: sessionsDir,
		RepoRoot:    repoRoot,
		DoneFor:     10 * time.Minute,
		StaleAfter:  30 * time.Minute,
		ScanWindow:  48 * time.Hour,
		Now:         func() time.Time { return now },
	})

	agents, err := collector.refresh()
	if err != nil {
		t.Fatalf("partial metadata refresh returned an error: %v", err)
	}
	if len(agents) != 0 {
		t.Fatalf("partial metadata produced agents: %#v", agents)
	}

	appendText(t, sessionPath, "\n"+eventLine(now, map[string]any{"type": "task_started"})+"\n")
	agents, err = collector.refresh()
	if err != nil {
		t.Fatalf("completed metadata refresh returned an error: %v", err)
	}
	if len(agents) != 1 || agents[0].Status != statusRunning {
		t.Fatalf("agents after metadata completion = %#v", agents)
	}
}

func TestCollectorDiscardsOversizedLiveRecordsIncrementally(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	repoRoot := t.TempDir()
	sessionsDir := t.TempDir()
	sessionPath := filepath.Join(sessionsDir, "oversized-live.jsonl")
	writeSessionLines(t, sessionPath, []string{
		sessionMetaLine(now, "oversized-live", repoRoot, "/root/live", "", "parent", true),
		eventLine(now, map[string]any{"type": "task_started"}),
	}, true)

	collector := newCollector(collectorOptions{
		SessionsDir: sessionsDir,
		RepoRoot:    repoRoot,
		DoneFor:     10 * time.Minute,
		StaleAfter:  30 * time.Minute,
		ScanWindow:  48 * time.Hour,
		Now:         func() time.Time { return now },
	})
	if _, err := collector.refresh(); err != nil {
		t.Fatal(err)
	}

	appendText(t, sessionPath, strings.Repeat("x", maxJSONLRecordBytes+1))
	if _, err := collector.refresh(); err != nil {
		t.Fatal(err)
	}
	firstSize := fileSize(t, sessionPath)
	tracked := collector.files[sessionPath]
	if tracked.offset != firstSize || !tracked.stream.discarding || len(tracked.stream.partial) != 0 {
		t.Fatalf("first oversized chunk was retained: offset=%d size=%d stream=%#v", tracked.offset, firstSize, tracked.stream)
	}

	appendText(t, sessionPath, strings.Repeat("y", maxJSONLRecordBytes))
	if _, err := collector.refresh(); err != nil {
		t.Fatal(err)
	}
	secondSize := fileSize(t, sessionPath)
	if tracked.offset != secondSize || !tracked.stream.discarding {
		t.Fatalf("growing oversized record was reread: offset=%d size=%d", tracked.offset, secondSize)
	}

	appendText(t, sessionPath, "\n"+eventLine(now, map[string]any{
		"type": "agent_message", "phase": "commentary", "message": "Recovered after the oversized record",
	})+"\n")
	agents, err := collector.refresh()
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].Summary != "Recovered after the oversized record" {
		t.Fatalf("agents after oversized record = %#v", agents)
	}
	if tracked.stream.discarding {
		t.Fatal("oversized-record discard state did not clear at newline")
	}
}

func TestJSONLAccumulatorSkipsOversizedRecordsAndContinues(t *testing.T) {
	accumulator := jsonlAccumulator{}
	var lines []string
	visit := func(line []byte) bool {
		lines = append(lines, string(line))
		return false
	}

	accumulator.feed([]byte(strings.Repeat("x", maxJSONLRecordBytes)), visit)
	accumulator.feed([]byte("x\nvalid"), visit)
	accumulator.feed([]byte("-line\n"), visit)

	if len(lines) != 1 || lines[0] != "valid-line" {
		t.Fatalf("bounded accumulator lines = %#v", lines)
	}
}

func TestCollectorDiscoversOnItsOwnCadenceAndDropsDeletedFiles(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	currentNow := now
	repoRoot := t.TempDir()
	sessionsDir := t.TempDir()
	collector := newCollector(collectorOptions{
		SessionsDir:   sessionsDir,
		RepoRoot:      repoRoot,
		DoneFor:       10 * time.Minute,
		StaleAfter:    30 * time.Minute,
		ScanWindow:    48 * time.Hour,
		DiscoverEvery: 10 * time.Second,
		Now:           func() time.Time { return currentNow },
	})

	if agents, err := collector.refresh(); err != nil || len(agents) != 0 {
		t.Fatalf("initial refresh = %#v, %v", agents, err)
	}

	sessionPath := filepath.Join(sessionsDir, "later.jsonl")
	writeSessionLines(t, sessionPath, []string{
		sessionMetaLine(now, "later", repoRoot, "/root/later", "", "parent", true),
		eventLine(now, map[string]any{"type": "task_started"}),
	}, true)

	if agents, err := collector.refresh(); err != nil || len(agents) != 0 {
		t.Fatalf("early discovery refresh = %#v, %v", agents, err)
	}

	collector.forceDiscovery()
	agents, err := collector.refresh()
	if err != nil || len(agents) != 1 {
		t.Fatalf("forced discovery refresh = %#v, %v", agents, err)
	}

	if err := os.Remove(sessionPath); err != nil {
		t.Fatal(err)
	}
	agents, err = collector.refresh()
	if err != nil {
		t.Fatalf("deleted session produced a permanent read error: %v", err)
	}
	if len(agents) != 0 || len(collector.files) != 0 {
		t.Fatalf("deleted session remains tracked: agents=%#v files=%d", agents, len(collector.files))
	}
}

func TestCollectorThrottlesFailedDiscoveryAttempts(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	currentNow := now
	repoRoot := t.TempDir()
	sessionsDir := filepath.Join(t.TempDir(), "not-created-yet")
	collector := newCollector(collectorOptions{
		SessionsDir:   sessionsDir,
		RepoRoot:      repoRoot,
		DoneFor:       10 * time.Minute,
		StaleAfter:    30 * time.Minute,
		ScanWindow:    48 * time.Hour,
		DiscoverEvery: 10 * time.Second,
		Now:           func() time.Time { return currentNow },
	})

	if _, err := collector.refresh(); err == nil {
		t.Fatal("missing sessions directory did not report a discovery error")
	}
	sessionPath := filepath.Join(sessionsDir, "created-after-error.jsonl")
	writeSessionLines(t, sessionPath, []string{
		sessionMetaLine(now, "created", repoRoot, "/root/created", "", "parent", true),
		eventLine(now, map[string]any{"type": "task_started"}),
	}, true)

	if agents, err := collector.refresh(); err == nil || len(agents) != 0 {
		t.Fatalf("failed discovery was not throttled: agents=%#v err=%v", agents, err)
	}

	currentNow = currentNow.Add(10 * time.Second)
	agents, err := collector.refresh()
	if err != nil || len(agents) != 1 {
		t.Fatalf("retry after discovery interval = %#v, %v", agents, err)
	}
}

func TestCollectorBootstrapsLargeSessionsByScanningBackwardToLifecycle(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	repoRoot := t.TempDir()
	sessionsDir := t.TempDir()
	sessionPath := filepath.Join(sessionsDir, "large.jsonl")
	padding := strings.Repeat("x", int(reverseScanChunkBytes+1024))

	writeSessionLines(t, sessionPath, []string{
		sessionMetaLine(now.Add(-3*time.Minute), "large", repoRoot, "/root/large_review", "", "parent", true),
		padding,
		eventLine(now.Add(-2*time.Minute), map[string]any{
			"type": "task_started", "started_at": now.Add(-2 * time.Minute).Unix(),
		}),
		eventLine(now.Add(-time.Minute), map[string]any{
			"type": "agent_message", "phase": "commentary", "message": "Reviewing after a large tool payload",
		}),
		strings.Repeat("y", int(reverseScanChunkBytes+1024)),
		eventLine(now, map[string]any{"type": "tool_event", "message": "tail has no lifecycle boundary"}),
	}, true)

	collector := newCollector(collectorOptions{
		SessionsDir: sessionsDir,
		RepoRoot:    repoRoot,
		DoneFor:     10 * time.Minute,
		StaleAfter:  30 * time.Minute,
		ScanWindow:  48 * time.Hour,
		Now:         func() time.Time { return now },
	})
	agents, err := collector.refresh()
	if err != nil {
		t.Fatalf("large session refresh returned an error: %v", err)
	}
	if len(agents) != 1 || agents[0].Status != statusRunning {
		t.Fatalf("large session agents = %#v", agents)
	}
	if agents[0].Summary != "Reviewing after a large tool payload" {
		t.Fatalf("large session summary = %q", agents[0].Summary)
	}
	info, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := collector.files[sessionPath].offset; got != info.Size() {
		t.Fatalf("initial offset = %d, want %d", got, info.Size())
	}
}

func TestCollectorCapsInitialReverseScan(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	repoRoot := t.TempDir()
	sessionsDir := t.TempDir()
	sessionPath := filepath.Join(sessionsDir, "capped.jsonl")

	writeSessionLines(t, sessionPath, []string{
		sessionMetaLine(now.Add(-3*time.Minute), "capped", repoRoot, "/root/capped", "", "parent", true),
		eventLine(now.Add(-2*time.Minute), map[string]any{"type": "task_started"}),
		eventLine(now.Add(-time.Minute), map[string]any{
			"type": "agent_message", "phase": "commentary", "message": "This state is outside the scan budget",
		}),
		strings.Repeat("z", int(initialScanLimitBytes+1024)),
		eventLine(now, map[string]any{"type": "tool_event", "message": "bounded tail"}),
	}, true)

	collector := newCollector(collectorOptions{
		SessionsDir: sessionsDir,
		RepoRoot:    repoRoot,
		DoneFor:     10 * time.Minute,
		StaleAfter:  30 * time.Minute,
		ScanWindow:  48 * time.Hour,
		Now:         func() time.Time { return now },
	})
	agents, err := collector.refresh()
	if err != nil {
		t.Fatalf("capped session refresh returned an error: %v", err)
	}
	if len(agents) != 1 || agents[0].Status != statusStale {
		t.Fatalf("out-of-window lifecycle was not preserved as unknown/stale: %#v", agents)
	}
	if agents[0].Summary != "Lifecycle state is outside the startup scan window" {
		t.Fatalf("unknown lifecycle summary = %q", agents[0].Summary)
	}
}

func TestUnknownLifecycleRecoversFromAppendedCompletionAndCommentary(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	repoRoot := t.TempDir()
	sessionsDir := t.TempDir()
	sessionPath := filepath.Join(sessionsDir, "unknown-recovery.jsonl")
	writeSessionLines(t, sessionPath, []string{
		sessionMetaLine(now.Add(-time.Minute), "unknown", repoRoot, "/root/unknown", "", "parent", true),
		strings.Repeat("x", int(initialScanLimitBytes+1024)),
	}, false)

	collector := newCollector(collectorOptions{
		SessionsDir: sessionsDir,
		RepoRoot:    repoRoot,
		DoneFor:     10 * time.Minute,
		StaleAfter:  30 * time.Minute,
		ScanWindow:  48 * time.Hour,
		Now:         func() time.Time { return now },
	})
	agents, err := collector.refresh()
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].Status != statusStale {
		t.Fatalf("unterminated oversized tail was not shown as unknown: %#v", agents)
	}

	appendText(t, sessionPath, "\n"+eventLine(now, map[string]any{
		"type": "task_complete", "completed_at": now.Unix(), "last_agent_message": "Recovered completion",
	})+"\n")
	agents, err = collector.refresh()
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].Status != statusDone || agents[0].Summary != "Recovered completion" {
		t.Fatalf("unknown lifecycle did not accept completion: %#v", agents)
	}

	appendText(t, sessionPath, eventLine(now.Add(time.Second), map[string]any{
		"type": "agent_message", "phase": "commentary", "message": "Recovered follow-up commentary",
	})+"\n")
	agents, err = collector.refresh()
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || agents[0].Status != statusRunning || agents[0].Summary != "Recovered follow-up commentary" {
		t.Fatalf("completed lifecycle did not accept follow-up commentary: %#v", agents)
	}
}

func TestCollectorAcceptsStringSubagentSources(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	repoRoot := t.TempDir()
	sessionsDir := t.TempDir()
	sessionPath := filepath.Join(sessionsDir, "review.jsonl")
	meta := jsonLine(now, "session_meta", map[string]any{
		"id":               "review-1",
		"cwd":              repoRoot,
		"parent_thread_id": "parent-1",
		"source":           map[string]any{"subagent": "review"},
	})
	writeSessionLines(t, sessionPath, []string{
		meta,
		eventLine(now, map[string]any{"type": "task_started"}),
		eventLine(now, map[string]any{
			"type": "agent_message", "phase": "commentary", "message": "Checking repository standards",
		}),
	}, true)

	collector := newCollector(collectorOptions{
		SessionsDir: sessionsDir,
		RepoRoot:    repoRoot,
		DoneFor:     10 * time.Minute,
		StaleAfter:  30 * time.Minute,
		ScanWindow:  48 * time.Hour,
		Now:         func() time.Time { return now },
	})
	agents, err := collector.refresh()
	if err != nil {
		t.Fatalf("string-source refresh returned an error: %v", err)
	}
	if len(agents) != 1 || agents[0].Name != "review" || agents[0].ParentID != "parent-1" {
		t.Fatalf("string-source agents = %#v", agents)
	}
}

func TestFindRepositoryRootAndPathWithinResolveSymlinks(t *testing.T) {
	parent := t.TempDir()
	repoRoot := filepath.Join(parent, "repo")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "linked-repo")
	if err := os.Symlink(repoRoot, link); err != nil {
		t.Fatal(err)
	}

	if got, want := findRepositoryRoot(link), canonicalPath(repoRoot); got != want {
		t.Fatalf("findRepositoryRoot(%q) = %q, want %q", link, got, want)
	}
	if !pathWithin(link, filepath.Join(repoRoot, "nested")) {
		t.Fatal("pathWithin did not treat the symlink and physical repository as the same root")
	}
}

func sessionMetaLine(timestamp time.Time, id, cwd, path, nickname, parent string, subagent bool) string {
	source := any("cli")
	if subagent {
		source = map[string]any{
			"subagent": map[string]any{
				"thread_spawn": map[string]any{
					"parent_thread_id": parent,
					"agent_path":       path,
					"agent_nickname":   nickname,
				},
			},
		}
	}
	return jsonLine(timestamp, "session_meta", map[string]any{
		"id":               id,
		"cwd":              cwd,
		"agent_path":       path,
		"agent_nickname":   nickname,
		"parent_thread_id": parent,
		"source":           source,
	})
}

func eventLine(timestamp time.Time, payload map[string]any) string {
	return jsonLine(timestamp, "event_msg", payload)
}

func jsonLine(timestamp time.Time, itemType string, payload any) string {
	encoded, err := json.Marshal(map[string]any{
		"timestamp": timestamp.Format(time.RFC3339Nano),
		"type":      itemType,
		"payload":   payload,
	})
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func writeSessionLines(t *testing.T, path string, lines []string, finalNewline bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := strings.Join(lines, "\n")
	if finalNewline {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendText(t *testing.T, path, content string) {
	t.Helper()
	handle, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	if _, err := handle.WriteString(content); err != nil {
		t.Fatal(err)
	}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}
