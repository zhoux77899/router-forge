package gateway

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/zhoux77899/router-forge/internal/domain"
)

type route struct {
	kind, path, responseID string
	protocol               domain.Protocol
	query                  url.Values
}

func nativeRoute(r *http.Request) (route, int, string) {
	if r.URL.RawPath != "" || strings.Contains(r.URL.Path, "//") {
		return route{}, http.StatusBadRequest, "invalid_path"
	}
	target := route{kind: "inference", path: r.URL.Path}
	method := http.MethodPost
	switch r.URL.Path {
	case "/v1/chat/completions":
		target.protocol = domain.ChatCompletions
	case "/v1/responses":
		target.protocol = domain.Responses
	case "/v1/messages":
		target.protocol = domain.AnthropicMessages
	default:
		if !strings.HasPrefix(r.URL.Path, "/v1/responses/") {
			return route{}, http.StatusNotFound, "unsupported_path"
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/responses/"), "/")
		if !validResponseID(parts[0]) {
			return route{}, http.StatusBadRequest, "invalid_response_id"
		}
		target.protocol, target.responseID = domain.Responses, parts[0]
		switch {
		case len(parts) == 1:
			target.kind, method = "retrieve", http.MethodGet
		case len(parts) == 2 && parts[1] == "cancel":
			target.kind = "cancel"
		default:
			return route{}, http.StatusNotFound, "unsupported_path"
		}
	}
	if r.Method != method {
		return target, http.StatusMethodNotAllowed, "unsupported_method"
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return target, http.StatusBadRequest, "unsupported_query"
	}
	for key, values := range query {
		if target.kind != "retrieve" {
			return target, http.StatusBadRequest, "unsupported_query"
		}
		switch key {
		case "include[]":
			if len(values) > 32 {
				return target, http.StatusBadRequest, "unsupported_query"
			}
			for _, value := range values {
				if !safeIdentifier(value, 128) {
					return target, http.StatusBadRequest, "unsupported_query"
				}
			}
		case "stream":
			if len(values) != 1 || (values[0] != "true" && values[0] != "false") {
				return target, http.StatusBadRequest, "unsupported_query"
			}
		case "starting_after":
			if len(values) != 1 {
				return target, http.StatusBadRequest, "unsupported_query"
			}
			if value, err := strconv.ParseInt(values[0], 10, 64); err != nil || value < 0 {
				return target, http.StatusBadRequest, "unsupported_query"
			}
		default:
			return target, http.StatusBadRequest, "unsupported_query"
		}
	}
	target.query = query
	return target, 0, ""
}

func validResponseID(id string) bool { return safeIdentifier(id, 256) && id != "." && id != ".." }
