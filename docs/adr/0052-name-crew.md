# ADR 0052: The name crew and its icon

Status: accepted, 2026-10-14. Epic: #349. Work item: #350.

## Context

The alpha releases of the tool have a long name. The name is difficult to say and does not tell what the tool does. Before the first beta release, the owner wants a short name and an icon in the current Catppuccin palette.

These facts apply:

- The user starts many agent sessions and gives each one a task. Then the user reviews the work of each session. The user is the lead of a small team of agents.
- The binary, the Go module, the tmux server, the environment variables, the home dir, the Mac app and the PWA all use the long name. Users of the alpha releases have state in these locations.

## Decision

### The name is `crew`

- The product, the binary and the Mac app get the name `crew`.
- The name is short (four letters) and easy to type and to say.
- The name tells the model of the tool: a crew is a team that you lead. The word is warm and not technical.

### Migration, and no alias

- Each rename of state has a migration and a test. Users of the alphas do not lose state, sessions, hooks or settings. The migration backs up each file before it changes it.
- The old command name does not stay as an alias. The migration rewrites the hooks, and removes the link with the old name next to the binary.
- Environment variables with the old prefix are read as a fallback.
- The code that finds the old names is in files with the names `legacy` or `migrate`, with their tests. No other file uses the old name.
- This ADR changes no names in the code. The work items after #350 in #349 do the rename.

### The known clash

- Chromebrew, a package manager for ChromeOS, also installs a command with the name `crew`.
- Chromebrew runs only on ChromeOS. `crew` runs on Linux servers and macOS. Thus the two commands do not occur on the same machine in normal use.

### The icon (variant B)

The owner selected variant B of the design file. The icon has only a rounded square and three circles. All sizes are a percentage of the side of the icon:

| Part | Size and position | Latte (light) | Mocha (dark) |
|---|---|---|---|
| Tile | full size, corner radius 23.5% | `#eff1f5` (Base) | `#313244` (Surface0) |
| Circle 1 | diameter 42%, left 12%, top 17% | `#1e66f5` (Blue) | `#1e66f5` |
| Circle 2, on top of circle 1 | diameter 42%, left 46%, top 17% | `#fe640b` (Peach), opacity 0.9 | `#fe640b`, opacity 0.9 |
| Dot | diameter 20%, left 40%, top 61% | `#4c4f69` (Text) | `#cdd6f4` (Text) |

The two large circles are the agents. The dot is the user, below them.

### The assets

- `web/public/icon.svg` is the source. `npm run icons` in `web/` makes the PWA icons, the maskable icon, `favicon-32.png`, `apple-touch-icon.png` and `macos/AppIcon.png` from it.
- `docs/images/brand/` holds the Latte and Mocha icons, the wordmarks (the icon and the word "crew" in the system font) as SVG and PNG, and the menu bar template image.
- The menu bar template image (`crewTemplate.png`, `crewTemplate@2x.png`, 18 pt) is one black outline of the three circles on a transparent background. macOS colours a template image for the menu bar.

## Why

- A short name that tells the model of the tool is easier to remember and to recommend.
- The icon uses the palette of the TUI and the PWA. Thus all surfaces look the same.
- Simple shapes stay clear at 16 px and in the menu bar.

## Rejected

- **Keep the alpha name:** it is difficult to say and to remember.
- **Variants A and C of the design file:** the owner selected B.
- **An alias for the old command name:** the owner wants no file but the migration code to use the old name. The migration rewrites the hooks, so only scripts of the user must change.

## Consequences

- The work items of #349 rename the CLI, the module, the state, the Mac app, the PWA, the docs and the repository.
- The Mac app continues to show an SF Symbol in the menu bar. The rename of the Mac app (#349) adds the template image to the app bundle and uses it.
- Users of ChromeOS who also use Chromebrew must use the full path of the binary.

## Note on older ADRs

ADRs 0001 to 0051 say crew where they named the alpha name. The text changed. The decisions did not. The commands, paths and environment variables that they name have crew forms now (`crew`, `~/.crew`, `CREW_*`, `.crew.toml`, `crew.sock`).
