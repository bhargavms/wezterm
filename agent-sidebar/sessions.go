package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	reverseScanChunkBytes  = int64(1 << 20)
	initialScanLimitBytes  = int64(8 << 20)
	metadataScanLimitBytes = int64(1 << 20)
	maxJSONLRecordBytes    = 1 << 20
	streamReadBufferBytes  = 64 << 10
)

type agentStatus int

const (
	statusRunning agentStatus = iota
	statusStale
	statusDone
)

type agent struct {
	ID          string
	Path        string
	Name        string
	Nickname    string
	ParentID    string
	CWD         string
	Summary     string
	Status      agentStatus
	StartedAt   time.Time
	UpdatedAt   time.Time
	CompletedAt time.Time
	SourceFile  string
}

type collectorOptions struct {
	SessionsDir   string
	RepoRoot      string
	DoneFor       time.Duration
	StaleAfter    time.Duration
	ScanWindow    time.Duration
	DiscoverEvery time.Duration
	Now           func() time.Time
}

type collector struct {
	options          collectorOptions
	files            map[string]*sessionFile
	lastDiscovery    time.Time
	lastDiscoveryErr error
}

type sessionFile struct {
	path        string
	offset      int64
	ignored     bool
	initialized bool
	isSubagent  bool
	lastMod     time.Time
	agent       agent
	stream      jsonlAccumulator
}

type jsonlAccumulator struct {
	partial    []byte
	discarding bool
}

func newCollector(options collectorOptions) *collector {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.DiscoverEvery <= 0 {
		options.DiscoverEvery = 5 * time.Second
	}
	return &collector{
		options: options,
		files:   make(map[string]*sessionFile),
	}
}

func (c *collector) refresh() ([]agent, error) {
	now := c.options.Now()
	if c.lastDiscovery.IsZero() || now.Sub(c.lastDiscovery) >= c.options.DiscoverEvery {
		c.lastDiscovery = now
		c.lastDiscoveryErr = c.discover()
	}
	var readErrors []error

	for path, file := range c.files {
		if !file.initialized {
			if err := c.initializeFile(file); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					delete(c.files, path)
					continue
				}
				readErrors = append(readErrors, fmt.Errorf("%s: %w", filepath.Base(file.path), err))
				continue
			}
			if !file.initialized {
				continue
			}
		}
		if file.ignored {
			continue
		}
		if err := c.readNewEvents(file); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				delete(c.files, path)
				continue
			}
			readErrors = append(readErrors, fmt.Errorf("%s: %w", filepath.Base(file.path), err))
		}
	}

	agents := make([]agent, 0)
	for _, file := range c.files {
		if !file.isSubagent || file.ignored {
			continue
		}

		current := file.agent
		if current.Status == statusRunning && now.Sub(file.lastMod) > c.options.StaleAfter {
			current.Status = statusStale
		}
		if current.Status == statusDone && now.Sub(current.CompletedAt) > c.options.DoneFor {
			continue
		}
		agents = append(agents, current)
	}

	sort.Slice(agents, func(i, j int) bool {
		if agents[i].Status != agents[j].Status {
			return agents[i].Status < agents[j].Status
		}
		return agents[i].UpdatedAt.After(agents[j].UpdatedAt)
	})

	return agents, errors.Join(append([]error{c.lastDiscoveryErr}, readErrors...)...)
}

func (c *collector) forceDiscovery() {
	c.lastDiscovery = time.Time{}
}

func (c *collector) discover() error {
	cutoff := c.options.Now().Add(-c.options.ScanWindow)
	seen := make(map[string]struct{})
	err := filepath.WalkDir(c.options.SessionsDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.ModTime().Before(cutoff) {
			return nil
		}
		seen[path] = struct{}{}

		tracked, exists := c.files[path]
		if !exists {
			tracked = &sessionFile{path: path, lastMod: info.ModTime()}
			c.files[path] = tracked
			if err := c.initializeFile(tracked); err != nil {
				return err
			}
		} else {
			tracked.lastMod = info.ModTime()
			if !tracked.initialized {
				if err := c.initializeFile(tracked); err != nil {
					return err
				}
			}
			if info.Size() < tracked.offset {
				c.resetFile(tracked)
				if err := c.initializeFile(tracked); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	for path := range c.files {
		if _, exists := seen[path]; !exists {
			delete(c.files, path)
		}
	}
	return nil
}

func (c *collector) initializeFile(file *sessionFile) error {
	handle, err := os.Open(file.path)
	if err != nil {
		return err
	}
	defer handle.Close()

	metaSeen := false
	lineNumber := 0
	if err := walkJSONLLinesForward(handle, metadataScanLimitBytes, func(line []byte) bool {
		lineNumber++
		var item envelope
		if json.Unmarshal(line, &item) == nil && item.Type == "session_meta" {
			metaSeen = true
			c.applySessionMeta(file, item.Payload)
			return true
		}
		return lineNumber >= 8
	}); err != nil {
		return err
	}

	if !metaSeen {
		if info, statErr := handle.Stat(); statErr == nil && info.Size() >= metadataScanLimitBytes {
			file.initialized = true
			file.ignored = true
			file.offset = info.Size()
			return nil
		}
		file.initialized = false
		return nil
	}

	file.initialized = true
	file.stream = jsonlAccumulator{}
	if !file.isSubagent || !pathWithin(c.options.RepoRoot, file.agent.CWD) {
		file.ignored = true
		info, statErr := handle.Stat()
		if statErr == nil {
			file.offset = info.Size()
		}
		return nil
	}

	return c.readInitialEvents(file)
}

func (c *collector) readNewEvents(file *sessionFile) error {
	info, err := os.Stat(file.path)
	if err != nil {
		return err
	}
	file.lastMod = info.ModTime()
	if info.Size() < file.offset {
		c.resetFile(file)
		if err := c.initializeFile(file); err != nil {
			return err
		}
		if file.ignored || !file.initialized {
			return nil
		}
		info, err = os.Stat(file.path)
		if err != nil {
			return err
		}
		file.lastMod = info.ModTime()
	}
	if info.Size() == file.offset {
		return nil
	}

	handle, err := os.Open(file.path)
	if err != nil {
		return err
	}
	defer handle.Close()

	if _, err := handle.Seek(file.offset, io.SeekStart); err != nil {
		return err
	}

	buffer := make([]byte, streamReadBufferBytes)
	for {
		read, readErr := handle.Read(buffer)
		if read > 0 {
			file.offset += int64(read)
			file.stream.feed(buffer[:read], func(line []byte) bool {
				c.applyLine(file, line)
				return false
			})
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}

func walkJSONLLinesForward(reader io.Reader, byteLimit int64, visit func([]byte) bool) error {
	accumulator := jsonlAccumulator{}
	buffer := make([]byte, streamReadBufferBytes)
	remaining := byteLimit

	for remaining > 0 {
		readSize := int(min(int64(len(buffer)), remaining))
		read, err := reader.Read(buffer[:readSize])
		if read > 0 {
			remaining -= int64(read)
			if accumulator.feed(buffer[:read], visit) {
				return nil
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
	return nil
}

func (accumulator *jsonlAccumulator) feed(data []byte, visit func([]byte) bool) bool {
	for len(data) > 0 {
		newline := bytes.IndexByte(data, '\n')
		complete := newline >= 0
		fragment := data
		if complete {
			fragment = data[:newline]
		}

		if accumulator.discarding {
			if !complete {
				return false
			}
			accumulator.discarding = false
		} else if len(accumulator.partial)+len(fragment) > maxJSONLRecordBytes {
			accumulator.partial = nil
			accumulator.discarding = !complete
		} else {
			accumulator.partial = append(accumulator.partial, fragment...)
			if complete {
				if len(accumulator.partial) > 0 && visit(accumulator.partial) {
					accumulator.partial = nil
					return true
				}
				accumulator.partial = nil
			}
		}

		if !complete {
			return false
		}
		data = data[newline+1:]
	}
	return false
}

func (c *collector) readInitialEvents(file *sessionFile) error {
	handle, err := os.Open(file.path)
	if err != nil {
		return err
	}
	defer handle.Close()

	info, err := handle.Stat()
	if err != nil {
		return err
	}
	file.lastMod = info.ModTime()

	completeEnd, foundCompleteLine, err := lastCompleteJSONLEnd(handle, info.Size())
	if err != nil {
		return err
	}
	if !foundCompleteLine {
		file.offset = info.Size()
		file.stream.discarding = true
		c.markLifecycleUnknown(file)
		return nil
	}
	file.offset = completeEnd
	if completeEnd == 0 {
		return nil
	}

	state := turnBootstrap{}
	scanStart := max(int64(0), completeEnd-initialScanLimitBytes)
	err = walkJSONLLinesBackward(handle, scanStart, completeEnd, func(line []byte) bool {
		return state.consume(line)
	})
	if !state.apply(&file.agent) {
		c.markLifecycleUnknown(file)
	}
	return err
}

func (c *collector) markLifecycleUnknown(file *sessionFile) {
	file.agent.Status = statusStale
	file.agent.Summary = "Lifecycle state is outside the startup scan window"
	file.agent.UpdatedAt = file.lastMod
}

func lastCompleteJSONLEnd(file *os.File, size int64) (int64, bool, error) {
	searchStart := max(int64(0), size-initialScanLimitBytes)
	for end := size; end > searchStart; {
		start := max(searchStart, end-reverseScanChunkBytes)
		data, err := readFileSegment(file, start, end-start)
		if err != nil {
			return 0, false, err
		}
		if newline := bytes.LastIndexByte(data, '\n'); newline >= 0 {
			return start + int64(newline) + 1, true, nil
		}
		end = start
	}
	return 0, false, nil
}

func walkJSONLLinesBackward(file *os.File, start, end int64, visit func([]byte) bool) error {
	var carry []byte
	discardOversized := false
	for end > start {
		chunkStart := max(start, end-reverseScanChunkBytes)
		chunk, err := readFileSegment(file, chunkStart, end-chunkStart)
		if err != nil {
			return err
		}

		data := chunk
		if discardOversized {
			lastNewline := bytes.LastIndexByte(chunk, '\n')
			if lastNewline < 0 {
				end = chunkStart
				continue
			}
			data = chunk[:lastNewline+1]
			discardOversized = false
		} else if len(carry) > 0 {
			data = make([]byte, 0, len(chunk)+len(carry))
			data = append(data, chunk...)
			data = append(data, carry...)
		}

		processFrom := 0
		if chunkStart > 0 {
			newline := bytes.IndexByte(data, '\n')
			if newline < 0 {
				if len(data) > maxJSONLRecordBytes {
					carry = nil
					discardOversized = true
				} else {
					carry = append(carry[:0], data...)
				}
				end = chunkStart
				continue
			}
			if newline+1 > maxJSONLRecordBytes {
				carry = nil
				discardOversized = true
			} else {
				carry = append(carry[:0], data[:newline+1]...)
			}
			processFrom = newline + 1
		}

		lines := bytes.Split(data[processFrom:], []byte{'\n'})
		for index := len(lines) - 1; index >= 0; index-- {
			if len(lines[index]) > 0 && len(lines[index]) <= maxJSONLRecordBytes && visit(lines[index]) {
				return nil
			}
		}
		end = chunkStart
	}
	return nil
}

func readFileSegment(file *os.File, offset, size int64) ([]byte, error) {
	if size <= 0 {
		return nil, nil
	}
	data := make([]byte, int(size))
	read, err := file.ReadAt(data, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return data[:read], nil
}

func (c *collector) resetFile(file *sessionFile) {
	file.offset = 0
	file.ignored = false
	file.initialized = false
	file.isSubagent = false
	file.agent = agent{}
	file.stream = jsonlAccumulator{}
}

func pathWithin(root, candidate string) bool {
	if root == "" || candidate == "" {
		return false
	}
	relative, err := filepath.Rel(canonicalPath(root), canonicalPath(candidate))
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
