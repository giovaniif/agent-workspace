---
name: crew-browser
description: Drive the browser of the crew Mac app from this session with the crew browser CLI. Use it to open a dev server, read a page, click, fill forms, take screenshots or check a UI change in a real browser.
---
<!-- crew:browser-skill -->

# crew browser

The crew Mac app has a browser for this session. Control it with `crew browser`. The command finds this session from its pane. In other places, add `--session <id>`.

## Commands

- `crew browser goto <url>`: open a URL. A bare port such as `3000` or `localhost:3000/login` opens on the server host.
- `crew browser snapshot`: print the accessibility tree of the page. Each element that you can use has a ref such as `@e1`.
- `crew browser click @e1`: click an element.
- `crew browser fill @e2 some text`: replace the value of a field.
- `crew browser type some text`: type into the focused element.
- `crew browser press Enter`: press a key (`Enter`, `Tab`, `Escape`, `ArrowDown`, ...).
- `crew browser wait @e3`, `crew browser wait 500` or `crew browser wait Signed in`: wait for an element, a time in ms, or a text on the page.
- `crew browser screenshot`: write a PNG and print its path. Read the file to see the page. `--out file.png` selects the file.
- `crew browser eval 'document.title'`: run JavaScript in the page and print the result.
- `crew browser tabs`: list the tabs of this session.
- `crew browser cookies [domain]`: list cookie names and domains, never values.
- `crew browser set-device desktop|ipad|iphone`: change the viewport size.

Add `--json` to get the result as JSON. Add `--timeout 60s` for a slow page.

## Rules

- Refs are valid until the next `snapshot`. After a page change, take a new snapshot before you click again.
- The user can see the browser and can take over at any time. Do not enter passwords or payment data.
- If the command prints `no browser: the macOS app is not connected`, the browser is not available. Tell the user, and continue without it.
