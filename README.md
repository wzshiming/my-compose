# my-compose

## Testing

Unit tests: `go test ./...`.

End-to-end: `MY_COMPOSE_E2E_ENGINE=docker go test -tags e2e ./test/e2e` runs the scenario in `test/e2e/testdata/stack` through the named engine, which must be in `PATH`; `-args -update` rewrites the golden from that run and `MY_COMPOSE_E2E_RECORD=<dir>` saves the raw engine output there.

CI runs the e2e test for docker, podman and nerdctl on Linux and for wslc on Windows; every engine's transcript must match the single `test/e2e/testdata/stack/transcript.golden`. Apple `container` has only dry-run goldens and unit tests because hosted macOS runners cannot start containers.
