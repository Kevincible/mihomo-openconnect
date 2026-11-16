# Upstream maintenance and releases

`upstream.lock.json` records the Mihomo tag/SHA, sing-openconnect Go version/SHA, tracked branch, Go toolchain and release channel. Published locks also record the adaptation input commit, release number and SHA-256 of the input combination. A release's `version-manifest.json` adds the complete source commit, six platform build configurations and binary hashes. Every archive includes its own `BUILD.json`.

The repository retains Mihomo's original module path and complete source history. Functional edits are concentrated in the OpenConnect files, parser registration, appended adapter enum and module dependencies. Small format-string corrections in `adapter/provider/override_expr.go` and `transport/vless/vision/conn.go` make upstream pass the newer Go vet checks. The original upstream build/release/CMFA workflows are replaced, so this fork does not publish to upstream services.

## Tracking

[Track upstream and release](../../.github/workflows/track-upstream.yml) runs at minute 17 every six hours and can be dispatched manually. It selects the newest stable Mihomo release; sing-openconnect prefers newer stable tags and otherwise follows the configured branch. The initial baseline commit is on sing-openconnect `dev`, so the initial lock tracks `dev` (not its older `main` tip). An older tag cannot downgrade a newer commit. A moved Mihomo tag, rewritten library history, merge conflict, invalid version or dependency error fails the job.

Preparation runs on a uniquely named `automation/upstream-*` branch. A three-way merge retains the new Mihomo history and applies the existing fork changes; incompatible edits stop at the conflict. Only this phase may run `go get`/`go mod tidy`. It pins the resulting dependency version and commits both module files. All validation and release builds use `-mod=readonly` and the locked Go version.

The same reusable Validate workflow runs for pushes/PRs and for the update candidate. Publication requires every regression, real ocserv/race test and platform build. It does not depend on a bot push starting another workflow. Ordinary library commits publish as GitHub prereleases and do not replace the latest stable release.

After validation, publication checks that `main` has not advanced, atomically pushes the candidate source and an annotated source tag, uploads complete assets to a draft, and only then publishes it. Existing tags and public releases are never overwritten. Existing draft assets are accepted only if identical. Same inputs plus an existing public release are a no-op. An interrupted publication can be retried on the same inputs. Release archives use deterministic file metadata so retrying a build does not change checksums.

One concurrency group serializes tracking and release. Updates made to main during validation cause publication to stop and retry against the new main. Failed preparation or validation leaves the previous usable release intact; candidate branch names, commit IDs and logs remain inspectable in Actions. No automatic PR/issue messages are sent to upstream maintainers.

## Manual operation

The workflow uses only `GITHUB_TOKEN`, with `contents: write` restricted to preparation/publication; validation is read-only. Enable Actions for the repository. No company VPN secrets or additional PAT are required. Scheduled GitHub jobs can be delayed, and inactive public repositories may have schedules disabled by GitHub; manual dispatch remains available.

```sh
gh workflow run track-upstream.yml -R Kevincible/mihomo-openconnect \
  -f mihomo=v1.19.34 \
  -f openconnect=cd35f41e8f69d4c28ac5f9ac87d13e6419a0cac3

# Read-only inspection of current upstream choices:
python3 scripts/openconnect/upstream.py --check --repo Kevincible/mihomo-openconnect

# Reproduce/verify an already published version, without republishing:
gh workflow run validate.yml -R Kevincible/mihomo-openconnect -f ref=v1.19.34-oc.1
```

Explicit library SHAs are candidates; explicit stable library tags enter the stable channel. An existing fixed baseline may be stable after its integration gates pass even when the library uses a pseudo-version. Stable release numbering is `v<Mihomo>-oc.N`; the serial increases for dependency or adapter-only changes too. GitHub's prerelease flag distinguishes ordinary-commit candidates.

Use a clean checkout to prepare updates locally. `upstream.py` creates a branch and may leave conflicts for inspection; `--check` does not edit the checkout. Do not run a mutating prepare command inside a worktree containing unrelated changes.

## Rollback and Go module consumers

Keep the previous executable and protected config before switching binaries. To roll back, stop your host-managed process, restore the previous verified release binary and config, and restart it. No automatic OS service installation or production configuration change is performed by this repository.

Pin a published source tag in the consumer's main module:

```go
replace github.com/metacubex/mihomo => github.com/Kevincible/mihomo-openconnect v1.19.34-oc.1
```

Use the actual tag shown in Releases, not `@latest`; Go treats `-oc.N` as a prerelease version. Dependency-module replacements do not propagate: retain the protobuf replacement required by Mihomo in the consumer's main module. To roll back a Go consumer, restore its previous explicit replacement and module files and rebuild. Tracking refuses to merge an older Mihomo tag as if it removed newer code; use the published source tag for historical validation.

## Dependency changes

The initial library requires Go 1.24+ and raises the selected common dependencies, including `x/crypto`, `x/net`, `x/sys`, `x/sync`, `x/text`, `x/tools`, compression and protobuf. See the committed module diff against `v1.19.34`. MetaCubeX and SagerNet sing modules coexist under their own module paths; neither is globally replaced with the other. Cross-platform CLI artifacts use CGO=0 and gVisor. The release does not require libopenconnect or a second Go runtime.
