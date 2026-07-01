package api

// AnalyzeRequest is the POST /analyze request body.
type AnalyzeRequest struct {
	Text string `json:"text"`
}

// errorResponse is the JSON envelope returned for 4xx/5xx responses. Messages
// are user-facing Vietnamese and intentionally coarse — they never leak
// internal error detail to the client.
type errorResponse struct {
	Error string `json:"error"`
}
