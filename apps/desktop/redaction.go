package main

import (
	"encoding/json"
	"io"
	"regexp"
	"strings"
)

var bearerPattern = regexp.MustCompile(`(?i)(?:bearer|basic)\s+[a-z0-9._~+/=\-]+`)
var escapedQuotePattern = regexp.MustCompile(`\\(["'])`)
var credentialAssignmentPattern = regexp.MustCompile(`(?i)(["']?(?:api[_-]?key|control[_-]?token|gateway[_-]?token|proxy[_-]?(?:username|password|authorization|credentials)|authorization|username|password|access[_-]?token|refresh[_-]?token)["']?\s*[=:]\s*)(?:"(?:\\.|[^"\\])*"|'[^']*'|[^\s,;}\]]+)`)
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

func redactNetworkProfileResponse(body string, secrets, proxySecrets []string) string {
	allSecrets := append(append([]string{}, secrets...), proxySecrets...)
	value, ok := decodeJSON(body)
	profile, object := value.(map[string]any)
	if !ok || !object {
		return redactResponse(body, allSecrets)
	}
	scope, _ := profile["scope"].(string)
	id, _ := profile["id"].(string)
	revision, _ := profile["revision"].(string)
	route, _ := profile["proxy_mode"].(string)
	if (scope != "desktop" && scope != "environment") || id == "" || revision == "" || (route != "direct" && route != "proxy") {
		return redactResponse(body, allSecrets)
	}
	// A short proxy credential must not corrupt user-authored public paths or names.
	publicFields := map[string]bool{
		"id": true, "revision": true, "name": true, "scope": true, "environment_id": true,
		"proxy_mode": true, "proxy_url": true, "proxy_targets": true, "proxy_auth": true,
		"no_proxy": true, "relay_mode": true, "tls_mode": true, "trust_mode": true,
		"ca_cert_path": true, "mirrors": true, "has_credentials": true,
		"credential_revision": true, "tls_risk_accepted": true, "created_at": true,
	}
	for key, child := range profile {
		if publicFields[key] {
			profile[key] = redactValue(child, secrets)
		} else {
			profile[key] = redactValue(map[string]any{key: child}, allSecrets).(map[string]any)[key]
		}
	}
	data, err := json.Marshal(profile)
	if err != nil {
		return redactResponse(body, allSecrets)
	}
	return string(data)
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
			case "apikey", "controltoken", "gatewaytoken", "authorization", "username", "password", "proxyusername", "proxypassword", "proxyauthorization", "proxycredentials", "accesstoken", "refreshtoken":
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
