# fuigo 

`go install` with pre-build steps.

## Why

`go install` cannot run `go generate` or any pre-build step, so projects that
need code generation or a frontend build must either commit generated artifacts
or document a multi-step manual build. `fuigo` is a drop-in `go install`
replacement that runs a module's declared pre-build steps first. It is a
temporary solution until Go adds native pre-build support.

## Install

```sh
go install github.com/sopranoworks/fuigo/cmd/fuigo@latest
```

## Usage

```sh
fuigo <package>[@version]
```

```sh
fuigo github.com/sopranoworks/shoka/cmd/shoka@latest
```

If the target module has a `fuigo.yaml` at its root, fuigo runs the declared
steps before `go install`. If it does not, fuigo behaves exactly like
`go install`.

Flags:

- `--yes` — skip the confirmation prompt (for CI)
- `--list` — show the steps without executing them
- `--version` — print the fuigo version

## fuigo.yaml

A list of pre-build steps, run in order from the module root:

```yaml
steps:
  - npmgo install --cache-only --lockfile web/package-lock.json
  - esbuild --entry web/src/main.tsx --bundle --outdir server/dist/
  - go generate ./...
```

Steps must start with one of three commands:

- `go` — runs the external `go` tool
- `npmgo` — installs npm packages (built-in, no Node.js required)
- `esbuild` — bundles TS/JSX/CSS (built-in)

`npmgo` and `esbuild` are compiled into fuigo, so a single `go install fuigo`
gives you npm install + bundling + Go build orchestration. No external commands
other than `go` are ever executed.

### Running a step in a subdirectory

A step may be written as a map with a `workdir` (relative to the module root)
instead of a bare string. fuigo runs the command with that directory as its
working directory — there is no shell, so `cd …&& …` does not work. The workdir
must stay within the module root.

```yaml
steps:
  - command: go run .
    workdir: build/frontend
  - go generate ./server/...
```

## How it works

1. Resolve the module version and download its source zip from the Go module
   proxy (`proxy.golang.org`) — the same mechanism as `go install`, no git.
   Private modules fall back to a git clone.
2. Extract to a temporary directory.
3. Read `fuigo.yaml`, show the steps, prompt for confirmation.
4. Run the steps, then `go install`.
5. Clean up the temporary directory.

## Security

Steps are defined by the module author, in the repository — the same trust
model as `go install`, which already compiles and runs the module's code. fuigo
shows the steps and asks before running them; `--yes` skips the prompt. Only
`go`, `npmgo`, and `esbuild` steps are permitted.

## License

MIT © 2026 Sopranoworks, Osamu Takahashi
