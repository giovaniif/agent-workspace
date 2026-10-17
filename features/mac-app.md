# Mac app

The macOS app (`macos/`): the main window, review, new session, automations, browser, disk, first run and the menu bar. The Linux server has no macOS. Thus the proof is the `macos` job of the `ci` workflow and its `macos-snapshots` artifact.

## Setup

```sh
true
```

## Recipes

### the newest main run has a green macos job and snapshots

Needs: VERIFY_MAC_RUN

```sh
sh -c 'go run ./scripts/verify mac evidence --run "$VERIFY_MAC_RUN" --out-dir /tmp/crew-verify-map/mac'
```

Expect: the `macos` job is `success` and the artifact has the main window, review and menu bar PNGs. Set `VERIFY_MAC_RUN` to a run id from `gh run list --workflow ci --branch main --limit 1`.

### drive the Mac app live (manual: no macOS on the server; read the snapshot PNGs from the artifact)

## Cleanup

```sh
true
```

## Gotchas

- `mac evidence --pr <n>` uses the newest `ci` run of the PR head. A run that is still in progress fails with "not finished".
