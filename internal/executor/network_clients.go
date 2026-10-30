package executor

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zhoux77899/router-forge/internal/domain"
	"github.com/zhoux77899/router-forge/internal/netpolicy"
)

func explicitNetworkEnvironment(environment []string) map[string]string {
	out := map[string]string{}
	for _, entry := range environment {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		upper := strings.ToUpper(key)
		if len(netpolicy.StripNetworkEnvironment([]string{entry})) == 0 || upper == "ROUTER_FORGE_NETWORK_STAGE" || upper == "ROUTER_FORGE_NETWORK_BIN" || upper == "GIT_CONFIG_NOSYSTEM" || upper == "GIT_CONFIG_GLOBAL" || upper == "PIP_CONFIG_FILE" || upper == "UV_NO_CONFIG" || upper == "NPM_CONFIG_USERCONFIG" || upper == "NPM_CONFIG_GLOBALCONFIG" || upper == "APT_CONFIG" || upper == "CURL_HOME" || upper == "WGETRC" || upper == "PYTHONPATH" {
			out[key] = value
		}
	}
	return out
}

func aptNetworkConfiguration(cfg *nativeStageNetwork) string {
	text := "Dir::Etc::parts \"-\";\nDir::Etc::main \"-\";\n"
	if cfg.Profile.Mirrors.APT.Mode == domain.NetworkCustom {
		text += "Dir::Etc::sourcelist \"" + path.Join(path.Dir(cfg.Env["APT_CONFIG"]), "apt.list") + "\";\nDir::Etc::sourceparts \"-\";\n"
	}
	for _, scheme := range []string{"http", "https"} {
		proxy := cfg.Env[strings.ToUpper(scheme)+"_PROXY"]
		if proxy == "" {
			proxy = "DIRECT"
		}
		text += "Acquire::" + scheme + "::Proxy \"" + proxy + "\";\n"
	}
	verified := "true"
	if cfg.Profile.TLSMode == domain.NetworkSkipTLS {
		verified = "false"
	}
	text += "Acquire::https::Verify-Peer \"" + verified + "\";\nAcquire::https::Verify-Host \"" + verified + "\";\n"
	if cfg.Profile.TLSMode == domain.NetworkVerifyTLS && cfg.Profile.TrustMode == domain.NetworkCustomCA {
		text += "Acquire::https::CaInfo \"" + cfg.Env["SSL_CERT_FILE"] + "\";\n"
	}
	return text
}

func networkClientArgumentGuard(tool string, strict bool) string {
	guard := `rf_reject() { printf '%s\n' 'explicit network policy rejects conflicting client options' >&2; exit 78; }
`
	strictValue := "0"
	if strict {
		strictValue = "1"
	}
	guard += "rf_strict=" + strictValue + "\n"
	switch tool {
	case "curl":
		guard += `for rf_arg do
  rf_flag=${rf_arg%%=*}
  case "$rf_flag" in
    --proxy|--noproxy|--preproxy|--proxy-user|--proxy-header|--config|--cacert|--capath|--proxy-cacert|--proxy-capath|--socks4|--socks4a|--socks5|--socks5-hostname) rf_reject ;;
    --insecure|--proxy-insecure) [ "$rf_strict" = 0 ] || rf_reject ;;
    --*) ;;
    -*)
      rf_short=${rf_arg#-}
      while [ -n "$rf_short" ]; do
        rf_char=${rf_short%"${rf_short#?}"}
        rf_short=${rf_short#?}
        case "$rf_char" in
          K|x|U) rf_reject ;;
          k) [ "$rf_strict" = 0 ] || rf_reject ;;
          d|D|e|E|F|H|m|o|r|t|u|w|z) break ;;
        esac
      done ;;
  esac
done
`
	case "wget":
		guard += `for rf_arg do
  rf_flag=${rf_arg%%=*}
  case "$rf_flag" in
    --execute|--config|--proxy|--no-proxy|--ca-certificate|--ca-directory) rf_reject ;;
    --no-check-certificate) [ "$rf_strict" = 0 ] || rf_reject ;;
    --check-certificate) case "$rf_arg" in *=off|*=false) [ "$rf_strict" = 0 ] || rf_reject ;; esac ;;
    --*) ;;
    -*)
      rf_short=${rf_arg#-}
      while [ -n "$rf_short" ]; do
        rf_char=${rf_short%"${rf_short#?}"}
        rf_short=${rf_short#?}
        case "$rf_char" in
          e|Y) rf_reject ;;
          O|o|a|t|T|U|P|w|Q|l|n) break ;;
        esac
      done ;;
  esac
done
`
	case "git":
		guard += `rf_config_next=0
for rf_arg do
  rf_config=
  if [ "$rf_config_next" = 1 ]; then rf_config=$rf_arg; rf_config_next=0; fi
  case "$rf_arg" in
    --config-env*) rf_reject ;;
    -c) rf_config_next=1 ;;
    -c*) rf_config=${rf_arg#-c} ;;
  esac
  if [ -n "$rf_config" ]; then
    rf_key=$(printf '%s' "${rf_config%%=*}" | /usr/bin/tr '[:upper:]' '[:lower:]')
    case "$rf_key" in http.*|https.*|core.gitproxy|include.*|includeif.*|url.*) rf_reject ;; esac
  fi
done
[ "$rf_config_next" = 0 ] || rf_reject
`
	}
	return guard
}

func writeNetworkClientWrappers(cfg *nativeStageNetwork, container bool) error {
	if runtime.GOOS == "windows" && !container {
		return nil
	}
	bin := filepath.Join(cfg.directory, "bin")
	if err := privateDir(bin); err != nil {
		return errors.New("could not reserve owned downloader wrappers")
	}
	for _, tool := range []string{"curl", "wget", "git"} {
		args := []string{}
		switch tool {
		case "curl":
			args = append(args, "--disable")
			if cfg.Profile.TLSMode == domain.NetworkSkipTLS {
				args = append(args, "--insecure", "--proxy-insecure")
			} else if cfg.Profile.TrustMode == domain.NetworkCustomCA {
				args = append(args, "--cacert", cfg.Env["SSL_CERT_FILE"], "--proxy-cacert", cfg.Env["SSL_CERT_FILE"])
			}
		case "wget":
			args = append(args, "--no-config")
			if cfg.Profile.TLSMode == domain.NetworkSkipTLS {
				args = append(args, "--no-check-certificate")
			} else if cfg.Profile.TrustMode == domain.NetworkCustomCA {
				args = append(args, "--ca-certificate="+cfg.Env["SSL_CERT_FILE"])
			}
		case "git":
			verify := "true"
			if cfg.Profile.TLSMode == domain.NetworkSkipTLS {
				verify = "false"
			}
			args = append(args, "-c", "http.sslVerify="+verify)
			if cfg.Profile.TLSMode == domain.NetworkVerifyTLS && cfg.Profile.TrustMode == domain.NetworkCustomCA {
				args = append(args, "-c", "http.sslCAInfo="+cfg.Env["SSL_CERT_FILE"])
			}
		}
		quoted := []string{}
		for _, arg := range args {
			quoted = append(quoted, shellQuote(arg))
		}
		content := "#!/bin/sh\n" + networkClientArgumentGuard(tool, cfg.Profile.TLSMode == domain.NetworkVerifyTLS) + "rf_real=\nfor rf_dir in /usr/bin /bin /usr/local/bin; do\n  if [ -x \"$rf_dir/" + tool + "\" ]; then rf_real=\"$rf_dir/" + tool + "\"; break; fi\ndone\n[ -n \"$rf_real\" ] || exit 78\nexec \"$rf_real\" " + strings.Join(quoted, " ") + " \"$@\"\n"
		filename := filepath.Join(bin, tool)
		if err := writePrivate(filename, []byte(content)); err != nil {
			return errors.New("could not persist owned downloader policy")
		}
		if err := os.Chmod(filename, 0755); err != nil {
			return errors.New("could not make owned downloader policy executable")
		}
	}
	if container {
		if err := os.Chmod(bin, 0755); err != nil {
			return errors.New("could not expose owned downloader wrappers read-only")
		}
	}
	return nil
}
