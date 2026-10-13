package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"time"
)

// Only numeric, allow-listed restic fields leave the subprocess boundary. File
// names, URLs, current_files and arbitrary tool messages are never published.
type transferProgress struct {
	BytesProcessed int64
	BytesTotal     int64
	FilesProcessed int64
	FilesTotal     int64
	BytesAdded     int64
}
type progressKey struct{}
type progressCallback func(transferProgress)
type progressCommandRunner interface {
	RunProgress(context.Context, string, []string, []string, string, progressCallback) ([]byte, error)
}

func withTransferProgress(ctx context.Context, callback progressCallback) context.Context {
	return context.WithValue(ctx, progressKey{}, callback)
}

func decodeTransferProgress(line []byte) (transferProgress, bool, bool) {
	var event struct {
		MessageType         string `json:"message_type"`
		BytesDone           int64  `json:"bytes_done"`
		TotalBytes          int64  `json:"total_bytes"`
		FilesDone           int64  `json:"files_done"`
		TotalFiles          int64  `json:"total_files"`
		TotalBytesProcessed int64  `json:"total_bytes_processed"`
		TotalFilesProcessed int64  `json:"total_files_processed"`
		DataAdded           int64  `json:"data_added"`
	}
	if json.Unmarshal(line, &event) != nil || event.MessageType != "status" && event.MessageType != "summary" {
		return transferProgress{}, false, false
	}
	p := transferProgress{BytesProcessed: max(0, event.BytesDone), BytesTotal: max(0, event.TotalBytes), FilesProcessed: max(0, event.FilesDone), FilesTotal: max(0, event.TotalFiles), BytesAdded: max(0, event.DataAdded)}
	summary := event.MessageType == "summary"
	if summary {
		p.BytesProcessed = max(0, event.TotalBytesProcessed)
		p.BytesTotal = p.BytesProcessed
		p.FilesProcessed = max(0, event.TotalFilesProcessed)
		p.FilesTotal = p.FilesProcessed
	}
	return p, true, summary
}

type progressBuffer struct {
	output        cappedBuffer
	pending       []byte
	callback      progressCallback
	lastPublished time.Time
}

func (b *progressBuffer) Write(data []byte) (int, error) {
	n := len(data)
	// A malformed line cannot cause an unbounded allocation even if a subprocess
	// emits continuous output without newlines.
	for len(data) > 0 {
		end := bytes.IndexByte(data, '\n')
		amount := len(data)
		if end >= 0 {
			amount = end + 1
		}
		if len(b.pending)+amount > 16<<20 {
			b.output.overflow = true
			b.pending = nil
			data = data[amount:]
			continue
		}
		b.pending = append(b.pending, data[:amount]...)
		data = data[amount:]
		if end >= 0 {
			b.consume(b.pending)
			b.pending = nil
		}
	}
	return n, nil
}
func (b *progressBuffer) consume(line []byte) {
	progress, recognized, summary := decodeTransferProgress(line)
	if recognized {
		if b.callback != nil && (summary || b.lastPublished.IsZero() || time.Since(b.lastPublished) >= time.Second) {
			b.callback(progress)
			b.lastPublished = time.Now()
		}
		if !summary {
			return
		} // Long-running status streams never fill the response buffer.
	}
	_, _ = b.output.Write(line)
}
func (b *progressBuffer) finish() {
	if len(b.pending) > 0 {
		b.consume(b.pending)
		b.pending = nil
	}
}
