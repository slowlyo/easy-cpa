package backend

import (
	"strings"
	"sync"
	"time"
)

// LogBuffer 维护固定长度的内存日志缓冲。
type LogBuffer struct {
	limit int
	mu    sync.RWMutex
	items []LogEntry
}

// NewLogBuffer 创建日志缓冲。
func NewLogBuffer(limit int) *LogBuffer {
	return &LogBuffer{limit: limit}
}

// Append 追加一条日志。
func (b *LogBuffer) Append(source, message string) LogEntry {
	entry := LogEntry{
		Timestamp: time.Now(),
		Source:    source,
		Message:   strings.TrimSpace(message),
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if entry.Message == "" {
		return entry
	}
	b.items = append(b.items, entry)
	if len(b.items) > b.limit {
		b.items = append([]LogEntry(nil), b.items[len(b.items)-b.limit:]...)
	}
	return entry
}

// List 返回日志副本。
func (b *LogBuffer) List() []LogEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return append([]LogEntry(nil), b.items...)
}
