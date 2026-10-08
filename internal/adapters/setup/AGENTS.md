# internal/adapters/setup

`agentws setup-worktree <path>` applies the recipe of a repo to a linked worktree, in the CLI process (no daemon). It reports the duration and the change in free bytes on the volume. See [ADR 0013](../../../docs/adr/0013-setup-recipes.md).

- **Recipe.** The `[setup]` table of `<main checkout>/.agentws.toml` (read with `BurntSushi/toml`): `copy`, `link`, `run` and `deps` (`clone`, `link` or `install`). The order is copy, link, deps, run. The first failure stops the recipe. The recipe never overwrites paths that are already in the worktree.
- **Deps.** `clone` is `cp -c -R` (APFS clonefile). In these conditions, the adapter uses a frozen install (`bun`, `pnpm`, `yarn` or `npm ci`, from the lockfile) and logs the reason:
  - The lockfile is different from the lockfile of main.
  - main has no `node_modules`.
- **Layers.** The rules are in `domain` (`Recipe.Validate`, `PlanDeps`, `InstallCommand`). `app.WorktreeSetup` uses the `RecipeSource`, `MainCheckouts`, `SetupFS` and `CommandRunner` ports. This package and `adapters/git` implement them.
- **Tests.** Integration tests are named `TestRecipe*`: `go test -tags integration -run Recipe ./internal/adapters/setup`.
- **Failure output.** `Shell.Run` streams the output of a `run` step to its writer and keeps the last 4 KiB. The error of a failing step has that tail. Thus the `failed` message of `session.new` shows why setup failed (the sheet of the Mac app prints it).
- **Budget.** A clone of a 1 GB `node_modules` must take less than 5 s and use less than 50 MB. Measured on APFS with 1000 files: 0.13 s and 0.3 MB.
