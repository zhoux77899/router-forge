package netpolicy

import "strings"

// StripNetworkEnvironment returns a new environment slice with host network
// overrides removed, case-insensitively. It never reads or changes os.Environ.
// This also strips registry/index/trusted-host controls for common downloaders
// and language-runtime options capable of injecting proxy or TLS overrides.
// Callers must separately isolate tool config files and explicitly add the
// chosen workload policy; this helper does not configure those tools.
func StripNetworkEnvironment(environment []string) []string {
	clean := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if !isNetworkEnvironment(strings.ToUpper(name)) {
			clean = append(clean, entry)
		}
	}
	return clean
}

func isNetworkEnvironment(name string) bool {
	if strings.Contains(name, "PROXY") || strings.Contains(name, "MIRROR") ||
		strings.Contains(name, "TLS") || strings.Contains(name, "SSL") {
		return true
	}
	for _, token := range strings.Split(name, "_") {
		switch token {
		case "CA", "CACERT", "CACERTS", "CAFILE", "CAPATH", "CAINFO", "CAROOT",
			"CERT", "CERTS", "CERTIFICATE", "CERTIFICATES", "TRUSTSTORE", "KEYSTORE":
			return true
		}
	}
	if strings.HasPrefix(name, "UV_INDEX_") || strings.HasPrefix(name, "CARGO_REGISTRIES_") {
		return true
	}
	switch name {
	case "PIP_INDEX_URL", "PIP_EXTRA_INDEX_URL", "PIP_TRUSTED_HOST", "PIP_FIND_LINKS",
		"UV_INDEX", "UV_DEFAULT_INDEX", "UV_EXTRA_INDEX_URL", "UV_FIND_LINKS", "UV_INSECURE_HOST",
		"NPM_CONFIG_REGISTRY", "NPM_CONFIG_DISTURL",
		"YARN_REGISTRY", "BUN_CONFIG_REGISTRY", "CARGO_REGISTRY_INDEX", "CARGO_HTTP_CHECK_REVOKE",
		"GOINSECURE", "GOSUMDB", "GONOSUMDB", "GODEBUG",
		"NODE_OPTIONS", "JAVA_TOOL_OPTIONS", "_JAVA_OPTIONS", "JDK_JAVA_OPTIONS":
		return true
	}
	return false
}
