-- +goose Up
-- Global daily LLM/paid-pipeline call counter. One row per calendar day
-- (Asia/Ho_Chi_Minh, computed by the caller). Survives restarts and is
-- shared across instances via Postgres — the wallet safety net for
-- /v1/analyze (see docs/plans/2026-07-11-001-feat-rate-limit-budget-cap-plan.md).
-- ~365 rows/year at a few dozen bytes each; no retention job needed.
CREATE TABLE llm_budget (
    day        DATE        PRIMARY KEY,
    count      INTEGER     NOT NULL DEFAULT 0 CHECK (count >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS llm_budget;
