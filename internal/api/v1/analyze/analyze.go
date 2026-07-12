package analyze

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/bind"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/problem"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/respond"
)

// Routes returns this package's routes as their own sub-router, so the
// package owns its URL structure and error-wrapping instead of router.go
// listing them by hand. The parent router decides the mount point (see
// NewRouter, which mounts this at "/v1/analyze"); POST "/" here is that
// mount point's root.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/", problem.Handler(h.Handle))
	return r
}

// Handle scores a suspicious message. The pipeline is three steps — the
// analyzer does NOT retrieve — mirroring cmd/seed: decode → validate →
// budget.Reserve (global daily cap on the paid pipeline) →
// retriever.HybridSearch (vector + keyword, RRF-fused, reranked) →
// analyzer.Score. The budget gate runs before HybridSearch because
// retrieval already calls the paid embedder, not just Score. The verdict is
// analyzer.AnalysisResult, returned as-is: its JSON tags already are the
// public response shape, so no separate response DTO. Errors are returned
// rather than written directly — problem.Handler (mounted in the router)
// translates them to application/problem+json.
//
// @Summary      Score a message for scam risk
// @Description  Retrieves similar known scam patterns and asks the LLM to score the message red/yellow/green.
// @Tags         analyze
// @Accept       json
// @Produce      json
// @Param        request  body      Request                 true  "Message to score"
// @Success      200      {object}  analyzer.AnalysisResult
// @Failure      400      {object}  problem.Problem  "malformed body, empty text, or body too large"
// @Failure      429      {object}  problem.Problem  "rate limited, or the daily budget for paid calls is exhausted"
// @Failure      500      {object}  problem.Problem  "retrieval or scoring failed"
// @Failure      503      {object}  problem.Problem  "budget could not be verified; request rejected to fail closed"
// @Router       /v1/analyze [post]
func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) error {
	req, err := bind.JSON[Request](r)
	if err != nil {
		return err
	}

	text := strings.TrimSpace(req.Text)
	if text == "" {
		return problem.BadRequest("text không được rỗng")
	}

	ctx := r.Context()

	// Budget gate BEFORE retrieval: HybridSearch already calls the paid
	// embedder, so the wallet safety net must cover it too, not just Score.
	ok, err := h.budget.Reserve(ctx)
	if err != nil {
		return problem.Unavailable().WithErr(fmt.Errorf("reserve llm budget: %w", err))
	}
	if !ok {
		w.Header().Set("Retry-After", "3600")
		return problem.TooManyRequests("hệ thống đang quá tải, vui lòng thử lại sau")
	}

	chunks, err := h.retriever.HybridSearch(ctx, text, h.topK)
	if err != nil {
		return problem.Internal().WithErr(fmt.Errorf("retrieve scam patterns: %w", err))
	}

	result, err := h.scorer.Score(ctx, text, chunks)
	if err != nil {
		return problem.Internal().WithErr(fmt.Errorf("score message: %w", err))
	}

	respond.JSON(w, http.StatusOK, result)
	return nil
}
