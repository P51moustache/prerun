# Contributing

Small project, low ceremony. Thanks for being here.

## Build & test

```
go build -o prerun .
go test ./...                 # unit tests, no container runtime needed
./scripts/e2e.sh              # end-to-end against a real runtime (docker/colima/podman)
```

`scripts/e2e.sh` respects `PRERUN_TEST_IMAGE` (default `alpine:latest`) if you need a locally-available image.

## Good bug reports

Include: your `.gitlab-ci.yml` (or a minimal repro), the full prerun output, `prerun version`, and your container runtime (`docker version` / colima / podman).

## Good PRs

The [README's "Not yet" table](README.md#whats-supported-v01-honest-subset) is the roadmap: anything in the right-hand column is fair game. Keep the project's one rule: **unsupported things fail loudly or warn loudly, never silently.** A PR that adds a feature also adds its tests.
