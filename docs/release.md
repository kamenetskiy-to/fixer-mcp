# Release

## Goal

Publish the GitHub-ready Fixer MCP track from `github_repo/` itself.

The release story for this repo is no longer "run a private export script and clean the result." The canonical source for public publication is the staged repo layout already present in `github_repo/`.

## Release Units

- `packages/fixer-mcp-server`: Go control-plane binary
- `packages/client-wires`: Python launcher package
- `packages/compat-bridge`: Python compatibility package
- `docs/` and top-level examples: public operational canon for the repo

## Validation Gate

Run these before publishing:

```bash
python3 -m unittest discover -s tests
cd packages/fixer-mcp-server && go test ./...
```

## Repo-native Release Helper

The helper script lives in `scripts/release_public_repo.py` and runs entirely within `github_repo/`.

Dry-run the plan:

```bash
python3 scripts/release_public_repo.py --version 0.1.0 --dry-run --json
```

Execute the release build:

```bash
python3 scripts/release_public_repo.py --version 0.1.0
```

The helper does not require the external `build` frontend module. For Python packages it reads each package's `pyproject.toml` and invokes the declared PEP 517 backend directly, so a clean environment with `setuptools` is enough for the staged Python artifacts.

By default the helper:

- runs repo Python tests
- runs Go tests for `packages/fixer-mcp-server`
- builds `fixer-client-wires` sdist and wheel
- builds `fixer-compat-bridge` sdist and wheel
- builds a `fixer_mcp` binary
- assembles a canonical `github_repo/` publication snapshot under `assembly/github_repo/`
- writes `dist/releases/<version>/assembly-manifest.json`
- writes `dist/releases/<version>/release-manifest.json`

Expected output layout:

```text
dist/
  releases/
    <version>/
      assembly/
        github_repo/
      bin/
        fixer_mcp
      python/
        client-wires/
        compat-bridge/
      assembly-manifest.json
      release-manifest.json
```

The assembled tree is the first canonical publication boundary for the public repo. It is copied directly from `github_repo/` and contains the public docs, packages, examples, scripts, tests, and top-level `README.md` needed to inspect or publish the staged track without invoking legacy export tooling.

The helper writes the assembly snapshot before package-build steps, so reviewers can still inspect the canonical source payload even if a later packaging dependency is missing on the local machine. `release-manifest.json` is still written only after the full build succeeds.

## Publication Workflow

1. Update docs, package metadata, and staged code in `github_repo/`.
2. Run the validation gate.
3. Run `scripts/release_public_repo.py` for the target version.
4. Review `dist/releases/<version>/assembly/github_repo/` as the canonical assembled repo payload, then inspect `assembly-manifest.json` and `release-manifest.json`.
5. Publish from this repo boundary, using the built Python artifacts and Go binary as the release payload.

## Compatibility Position

Legacy export tooling outside `github_repo/` may still exist during migration, but it is no longer the product story for publication. At most, it is temporary compatibility infrastructure until the staged packages fully replace it.
