package limitedio

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

var ErrContentTooLarge = errors.New("content too large")

type LimitError struct {
	MaxBytes int64
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("%s: maximum is %d bytes", ErrContentTooLarge, e.MaxBytes)
}

func (e *LimitError) Unwrap() error {
	return ErrContentTooLarge
}

func NewLimitError(maxBytes int64) error {
	return &LimitError{MaxBytes: maxBytes}
}

func MaxBytes(err error) (int64, bool) {
	var limitErr *LimitError
	if !errors.As(err, &limitErr) {
		return 0, false
	}
	return limitErr.MaxBytes, true
}

func CheckString(content string, maxBytes int64) error {
	if int64(len(content)) > maxBytes {
		return NewLimitError(maxBytes)
	}
	return nil
}

func ReadFile(path string, maxBytes int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxBytes {
		return nil, NewLimitError(maxBytes)
	}

	content, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maxBytes {
		return nil, NewLimitError(maxBytes)
	}
	return content, nil
}

// Buffer retains at most MaxBytes while reporting successful writes to its
// producer. This lets subprocess pipes continue draining without allowing
// unbounded memory growth.
type Buffer struct {
	mu       sync.Mutex
	content  bytes.Buffer
	maxBytes int64
	exceeded bool
}

func NewBuffer(maxBytes int64) *Buffer {
	return &Buffer{maxBytes: maxBytes}
}

func (b *Buffer) Write(content []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	written := len(content)
	remaining := b.maxBytes - int64(b.content.Len())
	if remaining <= 0 {
		if len(content) > 0 {
			b.exceeded = true
		}
		return written, nil
	}
	if int64(len(content)) > remaining {
		_, _ = b.content.Write(content[:remaining])
		b.exceeded = true
		return written, nil
	}
	_, _ = b.content.Write(content)
	return written, nil
}

func (b *Buffer) WriteString(content string) (int, error) {
	return b.Write([]byte(content))
}

func (b *Buffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.content.Bytes()...)
}

func (b *Buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.content.String()
}

func (b *Buffer) Err() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.exceeded {
		return NewLimitError(b.maxBytes)
	}
	return nil
}
