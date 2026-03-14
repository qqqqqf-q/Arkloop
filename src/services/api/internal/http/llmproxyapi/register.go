package llmproxyapi

import nethttp "net/http"

func RegisterRoutes(mux *nethttp.ServeMux, deps Deps) {
	mux.HandleFunc("/v1/llm-proxy/chat/completions", chatCompletionsEntry(deps))
}
