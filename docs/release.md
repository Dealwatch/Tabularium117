# Release checklist

This is the maintainer's checklist for cutting a Tabularium 117 release.

1. Run the full verification: `./scripts/check.sh`. Do not release on a red
   check.
2. Update `CHANGELOG.md`: move the `[Unreleased]` (or beta) section's
   entries under a new `## [X.Y.Z] - YYYY-MM-DD` heading with the actual
   release date.
3. Commit the changelog update.
4. Tag the release:
   ```sh
   git tag -a vX.Y.Z -m "Tabularium 117 vX.Y.Z"
   git push origin vX.Y.Z
   ```
   The version string embedded in the binary comes from `git describe`, so
   the tag must exist **before** building — build after tagging, not before.

5. Build and publish.

   **Normally:** pushing the tag triggers
   `.github/workflows/release.yml`, which runs
   `./scripts/check.sh`, builds `dist/tabularium117.exe`, computes
   `dist/SHA256SUMS`, and publishes a GitHub release with both files
   attached (the workflow is also runnable by hand via
   `workflow_dispatch`).

   **If Actions are unavailable (manual path)**, from a checkout of the
   pushed tag:
   ```sh
   make build                    # or: VERSION=vX.Y.Z make build
   cd dist && sha256sum tabularium117.exe > SHA256SUMS
   ```
   Then create the GitHub release by hand for the `vX.Y.Z` tag and attach
   both `dist/tabularium117.exe` and `dist/SHA256SUMS`.

6. Verify the published download: fetch `tabularium117.exe` and
   `SHA256SUMS` from the release page and confirm the hash matches before
   announcing.

7. Announce per `KONZEPT.md` §9: the anno-mods Discord, r/anno, and the
   Anno Union community.

See `docs/beta.md` for what to tell beta testers.
