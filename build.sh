#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'Usage: bash build.sh\nBuild Web, backend/workers and the native Desktop package from clean outputs.\n'
}

if [[ $# -gt 0 ]]; then
  if [[ $# -eq 1 && ( "$1" == '--help' || "$1" == '-h' ) ]]; then
    usage
    exit 0
  fi
  usage >&2
  exit 2
fi

root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
cd -- "$root"

for tool in bun bunx go; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    printf 'Missing build tool: %s. Set up the documented prerequisites first.\n' "$tool" >&2
    exit 1
  fi
done

bun --version
go version
bunx --no-install mygo --version
bun --no-install -e 'for (const name of ["vite", "mygo-cli", "@resvg/resvg-js"]) require.resolve(name)'

# Full builds package the fresh host helpers, not an external or cross-target override.
unset ROUTER_FORGE_HELPERS_ROOT GOOS GOARCH
bun scripts/build-outputs.ts clean
bun run build:web
bun run build:backend
bun run --cwd ./apps/desktop build
bun scripts/build-outputs.ts verify

printf '\nClean build completed. Outputs: %s/build\n' "$root"
