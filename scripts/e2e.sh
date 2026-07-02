#!/bin/sh
# End-to-end tests against a real container runtime.
# Usage: ./scripts/e2e.sh   (from the repo root; respects PRERUN_TEST_IMAGE)
set -e
IMG="${PRERUN_TEST_IMAGE:-alpine:latest}"
BIN="./prerun"
FIX="testdata/pipeline.yml"

go build -o prerun .
fail() { echo "E2E FAIL: $1"; exit 1; }

rm -rf .prerun
$BIN run "$FIX" --default-image "$IMG" > /tmp/prerun-e2e1.log 2>&1 || fail "full pipeline run"
grep -q "artifacts from build:app injected" /tmp/prerun-e2e1.log || fail "artifact injection missing"
grep -q "✓ pipeline passed" /tmp/prerun-e2e1.log || fail "pipeline did not pass"

rm -rf .prerun
$BIN run "$FIX" --default-image "$IMG" --break test:app:2 --break-exec 'ls dist/ && echo AT-BREAK' > /tmp/prerun-e2e2.log 2>&1 || fail "breakpoint run"
grep -q "⏸ breakpoint: test:app before step 2" /tmp/prerun-e2e2.log || fail "breakpoint did not fire"
grep -q "AT-BREAK" /tmp/prerun-e2e2.log || fail "break-exec did not run"

printf 'boom:\n  stage: test\n  script:\n    - exit 3\n' > /tmp/prerun-fail.yml
if $BIN run /tmp/prerun-fail.yml --default-image "$IMG" > /dev/null 2>&1; then fail "failing pipeline should exit nonzero"; fi

if $BIN run "$FIX" --break nosuchjob --default-image "$IMG" > /tmp/prerun-e2e4.log 2>&1; then fail "invalid breakpoint should be rejected"; fi
grep -q "no job named" /tmp/prerun-e2e4.log || fail "invalid breakpoint error message missing"

echo "E2E OK"
