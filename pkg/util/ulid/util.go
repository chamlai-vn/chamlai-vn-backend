package ulidutil

import (
	"crypto/rand"
	"io"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

// NewString returns an ULID as a string, with the current time. It is safe for concurrent use
func NewString() string {
	return ulid.MustNew(ulid.Timestamp(time.Now().UTC()), SecureEntropy()).String()
}

// NewStringWithReverseTime returns an ULID as a string, with the MaxTime - current time.
// This will support sorting by time in reverse order - chat message feature.
func NewStringWithReverseTime() string {
	return ulid.MustNew(ulid.MaxTime()-ulid.Timestamp(time.Now().UTC()), SecureEntropy()).String()
}

// NewStringWithTime returns an ULID as a string, with the given time. Panic on failure.
func NewStringWithTime(t time.Time) string {
	return ulid.MustNew(ulid.Timestamp(t), SecureEntropy()).String()
}

var (
	secureEntropy     io.Reader
	secureEntropyOnce sync.Once
)

// SecureEntropy returns a thread-safe per process monotonically increasing secure entropy source
func SecureEntropy() io.Reader {
	secureEntropyOnce.Do(func() {
		secureEntropy = &ulid.LockedMonotonicReader{
			MonotonicReader: ulid.Monotonic(rand.Reader, 0),
		}
	})
	return secureEntropy
}
