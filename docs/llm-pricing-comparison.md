# LLM Pricing Comparison — Cheap Model Candidates

Research date: **06/07/2026**

| Model | Input ($/1M tokens) | Output ($/1M tokens) | Cached/Context caching input | Context window |
|---|---|---|---|---|
| **Gemini 3.5 Flash** | $1.50 | $9.00 (incl. thinking tokens) | $0.15/1M + $1.00/1M/hour storage | Not listed on pricing page — check [models docs](https://ai.google.dev/gemini-api/docs/models) |
| **Claude Haiku 4.5** | $1.00 | $5.00 | — | 200K |
| **GPT-5.4 mini** | $0.75 (batch: $0.375) | $4.50 (batch: $2.25) | $0.075 (batch: $0.0375) | Not listed on pricing page |

## Sources
- Gemini 3.5 Flash: https://ai.google.dev/gemini-api/docs/pricing
- Claude Haiku 4.5: https://platform.claude.com/docs/en/about-claude/pricing
- GPT-5.4 mini: https://developers.openai.com/api/docs/pricing

## Notes
- On pure per-token price, GPT-5.4 mini is the cheapest for both input and output, followed by Claude Haiku 4.5, then Gemini 3.5 Flash.
- Gemini 3.5 Flash's output price includes "thinking tokens," so effective cost for reasoning-heavy tasks may run higher than the raw output rate.
- GPT-5.4 mini's cached-input rate ($0.075/1M) is the cheapest caching option among the three if your workload has a large repeated prompt prefix.
- Context window sizes for Gemini 3.5 Flash and GPT-5.4 mini weren't listed on their pricing pages directly — only Haiku 4.5's 200K is confirmed from Anthropic's docs.
