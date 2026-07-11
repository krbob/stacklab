package requestid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"sync/atomic"
	"time"
)

const Header = "X-Request-ID"

var (
	validID         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	fallbackCounter atomic.Uint64
)

type contextKey struct{}

// Resolve accepts a safe caller-supplied identifier or creates a new one.
// Limiting the character set keeps values safe to echo in headers and logs.
func Resolve(candidate string) string {
	if validID.MatchString(candidate) {
		return candidate
	}
	return New()
}

func New() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err == nil {
		return "req_" + hex.EncodeToString(value[:])
	}
	return fmt.Sprintf("req_%x_%x", time.Now().UTC().UnixNano(), fallbackCounter.Add(1))
}

func WithContext(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

func FromContext(ctx context.Context) string {
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}
