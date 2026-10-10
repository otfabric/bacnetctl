# Contributing to bacnetctl

Thank you for your interest in contributing to `bacnetctl`! This document covers the guidelines for contributing.

## Getting Started

1. Fork the repository on GitHub
2. Clone your fork locally:
   ```sh
   git clone git@github.com:<your-username>/bacnetctl.git
   cd bacnetctl
   ```
3. Create a feature branch:
   ```sh
   git checkout -b feature/my-change
   ```

## Development Setup

### Prerequisites

- Go 1.25+ (see [go.mod](go.mod))
- [staticcheck](https://staticcheck.dev/) and [golangci-lint](https://golangci-lint.run/) for linting
- No hardware: the test suite brings its own simulated device (see [Testing](#testing))
- The working tree of [go-bacnet](https://github.com/otfabric/go-bacnet) next to this repository, while `go.mod` carries a `replace` to `../go-bacnet`

### Build & Test

```sh
# Run all checks (format, lint, vet, race tests, coverage gate)
make check

# Run tests with the race detector
make test

# Run tests with whole-module coverage (fails below 85%)
make coverage

# Build for the current platform (./bin/bacnetctl); runs `make check` first
make build
```

Run `make` (or `make help`) for the full list of targets.

## Making Changes

### Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Keep functions focused and small
- Use meaningful variable and function names
- Add comments only where the logic isn't self-evident; comments end with a period (enforced by the linter)
- Every Go file starts with `// SPDX-License-Identifier: MIT`

### Formatting

`make fmt` formats with the `gofmt` of the Go version in `go.mod`, which is the version CI uses, not necessarily the one you have installed. `make fmt-check` (part of `make check`) fails if either that `gofmt` or your local one would change a file: `gofmt` output differs slightly between Go releases, and a file the two disagree on fails CI. If they disagree, restructure the code (for example, move trailing comments onto their own lines) rather than picking a side.

### Commit Messages

Use clear, concise commit messages:

```
component: short description

Optional longer explanation of the change, why it was made,
and any relevant context.
```

Examples:
- `monitor: cancel the subscriptions when a later one is refused`
- `write: take the names of an enumeration for a value without type`
- `docs: explain the local port`

### Testing

- Add tests for new functionality in the appropriate `_test.go` file
- Use [`internal/bactest`](internal/bactest) for anything that talks to a device. It is a simulated BACnet/IP device on a free loopback UDP port, with a model of a dozen objects, that records the requests it receives and can be told to misbehave (`Silent`, `Mute`, `Without`, `MaxAPDU`, `Password`, `Router`). Command tests run the real command tree through `cmd.Run` and assert on stdout, stderr and the exit code
- The simulated device answers to the address that asked, also where a real device broadcasts: tests use `--device 127.0.0.1:<port>` and, for the commands that default to port 47808, `--local-port 0`. No test binds port 47808 or sends a broadcast
- Assert behaviour (output, exit code, device state), not just that the code ran
- Tests must not write files into the repository: use `t.TempDir()`
- Do not synchronize with sleeps: wait for what the device saw (`AwaitRequest`, `AwaitSubscriptions`) with a deadline. Short timeouts belong only where a timeout is what is tested
- Keep tests fast and race-clean; CI runs them with `-race` and a 120 s per-package timeout
- Whole-module statement coverage must stay at or above 85% (`make coverage`)
- The simulated device is built from the codecs of go-bacnet, so a test against it proves the command, not interoperability. If your change affects what goes on the wire, verify against a real device or another stack when possible, and say in the pull request what you tested against

### Documentation

When you change commands, flags, output or exit codes, update:

- The command's `Short` / `Long` / `Example` help text
- [README.md](README.md), including the environment variable table for new flags. An environment variable must hold the same kind of value for every command that reads it (a test enforces it): the environment of all commands is read at startup
- [docs/json-output.md](docs/json-output.md) and [docs/exitcodes.md](docs/exitcodes.md) — `--format json` and exit codes are a stable contract for scripts
- [RELEASE.md](RELEASE.md), calling out anything that breaks existing usage

## Submitting Changes

1. Push your branch to your fork:
   ```sh
   git push origin feature/my-change
   ```
2. Open a Pull Request against the `main` branch
3. Describe what your change does and why
4. Reference any related issues

For larger changes, please open an issue first to discuss the approach.

## Reporting Issues

- Use GitHub Issues to report bugs or suggest features
- Include the output of `bacnetctl version` in bug reports
- Provide the exact command that triggered the issue, along with the error output; `--debug` adds the diagnostics of the BACnet client and the causes of the error
- Mention the device type and vendor, its protocol revision (`bacnetctl device`), and whether it sits behind a router or a BBMD. A captured frame, as `bacnetctl decode` shows it, helps a lot
- Report security vulnerabilities privately: see [SECURITY.md](SECURITY.md)

## Project Structure

```
cmd/                      Root command and every command (discover, read, write, monitor, …), version, completion
internal/config/          Configuration types, flag and environment binding, device addresses
internal/bac/             What the commands do: the BACnet/IP session, addressing, names, value parsing and
                          rendering, the services, error classification, frame decoding
internal/bactest/         The simulated BACnet/IP device of the tests
internal/bactest/bacsim/  The same device as a stand-alone program, to try bacnetctl by hand
internal/format/          Output formatting (text, JSON, table, streaming)
internal/types/           Output data models
internal/runner/          Command wiring, formatted and streaming runs, fatal rendering
internal/errs/            Typed errors, stable codes, exit-code mapping
internal/cli/             Shared Cobra helpers
docs/                     Exit codes, JSON output contract
```

## License

By contributing to `bacnetctl`, you agree that your contributions will be licensed under the [MIT License](LICENSE).
