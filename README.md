# prerun

Run and debug your GitLab CI pipeline **locally**, before you push it.

`prerun` executes your `.gitlab-ci.yml` job graph on your machine in containers, and lets you **set a breakpoint on any step**: pause the pipeline mid-run and get a shell *inside the job container* to inspect the environment, poke at files, then resume.

No more `git commit -m "fix ci"` seances.

The transcript below is the actual output of [`examples/simple.yml`](examples/simple.yml) (with `--break-exec 'ls dist/'` standing in for the interactive shell so it can run unattended; in a terminal, `--break test:2` drops you into a live `sh` inside the job container instead):

```
$ prerun run examples/simple.yml --break test:2 --break-exec 'ls dist/'
→ 2 stage(s), 2 job(s)

[build] stage=build image=alpine:latest
  $ mkdir -p dist
  $ echo "compiled-v1" > dist/app.bin
  ↳ artifacts stored: dist/
  ✓ build

[test] stage=test image=alpine:latest
  ↳ artifacts from build injected
  $ echo "testing against $(cat dist/app.bin)"
  testing against compiled-v1
  ⏸ breakpoint: test before step 2
  ⏸ running --break-exec
  app.bin
  $ test "$(cat dist/app.bin)" = "compiled-v1"
  ✓ test
✓ pipeline passed
```

## Install

Requires Go 1.25+ and a container runtime (Docker, colima, podman, ...). Older Go (1.21+) with the default `GOTOOLCHAIN=auto` fetches the right toolchain on its own.

```
go install github.com/P51moustache/prerun@latest
```

Or clone and `go build`.

## Usage

```
prerun run [pipeline.yml] [flags]      # defaults to .gitlab-ci.yml

--break job[:step]     pause before a step (1-based) and open a shell in the job container
--break-exec "cmd"     at the breakpoint, run a command instead of a shell (scripting/CI-of-CI)
--default-image IMG    image for jobs that don't set one (default: alpine:latest)
--artifacts DIR        host dir for inter-job artifacts (default: .prerun/artifacts)
--docker BIN           container CLI, e.g. podman (default: docker)
```

Breakpoint specs are validated against the pipeline before anything runs: a typo'd job name or out-of-range step fails fast instead of silently never pausing.

## What's supported (v0.1, honest subset)

| Supported | Not yet |
|---|---|
| `stages` (+ GitLab's default stages) | `variables`, `rules` / `only` / `except` |
| jobs with `script` (string or list) | `services`, `cache`, `include`, `extends` |
| `image` per job | `before_script` / `after_script` |
| `artifacts.paths` (explicit paths; glob patterns are rejected with a clear error, not fumbled) | `parallel` / `matrix` |
| `needs` (names or `{job:}`) and `dependencies` for artifact flow, validated like GitLab's linter (unknown or later-stage references fail fast) | GitHub Actions (planned) |

Two honesty notes worth reading before you rely on it:

- **All jobs run sequentially in v0.1**, even independent jobs in the same stage. That's a deliberate first-cut simplification, not a discovered limitation. Stage-level concurrency is on the roadmap.
- **Anything unsupported is warned about loudly, not silently ignored**, including job-level keys. If a job uses `rules:`, prerun tells you it's ignoring it rather than letting you believe the rule gated anything. Hidden `.template` jobs produce a warning too (since `extends` isn't supported yet). If your pipeline leans on the right-hand column, prerun isn't ready for it. That's what the issue tracker is for.

## Isn't this just a wrapper around `docker exec`?

Mechanically, sure: jobs are containers and steps are execs; there's no magic and you can read all of it in an afternoon. The work is in the GitLab semantics riding on top (stage ordering, `needs`/`dependencies` artifact scoping, linter-style validation) and in the debugging workflow (`--break` on a live job container mid-pipeline). If the plumbing were the point, this README would be a shell script.

## How it relates to act and gitlab-ci-local

Both are excellent and battle-tested, credit where due:

- [act](https://github.com/nektos/act) covers GitHub Actions. Interactive step-through debugging is out of its scope; the maintainers' suggested approach for poking at a job is [`docker exec` into the container yourself](https://github.com/nektos/act/issues/1050).
- [gitlab-ci-local](https://github.com/firecow/gitlab-ci-local) covers far more of GitLab CI than prerun does today, including multi-job runs with `needs` and artifact hand-off. If you need broad `.gitlab-ci.yml` compatibility right now, use it.

What prerun is *for* is the debugging loop: breakpoints that pause the graph and drop you inside the failing job's container, with `--break-exec` for scripted inspection. That's the itch neither tool scratches, and it's the reason this exists.

## Status

Early stage and moving fast. The subset above is covered by unit tests (`go test ./...`) and an end-to-end script ([`scripts/e2e.sh`](scripts/e2e.sh)) that runs the real thing against a real container runtime; everything else is roadmap.

Transparency: this CLI is MIT-licensed and stays that way. A paid desktop companion (richer UI on the same engine) is planned. If you want a ping when that exists, there's a [waitlist](https://p51moustache.github.io/prerun-landing/). The CLI is not a demo; it's the foundation.
