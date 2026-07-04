package analyze

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/bind"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/problem"
	"github.com/chamlai-vn/chamlai-vn-backend/internal/api/respond"
)

// Handle scores a suspicious message. The pipeline is two steps — the
// analyzer does NOT retrieve — mirroring cmd/seed: decode → validate →
// retriever.HybridSearch (vector + keyword, RRF-fused, reranked) →
// analyzer.Score. The verdict is analyzer.AnalysisResult,
// returned as-is: its JSON tags already are the public response shape, so no
// separate response DTO. Errors are returned rather than written directly —
// problem.Handler (mounted in the router) translates them to
// application/problem+json.
//
// @Summary      Score a message for scam risk
// @Description  Retrieves similar known scam patterns and asks the LLM to score the message red/yellow/green.
// @Tags         analyze
// @Accept       json
// @Produce      json
// @Param        request  body      Request                 true  "Message to score"
// @Success      200      {object}  analyzer.AnalysisResult
// @Failure      400      {object}  problem.Problem  "malformed body, empty text, or body too large"
// @Failure      500      {object}  problem.Problem  "retrieval or scoring failed"
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
