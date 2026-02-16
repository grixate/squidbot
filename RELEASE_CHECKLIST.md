# Squidbot Stable Release Checklist

Use this checklist when preparing a stable GitHub release from `main`.

## 0) Preconditions

- [ ] Local checkout is clean:
  - `git status --porcelain`
- [ ] You are on `main` and up to date:
  - `git checkout main`
  - `git pull --ff-only`
- [ ] Go toolchain matches `go.mod` (`go1.25.x`):
  - `go version`

## 1) Quality Gates

- [ ] Unit/integration tests pass:
  - `go test ./...`
- [ ] Race suite passes:
  - `go test -race ./...`
- [ ] Flaky fanout race test is stable:
  - `go test -run TestEngineFanOutFanInParallelSubagents -race ./internal/agent -count=10`

## 2) Build + CLI Smoke Gates

- [ ] Build release binary:
  - `go build -o squidbot ./cmd/squidbot`
- [ ] Onboarding help is available:
  - `./squidbot onboard --help`
- [ ] Runtime status command works:
  - `./squidbot status`
- [ ] Doctor command runs:
  - `./squidbot doctor`

## 3) Release Notes Gates

- [ ] Collect commit range for this release:
  - If tags exist: `git log --oneline <last_tag>..HEAD`
  - If no tags exist: `git log --oneline --reverse`
- [ ] Notes include all required sections:
  - Highlights
  - Reliability and safety changes
  - Fixes
  - Upgrade notes
  - Known issues (if any)
  - Full changelog range

## 4) Tag + Publish

- [ ] Confirm working tree is clean before tagging:
  - `git status --porcelain`
- [ ] Create annotated tag:
  - `git tag -a v0.X.Y -m "Squidbot v0.X.Y"`
- [ ] Push branch and tag:
  - `git push origin main`
  - `git push origin v0.X.Y`
- [ ] Create GitHub Release from tag `v0.X.Y` and paste notes.

## 5) Post-Release Verification

- [ ] Confirm tag points to intended commit:
  - `git rev-parse v0.X.Y`
  - `git rev-parse HEAD`
- [ ] Re-run quick sanity suite on `main`:
  - `go test ./...`

## 6) Rollback Procedure

- [ ] Identify previous stable tag:
  - `git tag --sort=-creatordate`
- [ ] Rollback target documented in release issue/notes.
- [ ] Redeploy previous stable tag in runtime environments.
