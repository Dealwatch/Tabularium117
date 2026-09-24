# Release checklist

This is the maintainer's checklist for cutting a Tabularium 117 release.

1. Run the full verification: `./scripts/check.sh`. Do not release on a red
   check. If the UI changed, update its README screenshots from a long enough
   real or clearly labelled example recording to show a useful history chart.
2. Update `CHANGELOG.md`: move the `[Unreleased]` (or beta) section's
   entries under a new `## [X.Y.Z] - YYYY-MM-DD` heading with the actual
   release date. In the release notes, tell existing testers plainly to
   download the new `tabularium117.exe`; the app has no automatic updater.
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
   `dist/SHA256SUMS`, and publishes a GitHub release with those two files
   plus `LICENSE` and `THIRD_PARTY_NOTICES.md` attached - the licence texts
   have to travel with the binary (the workflow is also runnable by hand
   via `workflow_dispatch`).

   **If Actions are unavailable (manual path)**, from a checkout of the
   pushed tag:
   ```sh
   make build                    # or: VERSION=vX.Y.Z make build
   cd dist && sha256sum tabularium117.exe > SHA256SUMS
   ```
   Then create the GitHub release by hand for the `vX.Y.Z` tag and attach
   the same four files as the workflow: `dist/tabularium117.exe`,
   `dist/SHA256SUMS`, `LICENSE` and `THIRD_PARTY_NOTICES.md`.

6. Verify the published download: fetch `tabularium117.exe` and
   `SHA256SUMS` from the release page and confirm the hash matches before
   announcing.

7. Announce per `KONZEPT.md` §9: the anno-mods Discord, r/anno, and the
   Anno Union community.

See `docs/beta.md` for what to tell beta testers.
