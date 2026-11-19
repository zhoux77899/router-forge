# Build workflow

Use this workflow for artifact cleanup, full builds, and packaging changes.

## Clean full build

1. Use the existing Bash, Bun, Go and locked MyGo prerequisites. On Windows, use
   Git Bash. The entry checks tools and project tooling before cleanup; setup or
   dependency installation is separate from building.
2. Close applications and backends using output files before a clean build. Ask
   before stopping a user's process; the build entry never terminates processes.
3. Run the repository-root `build.sh` with `bash build.sh`, or use `bun run build`.
   `bun run build:desktop` is a compatibility alias for the same full build.
4. Each invocation removes the complete root `build` tree, including historical
   Desktop bundles, and existing legacy app/package outputs. The deletion
   allowlist and path checks live in `scripts/build-outputs.ts`. Every candidate is
   validated before any deletion; symlinks, junctions and unexpected entries fail
   the cleanup rather than expanding its scope.
5. Rebuild the read-only Web, backend/workers and native Desktop package with the
   existing component commands. Use the standard root `build` layout from the
   Vite/MyGo configurations, not ad-hoc `desktop-*` snapshot directories. The full
   entry packages fresh host helpers; external helper and cross-target environment
   overrides are cleared only in its subprocess.

Preserve source assets, generated source bindings, dependencies, tool caches and
application data. Cleanup is output-only, not `git clean`. Keep Git changes and
staging under the user's control.

## Completion

- The entry must finish with exit code zero and pass its artifact verification.
  `bun scripts/build-outputs.ts verify` also checks the current outputs directly.
- Verify the fresh Web, renderer, backend, workers and host-native package exist
  under root `build`; on Windows this includes the EXE, NSIS installer and adjacent
  `server`, `workers`, `dist` resources. An installer is a build output, not proof
  of installation or native visual acceptance.
- Run the smallest relevant tests and type checks separately; building does not
  replace them. Stop on failure instead of treating a partial tree as a release.
- Component commands remain available for targeted development/validation. Use
  the root entry when a clean complete build is required. Launching the GUI or
  installer is a separate, explicit user action.
