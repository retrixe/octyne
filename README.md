# octyne

A process manager with a web dashboard to access console and files remotely.

Octyne allows running multiple apps on a remote server and provides a web dashboard and HTTP API to manage them. It features the ability to access files and terminal input/output remotely over HTTP. This lets you host apps like web servers, game servers, or bots, without needing to use unfriendly tools like SSH or `screen` to manage them.

Octyne's built-in Web UI is developed as part of the [Ecthelion](https://github.com/retrixe/ecthelion) project. [Octyne's HTTP API is fully documented as well.](/docs/API.md)

## Table of Contents

- [Setup Recipes](#setup-recipes)
- [Quick Start](#quick-start)
  - [systemd Setup](#systemd-setup)
- [Configuration](#configuration)
  - [config.json](#configjson)
  - [Accounts](#accounts)
  - [Logging](#logging)
- [Multi-node Setup](#multi-node-setup)
- [HTTPS Setup](#https-setup)
  - [Sample Caddy Setup](#sample-caddy-setup)
  - [Sample nginx Config](#sample-nginx-config)
  - [Sample Apache Config](#sample-apache-config)

## Setup Recipes

You can follow these recipes to quickly set up Octyne for various use cases:

- [Host a Minecraft server with Octyne](/docs/recipes/HOST_MINECRAFT_SERVER.md)

Contributions to add more recipes are welcome!

## Quick Start

- [Download the latest version of Octyne from GitHub Releases for your OS and CPU.](https://github.com/retrixe/octyne/releases/latest)
- Place octyne in a folder (on Linux/macOS/\*nix, mark as executable with `chmod +x <octyne file name>`).
- Create a `config.json` next to Octyne (see [the configuration section](https://github.com/retrixe/octyne#configuration) for details).
- Run `./<octyne file name>` in a terminal in the folder to start Octyne. An `admin` user will be generated for you.
- You can now access the Octyne web dashboard at `http://<your server's IP>:7877` and the API at `http://<your server's IP>:42069`!
- Install [octynectl](https://github.com/retrixe/octynectl) to manage Octyne from the terminal. [Additionally, make sure to setup HTTPS!](https://github.com/retrixe/octyne#https-setup)

You might want to use `systemd` on Linux to start/stop Octyne automatically for you.

To get the current Octyne version, you can run `./octyne --version` or `./octyne -v`. This does not account for development builds.

### systemd Setup

Create a file named `octyne.service` in `/etc/systemd/system/` with the following content:

<details>
<summary>octyne.service</summary>

```ini
[Unit]
Description=Octyne
After=network.target
StartLimitIntervalSec=0

[Service]
Type=simple
Restart=on-failure
RestartSec=1
# Replace `abcxyz` with your Linux account username.
User=abcxyz
WorkingDirectory=/home/abcxyz/octyne/
# Install Octyne to /usr/local/bin/ to avoid issues with SELinux on Red Hat-based distros.
# If using SELinux, run `sudo restorecon /usr/local/bin/octyne` after moving the binary.
ExecStart=/usr/local/bin/octyne

[Install]
WantedBy=multi-user.target
```

</details>

Then simply run `sudo systemctl enable --now octyne.service` to enable and start Octyne.

## Configuration

Octyne relies on the `config.json` and `users.json` files in the current working directory to get configuration from.

The path to these files can be customised using the `--config=/path/to/config.json` and `--users=/path/to/users.json` CLI flags.

### config.json

This file contains Octyne's settings, such as all the apps Octyne should start, Redis support for authentication, etc.

*NOTE: Octyne supports comments and trailing commas in the config.json file, they don't need to be removed.*

Example `config.json` file:

```jsonc
{
  "servers": {
    "minecraft1": {
      "enabled": true,
      "directory": "/home/user/minecraft1",
      "command": "java -Xmx1024M -Xms1024M -jar spigot.jar nogui"
    },
    "minecraft2": {
      "enabled": true,
      "directory": "/home/user/minecraft2",
      "command": "java -Xmx1024M -Xms1024M -jar spigot.jar nogui"
    }
  }
}
```

<details>
<summary>Full config.json documentation</summary>

```jsonc
{
  "port": 42069, // optional, default is 42069
  "webUI": {
    "enabled": true, // optional, default true, whether the Octyne Web UI should be enabled
    "port": 7877 // optional, default is 7877, the port on which the Web UI listens
  },
  "unixSocket": {
    "enabled": true, // enables Unix socket API for auth-less actions by locally running apps e.g. octynectl
    "location": "", // optional, if absent, default is TMP/octyne.sock.PORT (see API.md for details)
    "group": "" // optional, sets the socket's group owner, if absent, default is current user's primary group
  },
  "redis": {
    // whether Octyne should use Redis for authentication, mainly useful for multi-node setups
    // to show them in the same Web UI and share user accounts/login sessions.
    // Redis will also persist login sessions across restarts.
    "enabled": false,
    "url": "redis://localhost", // link to Redis server
    "role": "primary" // role of this node, primary or secondary
    // note: there should be 1 primary node to manage/authenticate users in a multi-node setup!
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

### Accounts

The `users.json` file is used to store Octyne accounts. This file is automatically generated on first start with an `admin` user and a generated secure password which is logged to terminal. Modifying this file is not recommended, since the format is not fixed! You can perform account management via Octyne Web UI, Ecthelion, octynectl or other such tools.

Actions performed locally using the Unix socket API by apps like [octynectl](https://github.com/retrixe/octynectl) are logged as being performed by the `@local` user. Usernames starting with `@` are reserved for this reason. Apps running on your PC can use this API without a username or password (provided they run under the same system user).

### Logging

**Note: Fine-grained control over logging is currently *experimental*. Therefore, action names may change in any version, not just major versions. However, we will generally try to avoid this in the interest of stability.**

By default, Octyne will log all actions performed by users. You can enable/disable logging for specific actions by setting the action to `true` or `false` in the `logging.actions` object in `config.json`. For example, to disable logging for `auth.login` and `auth.logout`, your `actions` object would be:

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

## Multi-node Setup

A multi-node setup allows you to run Octyne multiple times across different servers, and manage them from a single Web UI (via standalone Ecthelion). This is useful for scaling your applications or managing multiple servers from one place.

In such a setup, you can use Redis to share user accounts and login sessions across nodes. Set `"redis.enabled": true` in `config.json` on all nodes, ensure they all connect to the same Redis server, and configure the `role` field in the Redis config.

The `role` field in the Redis config determines whether a node is a primary or secondary node. The primary node is responsible for managing user accounts and sessions, while secondary nodes rely on the primary node instead of managing user accounts and sessions themselves. Attempting to login/logout or manage accounts on a secondary node will result in an error, and they will not read the `users.json` file either.

You should have one (and only one!) primary node in your multi-node setup, which will handle user management and authentication. All other nodes should be set to the secondary role.

For a Web UI to manage your multi-node Octyne setup, setup a standalone Ecthelion instance. [Read the Ecthelion documentation for details on how to set it up.](https://github.com/retrixe/ecthelion#quick-start-standalone) Ecthelion's `config.json` must be configured with the primary node's URL in `ip` along with the URL of each secondary node in the `nodes` field.

## HTTPS Setup

HTTPS ensures end-to-end secure transmission. Using a reverse proxy like Caddy, Apache or nginx makes it easy to setup HTTPS, and allows you to setup rate limits (not yet built into Octyne).

If you are unfamiliar with web servers, Caddy is the ***simplest*** way to set up HTTPS, as it automatically obtains and renews certificates for you.

Sample configs for nginx and Apache are provided below too. You must setup HTTPS yourself with these web servers (Certbot is an easy way to do so).

### Sample Caddy Setup

Simply run `caddy reverse-proxy --from :7877 --to :8000` to setup the Octyne Web UI with HTTPS on port 8000! For more advanced setups e.g. combining Octyne with standalone Ecthelion, [read the Caddy documentation.](https://caddyserver.com/docs/quick-starts/reverse-proxy)

### Sample nginx Config

```nginx
location /octyne {
    rewrite /octyne/(.*) /$1 break;
    # Use 7877 for Web UI or 42069 for API, you typically want the Web UI unless using Ecthelion standalone
    proxy_pass http://127.0.0.1:7877;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    # Adjust this as necessary for file uploads:
    client_max_body_size 1024M;
    # Required for WebSocket functionality to work:
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "Upgrade";
}

# Ecthelion (this section is needed ONLY if using Ecthelion standalone)
location /console {
    # Remember to set the basePath in Ecthelion's config.json to /console (or whatever you pick)!
    proxy_pass http://127.0.0.1:3000;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
}
```

### Sample Apache Config

Note: Ensure `mod_proxy` is loaded.

```apache
<VirtualHost *:443>
  # Octyne
  Protocols h2 h2c http/1.1
  # Use 7877 for Web UI or 42069 for API, you typically want the Web UI unless using Ecthelion standalone
  ProxyPassMatch ^/octyne/(server/.*/console)$  ws://127.0.0.1:7877/$1
  ProxyPass /octyne http://127.0.0.1:7877
  ProxyPassReverse /octyne http://127.0.0.1:7877

  # Ecthelion (this section is needed ONLY if using standalone Ecthelion)
  # Remember to set the basePath in Ecthelion's config.json to /console (or whatever you pick)!
  ProxyPass /console http://127.0.0.1:4200/console
  ProxyPassReverse /console http://127.0.0.1:4200/console
</VirtualHost>
```
