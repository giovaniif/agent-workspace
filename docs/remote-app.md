# Remote app

`crew serve` serves a phone app (a PWA) for the sessions on this machine ([ADR 0046](adr/0046-remote-app.md)). The app shows which session needs you, a chat view of each session, and new sessions. `crew remote pair` prints a QR code that pairs a phone. You select how the phone gets to the machine. `crew` only gives HTTPS. A PWA needs a certificate that the phone trusts. Thus `serve` gives plain HTTP only on loopback, and refuses it on all other addresses.

To keep it running, use `crew setup serve`. It takes the same flags as `serve` (`--addr`, `--cert`, `--key`, `--self-signed`, `--url`):

```sh
crew setup serve --addr 0.0.0.0:7420 --cert ~/machine.crt --key ~/machine.key
crew setup serve --remove
```

On Linux, it writes the systemd user unit `$XDG_CONFIG_HOME/systemd/user/crew-serve.service` (`~/.config/systemd/user/crew-serve.service` when `XDG_CONFIG_HOME` is not set). On macOS, it writes the launchd agent `~/Library/LaunchAgents/dev.crew.serve.plist`. Each one:

- starts at login with the `PATH` and `CREW_HOME` of the shell that ran the command
- restarts `serve` if it exits
- logs to `$CREW_HOME/serve.log`.

If you run it again with the same arguments, nothing changes. With different arguments, it backs up the old file as `.bak` and writes it again. `--remove` stops the service and deletes the file. The unit holds the path of the binary, so run the command again after you move `crew`.

A systemd user service stops when your last login session ends, unless lingering is on. On a Linux machine that you reach only through SSH, run `loginctl enable-linger $USER` yourself one time. `setup serve` does not do it.

`crew setup daemon [--remove | --check]` does the same for the daemon (`crew-daemon.service` or `dev.crew.daemon`). It uses the `PATH` of your login shell, not the `PATH` of the caller. Thus sessions get a full `PATH`, also when you run it through a bare `ssh -T`. On Linux, if lingering is off, it shows a warning and prints the `sudo loginctl enable-linger` command. `--check` prints `{"installed","running","linger"}` as JSON.

**WARNING: Do not open `serve` to the internet, unless a layer in front of it adds its own authentication.** Pairing protects the app from strangers on a network that you already trust. It does not protect it from the open internet: each person who can get to the port can try pairing codes and probe the API. Use one of the setups below.

## Tailscale

Both machines are on one tailnet, and nothing is open to the internet. Get a certificate for the name of the machine, and let `serve` use it. Bind `serve` to the tailnet address only, not to `0.0.0.0`:

```sh
sudo tailscale cert machine.tailnet-name.ts.net
crew setup serve --addr "$(tailscale ip -4):7420" --cert machine.tailnet-name.ts.net.crt --key machine.tailnet-name.ts.net.key
crew remote pair --url https://machine.tailnet-name.ts.net:7420
```

1. Make the certificate and key readable by your user.
2. Before they expire (about every 90 days), run `tailscale cert` again.
3. Restart `serve`. On Linux: `systemctl --user restart crew-serve`. On macOS: `launchctl kickstart -k gui/$(id -u)/dev.crew.serve`.

To use port 443 and renew without a change to `serve`, run a reverse proxy on the machine (see below). Let the proxy use the same certificate.

## LAN or VPN, behind Caddy or nginx

Keep `serve` on loopback. Let a proxy on the same machine terminate TLS with a Let's Encrypt certificate. The proxy must be reachable only from your LAN or VPN. A DNS-01 challenge issues the certificate for a name that points to a private address. Thus no port is open to the internet.

```sh
crew setup serve
```

Caddy needs a DNS provider module and a `tls { dns <provider> ... }` block for a DNS-01 challenge. Without them, give Caddy a certificate that you already have (`tls cert.pem key.pem`). The example shows only the proxy part:

```caddyfile
agent.example.com {
    reverse_proxy 127.0.0.1:7420
}
```

With nginx, proxy to `http://127.0.0.1:7420`. Forward the WebSocket upgrade for `/api/v1/stream` (`proxy_http_version 1.1; proxy_set_header Upgrade $http_upgrade; proxy_set_header Connection "upgrade"`).

If the proxy cannot get to the loopback of the host (for example, a proxy in a container), it connects through TLS:

1. Start `serve` with `--self-signed --addr 0.0.0.0:7420`.
2. Set the proxy to trust that certificate.

Pair with `crew remote pair --url https://agent.example.com`.

## Cloudflare Tunnel

A tunnel needs no open port. Point `cloudflared` at the loopback `serve`. Put a Cloudflare Access application in front of the hostname. Thus a login occurs at Cloudflare before a request gets to the machine:

```yaml
ingress:
  - hostname: agent.example.com
    service: http://127.0.0.1:7420
  - service: http_status:404
```

Create the Access application for `agent.example.com` with a policy that allows only you. The installed app must also go through Access. Access sessions end, so do one of these:

- Use a long session duration.

Without Access (or a different layer that authenticates), the tunnel is a public endpoint, and the warning above applies.

