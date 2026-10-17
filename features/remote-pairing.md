# Remote, serve and pairing

`crew serve` is the HTTP and WebSocket API of the phone app. `crew remote pair` gives a single-use code, the phone redeems it with `POST /api/v1/pair` and gets a device token. `crew remote devices` lists devices and `crew remote revoke` removes one. `verify iso up` pairs the device `TEST-verify` this way.

## Setup

```sh
go run ./scripts/verify iso down
go run ./scripts/verify iso up
```

## Recipes

### the paired device is listed

```sh
go run ./scripts/verify iso crew --expect TEST-verify -- remote devices
```

### pair prints a link and a code

```sh
go run ./scripts/verify iso crew --expect "#pair=" --expect "single use" -- remote pair --name TEST-phone --url https://iso.invalid
```

### pair refuses an http URL

```sh
sh -c '! go run ./scripts/verify iso crew -- remote pair --url http://iso.invalid >/dev/null'
```

### the PWA signs in with the paired token

```sh
go run ./scripts/verify web drive / --viewport mobile --out-dir /tmp/crew-verify-map/remote --steps '[{"expect":{"role":"link","name":"New session"}},{"shot":"signed-in.png"}]'
```

### scan the QR code with a phone camera (manual: no camera on the server)

## Cleanup

```sh
go run ./scripts/verify iso down
```

## Gotchas

- `crew serve` checks `Origin` against `--url`. `iso up` passes the Vite URL as `--url`, because the PWA runs on the Vite dev server.
