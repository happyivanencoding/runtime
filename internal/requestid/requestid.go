package requestid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const Header = "X-Runtime-Request-Id"

var (
	validPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)
	fallbackSeq  atomic.Uint64
)

type contextKey struct{}

func New() string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return "req_" + hex.EncodeToString(raw[:])
	}
	return "req_fallback_" + time.Now().UTC().Format("20060102T150405.000000000") + "_" + strconv.FormatUint(fallbackSeq.Add(1), 36)
}

func Normalize(value string) string {
	value = strings.TrimSpace(value)
	if !validPattern.MatchString(value) {
		return ""
	}
	return value
}

func With(ctx context.Context, id string) context.Context {
	id = Normalize(id)
	if id == "" {
		id = New()
	}
	return context.WithValue(ctx, contextKey{}, id)
}

func From(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(contextKey{}).(string)
	return Normalize(value)
}

func Ensure(ctx context.Context) (context.Context, string) {
	if id := From(ctx); id != "" {
		return ctx, id
	}
	id := New()
	return With(ctx, id), id
}
