package main

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/infra/store"
)

// mustVNLocation loads the Asia/Ho_Chi_Minh zone (the llm_budget day
// boundary, see ReserveLLMBudget) and fails hard on error instead of
// falling back to UTC: two instances disagreeing on "what day is it" — one
// loads the zone, one silently falls back — would let the daily cap be
// double-spent across the boundary. main's blank time/tzdata import
// guarantees this succeeds regardless of the base image's OS zoneinfo
// package.
func mustVNLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		slog.Error("load Asia/Ho_Chi_Minh timezone", "error", err)
		os.Exit(1)
	}
	return loc
}

// reserveTimeout bounds how long a single budget-reserve DB round trip may
// take. Short on purpose: if the store is struggling, budgetGate must fail
// closed (reject the request) quickly rather than adding a full request's
// worth of latency to every call while the pipeline is blocked behind it.
const reserveTimeout = 250 * time.Millisecond

// exhaustedTTL is how long budgetGate remembers "today's cap is already
// spent" in-process before re-checking Postgres. Without this, a flood of
// requests arriving after the cap is hit would each still take the
// llm_budget row lock (the atomic UPDATE...WHERE still runs and still
// contends even when it matches zero rows) and a connection from the shared
// pool that retrieval also depends on — the safety net would not shed its
// own load. A short TTL bounds how long the gate can be stale after the
// budget is topped up or the day rolls over; it does not need to be
// coordinated across instances — each instance independently re-checking a
// few seconds apart is an acceptable trade for collapsing a flood to ~1 DB
// hit per instance per interval.
const exhaustedTTL = 10 * time.Second

// budgetGate adapts store.ReserveLLMBudget into the analyze.Budget
// interface, closing over the daily capacity and the timezone the day
// boundary is computed in (Asia/Ho_Chi_Minh — see mustVNLocation).
type budgetGate struct {
	store    *store.Store
	capacity int
	loc      *time.Location

	// mu guards exhaustedDay/exhaustedUntil, the short-circuit cache
	// described on exhaustedTTL. Reserve only ever tracks *today's* key —
	// there is no scenario where a caller reads or writes any other day —
	// so two scalars are enough; no map, no per-day accumulation.
	mu             sync.Mutex
	exhaustedDay   string    // "YYYY-MM-DD" this cache entry is for, "" if none
	exhaustedUntil time.Time // until when to skip the DB for exhaustedDay
}

func newBudgetGate(st *store.Store, capacity int, loc *time.Location) *budgetGate {
	return &budgetGate{store: st, capacity: capacity, loc: loc}
}

// Reserve implements analyze.Budget.
func (b *budgetGate) Reserve(ctx context.Context) (bool, error) {
	day := time.Now().In(b.loc)
	key := day.Format("2006-01-02")

	b.mu.Lock()
	stillExhausted := b.exhaustedDay == key && time.Now().Before(b.exhaustedUntil)
	b.mu.Unlock()
	if stillExhausted {
		return false, nil
	}

	reserveCtx, cancel := context.WithTimeout(ctx, reserveTimeout)
	defer cancel()

	ok, err := b.store.ReserveLLMBudget(reserveCtx, day, b.capacity)
	if err != nil {
		return false, err
	}
	if !ok {
		b.mu.Lock()
		b.exhaustedDay, b.exhaustedUntil = key, time.Now().Add(exhaustedTTL)
		b.mu.Unlock()
	}
	return ok, nil
}
