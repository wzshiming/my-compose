// Package engine defines the interface container engine dialects implement and selects one of them.
//
// Dialects live in the subpackages container, docker, podman, nerdctl and wslc, each exporting New; the docker-style four share internal/dockercli over internal/render and internal/parse, and enginetest holds the contract test, fixture steps, fake and golden helpers their tests use.
package engine
