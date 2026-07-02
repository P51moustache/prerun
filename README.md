# prerun

Run and debug your GitLab CI pipeline **locally**, before you push it.

`prerun` executes your `.gitlab-ci.yml` job graph on your machine in containers — with two things local CI runners usually don't give you:

1. **Real artifact hand-off between jobs.** `dist/` from `build` actually arrives in `test`, following GitLab's `needs`/`dependencies`/stage semantics.
2. **Breakpoints.** Pause the pipeline before any step and get a shell *inside the job container* — inspect the environment, poke at files, then exit to resume.

No more `git commit -m "fix ci"` seances.

```
$ prerun run --break test:2

[build] stage=build image=alpine:latest
  $ mkdir -p dist
  $ echo "compiled-v1" > dist/app.bin
  ↳ artifacts stored: dist/
  ✓ build

[test] stage=test image=alpine:latest
  ↳ artifacts from build injected
  $ echo "testing against $(cat dist/app.bin)"
  ⏸ breakpoint: test before step 2
  ⏸ opening shell inside the job container — exit to resume the pipeline
/workspace # ls dist/
app.bin
/workspace # exit
  $ test "$(cat dist/app.bin)" = "compiled-v1"
  ✓ test
✓ pipeline passed
```

## Install

Requires Go 1.22+ and a container runtime (Docker, colima, podman, ...).

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

## What's supported (v0.1 — honest subset)

| Supported | Not yet |
|---|---|
| `stages` (+ GitLab's default stages) | `variables`, `rules` / `only` / `except` |
| jobs with `script` (string or list) | `services`, `cache`, `include`, `extends` |
| `image` per job | `before_script` / `after_script` |
| `artifacts.paths` | `parallel` / `matrix` (jobs run sequentially for now) |
| `needs` (names or `{job:}`) and `dependencies` for artifact flow | GitHub Actions (planned) |

Unsupported top-level keys are **warned about loudly**, not silently ignored. If your pipeline leans on the right column, prerun isn't ready for it yet — that's what the issue tracker is for.

## How it relates to act and gitlab-ci-local

[act](https://github.com/nektos/act) and [gitlab-ci-local](https://github.com/firecow/gitlab-ci-local) are excellent, and if a single-job dry run is all you need, use them. prerun exists for the part between the jobs: whole-graph runs with faithful artifact hand-off, plus step-through debugging — which act's maintainers have explicitly scoped out.

## Status

Early stage and moving fast. The GitLab CI subset above is real and tested; everything else is roadmap. If you want updates (a richer desktop version is in the works): [prerun waitlist](https://p51moustache.github.io/prerun-landing/).

MIT licensed.
