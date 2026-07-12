package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ReserveLLMBudget tries to claim one slot of the daily paid-pipeline budget
// for day (only its date component is used) and reports whether it
// succeeded. ok is false once count has reached capacity for that day — the
// caller must not proceed to any paid call (Voyage/Claude) in that case.
//
// The reserve is a single atomic statement: INSERT the day's row with
// count=1, or on conflict increment it — but only if the current count is
// still below capacity. Under Postgres's default READ COMMITTED isolation,
// a conflicting UPDATE takes a row lock and then re-reads the latest
// COMMITTED value of that row (the EvalPlanQual path) before evaluating the
// WHERE/SET clauses — not a snapshot taken at statement start. So concurrent
// reservers serialize on that row lock and each sees the true count left by
// whoever committed just before it: no lost updates, and the WHERE clause
// makes it impossible for count to exceed capacity even under heavy
// concurrency. When the WHERE clause is false (already at capacity), the
// UPDATE matches zero rows, RETURNING produces nothing, and Scan reports
// pgx.ErrNoRows — that is the normal, expected "budget exhausted" signal,
// not an error.
//
// This must remain a single standalone statement (relies on implicit
// autocommit via QueryRow). Do not fold it into a larger transaction that
// also runs the paid pipeline — that would hold the row lock for the
// duration of a slow LLM call and serialize every other reserver behind it.
func (s *Store) ReserveLLMBudget(ctx context.Context, day time.Time, capacity int) (ok bool, err error) {
	const q = `
		INSERT INTO llm_budget (day, count) VALUES ($1, 1)
		ON CONFLICT (day) DO UPDATE
			SET count = llm_budget.count + 1, updated_at = now()
			WHERE llm_budget.count < $2
		RETURNING count`

	var count int
	err = s.pool.QueryRow(ctx, q, day.Format("2006-01-02"), capacity).Scan(&count)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: reserve llm budget: %w", err)
	}
	return true, nil
}
