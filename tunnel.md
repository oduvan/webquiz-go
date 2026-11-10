# SSH tunnel

The tunnel makes a WebQuiz server on a local computer reachable from the internet, through an SSH tunnel server. Students then open a public address such as `https://tunnel.example.com/tests/a3f7b2/`.

WebQuiz in Go (0.1 and later) uses the same protocol and the same tunnel server setup as 1.19 (Python). One tunnel server can serve 1.19 Python servers and Go servers at the same time, and nothing changes on the tunnel server when you update.

The code is in `internal/tunnel` (the tunnel itself) and `internal/server/tunnel.go` (the admin buttons and the status messages). The user guide describes the same in `docs/en/040_config.md`.

## Config

The `tunnel` section of `webquiz.yaml` is a mapping:

```yaml
tunnel:
  server: tunnel.example.com        # host name or IP address of the tunnel server
  public_key: keys/id_ed25519.pub   # path of the public key file
  private_key: keys/id_ed25519      # path of the private key file
  socket_name: room-12              # optional: a fixed name; without it every connection gets a random one
  config:                           # optional: the tunnel server settings, so nothing is downloaded
    username: tunneluser
    socket_directory: /var/run/tunnels
    base_url: https://tunnel.example.com/tests
```

- Without `server` there is no tunnel: the admin panel shows no tunnel part.
- A relative key path is read from the program's folder (`WEBQUIZ_BINARY_DIR`), like the other paths of the binary.
- `config` must have all three keys. When it is there, WebQuiz does not download `tunnel_config.yaml`.
- `socket_name` must be unique on the tunnel server. Use letters, digits, `-`, `_` and `.`. File transfer between servers needs it: other servers find this one by its name.
- A config save applies a changed `tunnel` section to the next connection and closes an open tunnel (the admin connects again). Adding a `tunnel` section to a running server that had none needs a restart.

## At start: the keys

When the config names a tunnel server, WebQuiz checks the two key files at start. It does not connect yet.

- Both files are missing: WebQuiz makes a new ED25519 key pair without a passphrase. The private key is in OpenSSH format and only its owner may read it (mode 600). The public key file has one line: `ssh-ed25519 <key> webquiz@tunnel`.
- Only one of the two files exists: the admin panel shows an error, for example "Public key exists but private key is missing".
- Both files exist but do not hold usable keys: the admin panel shows "Keys exist but are invalid" with the reason.
- A key path is missing in the config: the admin panel shows "SSH key paths not configured".

The admin panel shows the tunnel server, the fixed socket name (if there is one), the public key with a copy button, and the **Connect** button. The teacher adds the public key to `~/.ssh/authorized_keys` of the tunnel user on the tunnel server.

## Connect

When the admin clicks **Connect**, WebQuiz:

1. Gets the tunnel server settings: from `tunnel.config`, or from `https://{server}/tunnel_config.yaml`. It uses plain `http://` when `server` is an IP address (an IPv6 address goes in brackets). The file must have `username`, `socket_directory` and `base_url`; other keys are ignored. A redirect counts as an error, and the download gives up after 30 seconds.
2. Takes the socket name: `socket_name`, or a new random name of 6 or 8 hex characters.
3. Opens an SSH connection to port 22 of `server` as `username`, with the private key. It does not check the host key of the tunnel server (1.19 did not either). The login gives up after 120 seconds.
4. Asks the tunnel server to listen on the Unix socket `{socket_directory}/{socket name}` (the OpenSSH request `streamlocal-forward@openssh.com`).
5. From then on, every connection to that socket comes back over SSH (a `forwarded-streamlocal@openssh.com` channel), and WebQuiz passes it to its own port on `127.0.0.1`.

The public address is `{base_url}/{socket name}/`. The admin panel shows it in the tunnel part and in the list of addresses, and the **Connect** button becomes **Disconnect**. Every status change goes to the open admin pages as a `tunnel_status` message on the admin WebSocket (`/ws/admin`).

**Disconnect** asks the tunnel server to stop listening (`cancel-streamlocal-forward@openssh.com`), closes the SSH connection and stops reconnecting.

When a working connection is lost, WebQuiz connects again. It waits 5 seconds before the first try, and after each failed try twice as long as before, at most 300 seconds (5 minutes). It keeps trying until it is connected again or the admin clicks **Disconnect**.

## Tunnel server setup

This is the same as for 1.19.

- **SSH server.** The tunnel user (`username`) logs in with the keys in its `~/.ssh/authorized_keys` and may make the server listen on Unix sockets in `socket_directory` (remote forwarding). WebQuiz needs nothing else: direct stream-local connections from clients can stay blocked. With a fixed `socket_name`, a reconnect can fail while the old socket file still exists. `StreamLocalBindUnlink yes` in the sshd config avoids this. Without it, the old file stays until the tunnel server's cleanup job removes it (every 5 minutes).
- **Web server.** It serves `tunnel_config.yaml` at `https://{server}/tunnel_config.yaml` (not needed when every WebQuiz has `tunnel.config`). It sends requests for `{base_url}/{name}/...` to the Unix socket `{socket_directory}/{name}`, without the `{base_url}/{name}` part of the path: WebQuiz gets `/`, `/api/...` and so on. The quiz page uses relative addresses, so it works under that prefix.
- **Visitor address.** The web server adds the visitor's address to `X-Forwarded-For` (in nginx: `$proxy_add_x_forwarded_for`). Tunnel connections reach WebQuiz from `127.0.0.1`; WebQuiz trusts `X-Forwarded-For` only from there and takes its last entry. So visitors through the tunnel count as public addresses and cannot open the admin pages.

## File transfer through the tunnel

Servers on the same tunnel server with the same SSH user (one `socket_directory`) can send quiz, log and CSV files to each other. They talk over the public addresses (`{base_url}/{name}/api/transfer/...`), not over SSH. Each server needs a fixed `socket_name`. The details are in `internal/transfer` and in the "File transfer between servers" part of `CLAUDE.md`.
