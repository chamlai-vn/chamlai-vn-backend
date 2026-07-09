package main

import (
	"log"
	"sync"
)

// tally is a concurrency-safe new/skip/error counter shared by both modes'
// worker pools.
type tally struct {
	mu                sync.Mutex
	nNew, nSkip, nErr int
}

func (t *tally) totals() (int, int, int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.nNew, t.nSkip, t.nErr
}
func (t *tally) skip(format string, args ...any) {
	t.mu.Lock()
	t.nSkip++
	t.mu.Unlock()
	log.Printf(format, args...)
}
func (t *tally) fail(format string, args ...any) {
	t.mu.Lock()
	t.nErr++
	t.mu.Unlock()
	log.Printf(format, args...)
}
func (t *tally) ok(format string, args ...any) {
	t.mu.Lock()
	t.nNew++
	t.mu.Unlock()
	log.Printf(format, args...)
}
