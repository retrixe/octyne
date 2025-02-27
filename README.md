# octyne

A process manager with an HTTP API for remote console and file access.

Octyne allows running multiple apps on a remote server and provides an HTTP API to manage them. This allows for hosting web servers, game servers, bots and so on on remote servers without having to mess with SSH, using `screen` and `systemd` whenever you want to make any change, in a highly manageable and secure way.

It incorporates the ability to manage files and access the terminal output and input over HTTP remotely. For further security, it is recommended to use HTTPS (see [config.toml](#configtoml)) to ensure end-to-end secure transmission.

[retrixe/ecthelion](https://github.com/retrixe/ecthelion) complements octyne by providing a web interface to control apps on octyne remotely.

## Quick Start

- [Download the latest version of Octyne from GitHub Releases for your OS and CPU.](https://github.com/retrixe/octyne/releases/latest) Alternatively, you can get the latest bleeding edge version of Octyne from [GitHub Actions](https://github.com/retrixe/octyne/actions?query=branch%3Amain), or by compiling it yourself.
- Place octyne in a folder (on Linux/macOS/\*nix, mark as executable with `chmod +x <octyne file name>`).
- Create a `config.toml` next to Octyne (see [here](https://github.com/retrixe/octyne#configuration) for details).
- Run `./<octyne file name>` in a terminal in the folder to start Octyne. An `admin` user will be generated for you.
- You may want to get [Ecthelion](https://github.com/retrixe/ecthelion) to manage Octyne over the internet, and [octynectl](https://github.com/retrixe/octynectl) as a CLI tool to manage Octyne locally on your machine. [Additionally, make sure to follow the security practices here to prevent attacks against your setup!](https://github.com/retrixe/octyne#security-practices-and-reverse-proxying)
- You might want to manage Octyne using systemd on Linux systems, which can start/stop Octyne, start it on boot, store its logs and restart it on crash. [This article should help you out.](https://medium.com/@benmorel/creating-a-linux-service-with-systemd-611b5c8b91d6)

### Usage

To get the current Octyne version, you can run `./octyne --version` or `./octyne -v`. This does not account for development builds.

## Configuration

Octyne depends on two files in the current working directory to get configuration from. Note that Octyne refers to apps as "servers" in the config and API for legacy reasons (due to originally being targeted towards web servers and Minecraft servers).

The path to these files can be customised using the `--config=/path/to/config.json` and `--users=/path/to/users.json` CLI flags (if relative, resolved relative to the working directory).

### config.toml

Used to configure the apps Octyne should start, Redis-based authentication for allowing more than a single node, Unix socket API, and HTTPS support. A reverse proxy can also be used for HTTPS if it supports WSS.

```toml
port = 42069 # optional, default is 42069

[unixSocket]
# enables Unix socket API for auth-less actions by locally running apps e.g. octynectl
enabled = true
# optional, if absent, default is TMP/octyne.sock.PORT (see API.md for details)
# location = ""
# optional, sets the socket's group owner, if absent, default is current user's primary group
# group = ""

[redis]
# whether the authentication tokens should sync to Redis for more than 1 node
enabled = false
# link to Redis server
url = "redis://localhost"

[https]
# whether Octyne should listen using HTTP or HTTPS
enabled = false
# path to HTTPS certificate
cert = "/path/to/cert.pem"
# path to HTTPS private key
key = "/path/to/key.pem"

[logging]
# whether Octyne should log actions
enabled = true
# path to log files, can be relative or absolute
path = "logs"

[logging.actions]
# optional, disable logging for specific actions, more info below

# each key has the name of the server
[servers.test1]
# optional, default true, Octyne won't auto-start when false
# enabled = true
# the directory in which the server is located
directory = "/home/test/server"
# the command to run to start the server
command = "java -jar spigot-1.12.2.jar"
```

### config.json

In Octyne v1.4.0, JSON was replaced with TOML for configuration. However, JSON still works for backwards compatibility, so the documentation below has been retained. Note that JSON is deprecated and will be removed with v2.0. Migrate to v1.4.0 or later to use TOML.

<details>
<summary>config.json example</summary>

*NOTE: Octyne supports comments and trailing commas in the config.json file, they don't need to be removed.*

```jsonc
{
  "port": 42069, // optional, default is 42069
  "unixSocket": {
    "enabled": true, // enables Unix socket API for auth-less actions by locally running apps e.g. octynectl
    "location": "", // optional, if absent, default is TMP/octyne.sock.PORT (see API.md for details)
    "group": "" // optional, sets the socket's group owner, if absent, default is current user's primary group
  },
  "redis": {
    "enabled": false, // whether the authentication tokens should sync to Redis for more than 1 node
    "url": "redis://localhost" // link to Redis server
  },
  "https": {
    "enabled": false, // whether Octyne should listen using HTTP or HTTPS
    "cert": "/path/to/cert.pem", // path to HTTPS certificate
    "key": "/path/to/key.pem" // path to HTTPS private key
  },
  "logging": {
    "enabled": true, // whether Octyne should log actions
    "path": "logs", // path to log files, can be relative or absolute
    "actions": {} // optional, disable logging for specific actions, more info below
  },
  "servers": {
    "test1": { // each key has the name of the server
      "enabled": true, // optional, default true, Octyne won't auto-start when false
      "directory": "/home/test/server", // the directory in which the server is located
      "command": "java -jar spigot-1.12.2.jar" // the command to run to start the server
    }
  }
}
```

</details>

### users.json

Contains users who can log into Octyne. This file is automatically generated on first start with an `admin` user and a generated secure password which is logged to terminal. You can perform account management via Ecthelion, octynectl or other such tools.

```json
{
  "username": "password hashed with SHA-256 or Argon2id (recommended)"
}
```

Note: actions performed locally using the Unix socket API by apps like [octynectl](https://github.com/retrixe/octynectl) are logged as being performed by the `@local` user, avoid using this username. Apps running on your PC under the same user as Octyne can use this API without a username or password.

### Logging

**Note: Fine-grained control over logging is currently *experimental*. Therefore, action names may change in any version, not just major versions. However, we will generally try to avoid this in the interest of stability.**

By default, Octyne will log all actions performed by users. You can enable/disable logging for specific actions by setting the action to `true` or `false` in the `logging.actions` section in `config.toml`. For example, to disable logging for `auth.login` and `auth.logout`:

```toml
[logging.actions]
"auth.login" = false
"auth.logout" = false
```

Or, with the older `config.json` format:

```json
"actions": {
  "auth.login": false,
  "auth.logout": false
}
```

- Authentication (`auth`): `login`, `logout`
- Configuration (`config`): `reload`, `view`, `edit`
- Account management (`accounts`): `create`, `update`, `delete`
- Server management (`server`):
  - Top-level actions: `start`, `stop`, `kill`
  - Console (`server.console`): `access`, `input`
  - Files (`server.files`): `upload`, `download`, `createFolder`, `delete`, `move`, `copy`, `bulk`, `compress`, `decompress`

## Security Practices and Reverse Proxying

Use HTTPS to ensure end-to-end secure transmission. This is easy with Certbot and a reverse proxy like nginx or Apache (if you don't want to use Octyne's built-in HTTPS support). A reverse proxy can rate limit requests to Octyne as well, and put both Octyne and Ecthelion behind the same domain under different endpoints too! (⚠️ Or, under different subdomains, if you want, but this interferes with cookie authentication.)

### Sample nginx Config

```nginx
location /console {
    # Remember to set the basePath in Ecthelion's config.json to /console (or whatever you pick)!
    proxy_pass http://127.0.0.1:3000;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
}

location /octyne {
    rewrite /octyne/(.*) /$1 break;
    proxy_pass http://127.0.0.1:42069;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    # Adjust this as necessary for file uploads:
    client_max_body_size 1024M;
    # Required for WebSocket functionality to work:
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "Upgrade";
}

```

### Sample Apache Config

Note: Ensure `mod_proxy` is loaded.

```apache
<VirtualHost *:443>
  # Remember to set the basePath in Ecthelion's config.json to /console (or whatever you pick)!
  # Ecthelion
  ProxyPass /console http://127.0.0.1:4200/console
  ProxyPassReverse /console http://127.0.0.1:4200/console
  # Octyne
  Protocols h2 h2c http/1.1
  ProxyPassMatch ^/octyne/(server/.*/console)$  ws://127.0.0.1:42069/$1
  ProxyPass /octyne http://127.0.0.1:42069
  ProxyPassReverse /octyne http://127.0.0.1:42069
</VirtualHost>
```
