package netpolicy

import (
	"os"
	"reflect"
	"testing"
)

func TestStripNetworkEnvironmentNamespacesAndIsolation(t *testing.T) {
	t.Setenv("HTTP_PROXY", "host-marker")
	retained := []string{
		"PATH=D:\\tools", "LOCALAPPDATA=D:\\profile", "MY_CAUTION=keep", "PIP_NO_CACHE_DIR=1",
		"APP_DATA=proxy mirror CA TLS are values, not variable names", "EMPTY=", "NO_EQUALS", "=C:=D:\\work",
	}
	removed := []string{
		"http_proxy=secret", "hTtPs_PrOxY=secret", "ALL_PROXY=secret", "no_proxy=secret",
		"GOPROXY=secret", "GIT_PROXY_COMMAND=secret", "CUSTOM_PROXY_URL=secret",
		"APT_MIRROR=secret", "pypi_mirror=secret", "NVM_NODEJS_ORG_MIRROR=secret", "UV_PYTHON_INSTALL_MIRROR=secret",
		"SSL_CERT_FILE=secret", "ssl_cert_dir=secret", "REQUESTS_CA_BUNDLE=secret", "CURL_CA_BUNDLE=secret",
		"NODE_EXTRA_CA_CERTS=secret", "npm_config_cafile=secret", "GIT_SSL_CAINFO=secret", "CA_BUNDLE=secret",
		"CAFILE=secret", "CAPATH=secret", "CAROOT=secret", "CERTIFICATES_DIR=secret", "PIP_CERT=secret",
		"GIT_SSL_NO_VERIFY=secret", "node_tls_reject_unauthorized=secret", "UV_NATIVE_TLS=secret", "MY_TLS_MODE=secret",
		"PIP_INDEX_URL=secret", "PIP_EXTRA_INDEX_URL=secret", "PIP_TRUSTED_HOST=secret", "PIP_FIND_LINKS=secret",
		"UV_INDEX_URL=secret", "UV_DEFAULT_INDEX=secret", "UV_INDEX_PRIVATE_USERNAME=secret", "UV_INSECURE_HOST=secret",
		"npm_config_registry=secret", "NPM_CONFIG_DISTURL=secret", "YARN_REGISTRY=secret", "BUN_CONFIG_REGISTRY=secret",
		"CARGO_HTTP_CAINFO=secret", "CARGO_REGISTRIES_PRIVATE_INDEX=secret", "GOINSECURE=secret", "GODEBUG=secret",
		"NODE_OPTIONS=secret", "JAVA_TOOL_OPTIONS=secret", "_JAVA_OPTIONS=secret", "JDK_JAVA_OPTIONS=secret",
	}
	input := append(append([]string{}, retained...), removed...)
	before := append([]string{}, input...)
	actual := StripNetworkEnvironment(input)
	if !reflect.DeepEqual(actual, retained) {
		t.Fatal("network namespaces were retained or unrelated entries removed")
	}
	if !reflect.DeepEqual(input, before) || os.Getenv("HTTP_PROXY") != "host-marker" {
		t.Fatal("helper mutated its input or the host environment")
	}
	actual[0] = "changed output"
	if input[0] != retained[0] {
		t.Fatal("filtered environment aliases the supplied slice")
	}
	if len(StripNetworkEnvironment(nil)) != 0 {
		t.Fatal("empty environment unexpectedly gained defaults")
	}
}
