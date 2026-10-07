# internal/adapters/setup

`agentws setup-worktree <path>` applies a repo's recipe to a linked worktree, in the CLI process (no daemon), and reports duration and the change in free bytes on the volume. See [ADR 0013](../../../docs/adr/0013-setup-recipes.md).

- **Recipe.** The `[setup]` table of `<main checkout>/.agentws.toml` (read with `BurntSushi/toml`): `copy`, `link`, `run` and `deps` (`clone`, `link` or `install`). Order: copy, link, deps, run; the first failure stops. Paths already in the worktree are never overwritten.
- **Deps.** `clone` is `cp -c -R` (APFS clonefile). A lockfile that differs from main's, or a main without `node_modules`, falls back to a frozen install (`bun`, `pnpm`, `yarn` or `npm ci`, chosen by lockfile) and logs why.
- **Layers.** Rules in `domain` (`Recipe.Validate`, `PlanDeps`, `InstallCommand`); `app.WorktreeSetup` uses the `RecipeSource`, `MainCheckouts`, `SetupFS` and `CommandRunner` ports, implemented here and in `adapters/git`.
- **Tests.** Integration tests are named `TestRecipe*`: `go test -tags integration -run Recipe ./internal/adapters/setup`.
- **Failure output.** `Shell.Run` streams a `run` step's output to its writer and keeps the last 4 KiB; a failing step's error carries that tail, so `session.new`'s `failed` message shows why setup broke (the Mac app's sheet prints it).
- **Budget.** Cloning a 1 GB `node_modules` must take under 5 s and use under 50 MB. Measured on APFS with 1000 files: 0.13 s and 0.3 MB.
