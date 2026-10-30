package main

import (
	"encoding/json"
	"io"
	"regexp"
	"strings"
)

var bearerPattern = regexp.MustCompile(`(?i)bearer\s+[a-z0-9._~+/=\-]+`)
var escapedQuotePattern = regexp.MustCompile(`\\(["'])`)
var credentialAssignmentPattern = regexp.MustCompile(`(?i)(["']?(?:api[_-]?key|control[_-]?token|gateway[_-]?token|authorization|password|access[_-]?token|refresh[_-]?token)["']?\s*[=:]\s*)(?:"(?:\\.|[^"\\])*"|'[^']*'|[^\s,;}\]]+)`)
var urlCredentialPattern = regexp.MustCompile(`(?i)(https?://)[^/@\s]+:[^/@\s]+@`)

func decodeJSON(body string) (any, bool) {
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, false
	}
	return value, true
}

func redactResponse(body string, secrets []string) string {
	if value, ok := decodeJSON(body); ok {
		if data, err := json.Marshal(redactValue(value, secrets)); err == nil {
			return string(data)
		}
	}
	return redactText(body, secrets)
}

func redactText(text string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[redacted]")
		}
	}
	text = escapedQuotePattern.ReplaceAllString(text, "$1")
	text = bearerPattern.ReplaceAllString(text, "Bearer [redacted]")
	text = credentialAssignmentPattern.ReplaceAllString(text, "$1[redacted]")
	return urlCredentialPattern.ReplaceAllString(text, "$1[redacted]@")
}

func redactValue(value any, secrets []string) any {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			switch strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(key)) {
			case "apikey", "controltoken", "gatewaytoken", "authorization", "password", "accesstoken", "refreshtoken":
				v[key] = "[redacted]"
			default:
				v[key] = redactValue(child, secrets)
			}
		}
	case []any:
		for index, child := range v {
			v[index] = redactValue(child, secrets)
		}
	case string:
		if embedded, ok := decodeJSON(v); ok {
			switch embedded.(type) {
			case map[string]any, []any:
				if data, err := json.Marshal(redactValue(embedded, secrets)); err == nil {
					return string(data)
				}
			}
		}
		return redactText(v, secrets)
	}
	return value
}
