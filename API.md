# Octyne API Documentation

Octyne provides a REST API for interacting with the server (listening on port 42069 by default, but configurable). This API is used by the [Ecthelion web interface](https://github.com/retrixe/ecthelion) and can be used by other applications to interact with Octyne.

## Unix Socket API

Octyne v1.2+ provides a Unix socket API on Windows 10+ and Unix-like systems which is located by default at `TEMPDIR/octyne.sock.PORT`, where `TEMPDIR` is retrieved from <https://pkg.go.dev/os#TempDir> and `PORT` is the port specified in the config (the default is 42069). If using an older version of Windows, the Unix socket API will be unavailable.

This API is identical to the REST API in usage, with the same endpoints/params/etc. You can send HTTP requests to this API without requiring token authentication, only necessary system user/group privileges, which is useful for local actions performed by applications like [octynectl](https://github.com/retrixe/octynectl). Actions performed through the Unix socket API are logged as being performed by the `@local` user.

## Authentication

Retrieve a token using the [GET /login](#get-login) endpoint and store it safely. You can then pass this token to all subsequent requests to Octyne in the `Authorization` header or as an `X-Authentication` cookie (⚠️ supported since v1.1+, v1.0 has broken support with logout and ticket endpoints).

If using [the console API endpoint](#ws-serveridconsoleticketticket) or [the file download API endpoint](#get-serveridfilepathpathticketticket), you can use the one-time ticket system to make the use of these endpoints in the browser JavaScript environment convenient. Use [GET /ott (one-time ticket)](#get-ott-one-time-ticket) to retrieve a ticket using your token (same as requests to any other endpoint), then pass it in the URL query parameters. A ticket is valid for 30 seconds, tied to your account and IP address, and can only be used once.

## Errors

All endpoints may return an error. Errors are formatted in JSON in the following format: `{"error": "error description here"}`.

Currently, possible errors are not documented. This will be done in the future. Contributions in this department are welcome!

## Endpoints

- [GET /](#get-)
- [GET /login](#get-login)
- [GET /logout](#get-logout)
- [GET /ott (one-time ticket)](#get-ott-one-time-ticket)
- [GET /config](#get-config)
- [PATCH /config](#patch-config)
- [GET /config/reload](#get-configreload)
- [GET /accounts](#get-accounts)
- [POST /accounts](#post-accounts)
- [PATCH /accounts?username=username](#patch-accountsusernameusername)
- [DELETE /accounts?username=username](#delete-accountsusernameusername)
- [GET /servers](#get-servers)
- [GET /server/{id}](#get-serverid)
- [POST /server/{id}](#post-serverid)
- [WS /server/{id}/console?ticket=ticket](#ws-serveridconsoleticketticket)
- [GET /server/{id}/files?path=path](#get-serveridfilespathpath)
- [GET /server/{id}/file?path=path&ticket=ticket](#get-serveridfilepathpathticketticket)
- [POST /server/{id}/file?path=path](#post-serveridfilepathpath)
- [POST /server/{id}/folder?path=path](#post-serveridfolderpathpath)
- [DELETE /server/{id}/file?path=path](#delete-serveridfilepathpath)
- [PATCH /server/{id}/file](#patch-serveridfile)
- [POST /server/{id}/compress?path=path&compress=algorithm&archiveType=archiveType](#post-serveridcompresspathpathcompressalgorithmarchivetypearchivetype)
- [POST /server/{id}/decompress?path=path](#post-serveriddecompresspathpath)

### GET /

Get the running version of Octyne. Added in v1.1.0.

**Response:**

HTTP 200 JSON body response containing the Octyne version e.g. `{"version":"1.2.0"}`.

⚠️ *Warning:* On v1.0, this will return a non-JSON response `Hi, octyne is online and listening to this port successfully!`

---

### GET /login

This is the only endpoint which doesn't require authentication, since it is the login endpoint to obtain authentication tokens from.

**Request Query Parameters:**

- `cookie` - Optional, defaults to `false`. If set to `true`, the token will be returned in a cookie named `X-Authentication` instead, with a 3 month expiry, `SameSite=Strict` and `HttpOnly` (no `Secure`, since `SameSite=Strict` covers that when HTTPS is in use). Added in v1.1.0.

**Request Headers:**

- `Username` - The username of the account being logged into.
- `Password` - The password of the account being logged into.

**Response:**

HTTP 200 JSON body response with the token is returned on success, e.g. `{"token":"RCuRbzzSa51lNByCu+aeYXxoSeaO4HQgMJQ82gWqdSTPm7cHWCQxk7LoQEa8AIkiLBUQXCkkYF8gLHC3lOPfMVU4oU8rXGhQ1EB3VFP30VP2Dv7MG9clAsxuv2x+0jP5"}`.

If the `cookie` query parameter is `true` and Octyne v1.1+ is in use, then the body will be `{"success":true}` instead, and the token will be contained in the `X-Authentication` cookie in `Set-Cookie` header (see `cookie`'s documentation for details).

---

### GET /logout

Logout from Octyne. This invalidates your authentication token.

**Response:**

HTTP 200 JSON body response `{"success":true}` is returned on success.

---

### GET /ott (one-time ticket)

Provides you with a one-time ticket which can be used for authenticating with certain endpoints (see the [Authentication](#authentication) section for more details).

**Response:**

HTTP 200 JSON body response with the ticket e.g. `{"ticket":"UTGA3Q=="}` is returned on success. This ticket is tied to your account, IP address, can be used for one request only, and will expire in 30 seconds.

---

### GET /config

Get Octyne's configuration file contents. Added in v1.1.0.

**Response:**

HTTP 200 response with the configuration file contents in the body. The configuration file format is documented in the [README](https://github.com/retrixe/octyne/blob/main/README.md).

⚠️ *Warning:* The configuration file uses [HuJSON](https://github.com/tailscale/hujson) instead of JSON! This means that comments and trailing commas are allowed. Don't parse the body assuming that it's JSON.

---

### PATCH /config

Modify Octyne's configuration. Added in v1.1.0.

**Request Body:**

New configuration file contents in the body. The configuration file format is documented in the [README](https://github.com/retrixe/octyne/blob/main/README.md) and is formatted with [HuJSON](https://github.com/tailscale/hujson).

**Response:**

HTTP 200 JSON body response `{"success":true}` is returned on success.

---

### GET /config/reload

This endpoint tells Octyne to reload `config.json` from disk. Added in v1.1.0.

**Response:**

HTTP 200 JSON body response `{"success":true}` is returned on success.

---

### GET /accounts

Get a list of all accounts. Added in v1.1.0.

**Response:**

HTTP 200 JSON body response with an array of all usernames e.g. `["user1", "user2"]` is returned on success.

---

### POST /accounts

This endpoint can be used to create a new account. Added in v1.1.0.

**Request Body:**

A JSON body containing the username and password of the account to be created, e.g. `{"username":"user1", "password":"password1"}` (don't use these usernames or passwords in production lol).

**Response:**

HTTP 200 JSON body response `{"success":true}` is returned on success.

---

### PATCH /accounts?username=username

This endpoint can be used to change the username/password of an account. Added in v1.1.0.

**Request Query Parameters:**

- `username` - The username of the account to be changed. When missing, the username in the body is used instead. You must use this parameter if you want to rename an account. Added in v1.2.0.

**Request Body:**

A JSON body containing the new username and password of the account, e.g. `{"username":"user1", "password":"password1"}` (don't use these usernames or passwords in production lol).

⚠️ *Warning:* If no username is provided in the query parameters, the username in the body is used instead! This means that if you want to rename an account, you must use the `username` query parameter to specify the old username. However, the query parameter is only available since v1.2+! **On older versions, you cannot rename accounts, and the query parameter will not be used for changing passwords either!** If you want to avoid issues, **don't allow the user to change both the username and password at the same time, or you may end up changing the password of a different user instead of renaming the current one!** On older versions, attempting to change only the username will give you a "Username or password not provided!" error. Alternatively, you can implement a version check with the `GET /` endpoint.

**Response:**

HTTP 200 JSON body response `{"success":true}` is returned on success.

---

### DELETE /accounts?username=username

Delete an account. Added in v1.1.0.

**Response:**

HTTP 200 JSON body response `{"success":true}` is returned on success.

---

### GET /servers

Get a list of all servers along with basic information about them.

**Request Query Parameters:**

- `extrainfo` - Optional, defaults to `false`. If set to `true`, the response will include extra information about the server, currently whether or not the server is marked for deletion (`toDelete`). Added in v1.2.0.

**Response:**

HTTP 200 JSON body response with the status of all servers e.g. `{"servers":{"app1":0, "app1": 1}}` is returned on success.

- `0` means the returned server is not running.
- `1` means the returned server is currently running.
- `2` means the returned server has crashed and is not currently running.

If the query parameter `extrainfo` is `true`, then the response will include extra information about the server, like so:

```json
{
  "servers": {
    "app1": { "status": 0, "toDelete": true },
    "app2": { "status": 1, "toDelete": false }
  }
}
```

---

### GET /server/{id}

Get info about a specific server/app.

**Response:**

HTTP 200 JSON body response with information about the app.

- `status` - The status of the app, `0` for not running, `1` for running, `2` for crashed.
- `uptime` - The uptime of the app in nanoseconds.
- `cpuUsage` - The CPU usage of the app in percent.
- `memoryUsage` - The memory usage of the app in bytes.
- `totalMemory` - The total memory available to the app in byte.
- `toDelete` - Whether or not the app is marked for deletion.

e.g.

```json
{
  "status":      0,
  "uptime":      60000000000,
  "cpuUsage":    70,
  "memoryUsage": 1073741824,
  "totalMemory": 8589934592,
  "toDelete":    false
}
```

---

### POST /server/{id}

Start, stop or kill a server/app.

**Request Body:**

Either of the following words in the body:

- `START` - Start the server.
- `STOP` - Kill the server with SIGKILL. ⚠️ *Warning:* Deprecated in v1.1 in favour of `KILL` and `TERM`.
- `KILL` - Kill the server with SIGKILL. Added in v1.1.
- `TERM` - Gracefully stop the server with SIGTERM. Added in v1.1.

**Response:**

HTTP 200 JSON body response `{"success":true}` is returned on success.

---

### WS /server/{id}/console?ticket=ticket

Connect to the console of a server/app to receive its input/output. This endpoint is a WebSocket endpoint.

**Request Query Parameters:**

- `ticket` - Optional. For browsers and other such environments where you cannot set custom headers, you can use one-time tickets as described in the [Authentication](#authentication) section instead of setting the `Authorization` header.

**WebSocket Protocols:**

- ⚠️ None provided: If no protocol is specified, the old protocol is used by default. This only exists for backwards compatibility! Avoid using this protocol if possible.
- `console-v2`: This is the recommended protocol to use, since it has a proper extensible format and supports keep alives. Added in v1.1.0.

*Info:* All messages with either protocol are encoded as WebSocket text messages.

**Default protocol:**

⚠️ *Warning:* This protocol is relatively simple, but has issues on newer versions of Octyne (v1.1+), where you may see the WebSocket timeout when there is no activity. It is recommended to use the `console-v2` protocol instead, as this protocol will likely be removed with Octyne v2+.

After establishing a connection, you receive all the output logs from the app so far, and you continue to receive logs line-by-line. You can send input to the app by sending the input string over the WebSocket connection.

**console-v2 protocol:**

In this protocol, all messages are encoded in JSON strings in the following format:

```json
{
  "type": "type",
  // ... other fields
}
```

The client may receive messages of the following types:

- `settings` - This is sent upon initial connection, and is currently unused and has no fields.
- `error` - This is sent when an error occurs, and has the following fields:
  - `message` - The error message.
- `output` - This contains output from the app, sent in sequential order, and has the following fields:
  - `data` - The output received from the app. This can be appended to the previous output with a newline, and an `output` message may contain multiple lines joined with `\n` as well.
- `pong` - This is sent in response to a `ping` message, and has the following fields:
  - `id` - The ID from the client's `ping` message.

The client may send messages of the following types:

- `ping` - This is sent to check if the connection is still alive, and has the following fields:
  - `id` - A unique ID for this ping message. The server will respond with a `pong` message with the same ID.
- `input` - This is sent to send input to the app, and has the following fields:
  - `data` - The input to send to the app.

A client will receive the output from the app so far upon initial connection, will continue to receive output line-by-line, and can send input to the app, just like the older, deprecated v1 protocol. Clients should send a `ping` message every few seconds to keep the connection alive, as Octyne enforces a 30 second timeout.

---

### GET /server/{id}/files?path=path

Get a list of all files in a folder in the working directory of the app.

**Request Query Parameters:**

- `path` - The path of the folder to list contents of. This is relative to the server's root directory.

**Response:**

HTTP 200 JSON body response with an array of all files in the folder.

Properties include:

- `folder` - Whether or not the file is a folder.
- `name` - The name of the file.
- `size` - The size of the file in bytes.
- `lastModified` - The last modified time of the file in seconds since the Unix epoch.
- `mimeType` - The MIME type of the file.

Example response:

```json
{
  "contents": [
    {
      "folder": false,
      "name": "config.json",
      "size": 1284,
      "lastModified": 1600000000,
      "mimeType": "application/json"
    }
  ]
}
```

---

### GET /server/{id}/file?path=path&ticket=ticket

Download a file from the working directory of the app.

**Request Query Parameters:**

- `path` - The path of the file to download. This is relative to the server's root directory.
- `ticket` - Optional. For browsers and other such environments where you cannot set custom headers, you can use one-time tickets as described in the [Authentication](#authentication) section instead of setting the `Authorization` header.

**Response:**

HTTP 200 response with the file contents in the body is returned on success.

**Response Headers:**

These are helpful for apps and allow browsers to download files directly from this endpoint as well (provided you use a one-time ticket/pass `Authorization` header somehow).

- `Content-Disposition` - The filename of the file being downloaded e.g. `Content-Disposition: attachment; filename=file.txt`.
- `Content-Type` - The MIME type of the file being downloaded.
- `Content-Length` - The length of the file being downloaded.

---

### POST /server/{id}/file?path=path

Upload a file to the working directory of the app.

**Request Query Parameters:**

- `path` - The path to the folder where the file should be uploaded. This is relative to the server's root directory.

**Request Body:**

The body should be multipart form data, where the contents of the file you want to upload should be in a key named `upload`, and the filename in its metadata should correctly reflect the filename you want once it's uploaded. The upload limit is 5 GB since v1.1+ (was previously 100 MB in v1.0).

**Response:**

HTTP 200 JSON body response `{"success":true}` is returned on success.

---

### POST /server/{id}/folder?path=path

Create a folder in the working directory of the app.

**Request Query Parameters:**

- `path` - The path where the folder should be created. This is relative to the server's root directory.

**Response:**

HTTP 200 JSON body response `{"success":true}` is returned on success.

---

### DELETE /server/{id}/file?path=path

Delete a file or folder in the working directory of the app.

**Request Query Parameters:**

- `path` - The path of the file or folder to delete. This is relative to the server's root directory.

**Response:**

HTTP 200 JSON body response `{"success":true}` is returned on success.

---

### PATCH /server/{id}/file

Move or copy a file or folder in the working directory of the app.

**Request Body:**

- Old format (⚠️ *Warning:* deprecated): `mv` or `cp`, the old path of the file/folder, and the new path, joined together by new lines, e.g.

  ```text
  mv
  /config.json
  /config.json.old
  ```

- New format (added in v1.1+): Similar in working to the old format, but uses JSON to encode the body, e.g.
  
  ```json
  {
    "operation": "mv",
    "src": "/config.json",
    "dest": "/config.json.old"
  }
  ```

---

### POST /server/{id}/compress?path=path&compress=algorithm&archiveType=archiveType

Compress files/folders in the working directory of the app into a ZIP or TAR archive. Support for `tar(.gz/xz/zst)` archives was added in v1.2+.

⚠️ *Info:* The `POST /server/{id}/compress/v2` API is available as well, which is identical to this, but guaranteed to support `tar` archives, and can be used by API clients to ensure archives aren't accidentally created as ZIP files on older Octyne versions.

**Request Query Parameters:**

- `path` - The location to create the archive at. This is relative to the server's root directory.
- `archiveType` - Optional, default is `zip`. This specifies the archive type to use, currently, `zip` and `tar` are supported. Added in v1.2+.
- `compress` - Optional, default is `true`, possible values are `true`/`false`, and `gzip`/`zstd`/`xz` for `tar` archives. This specifies whether or not to compress files/folders in the archive. `true` corresponds to the default DEFLATE algorithm for `zip`, and GZIP for `tar`. ⚠️ *Warning:* This was broken in v1.0 (it accidentally used a header instead of query parameter), this was fixed in v1.1+.

**Request Body:**

A JSON body containing an array of paths to compress (relative to the server's root directory), e.g. `["/config.json", "/logs"]`.

⚠️ *Warning:* This API was unable to compress folders in v1.0, this function was added in v1.1+.

**Response:**

HTTP 200 JSON body response `{"success":true}` is returned on success.

---

### POST /server/{id}/decompress?path=path

Decompress a ZIP or TAR archive in the working directory of the app.

The full list of supported archive formats is:

- `.zip`
- `.tar(.gz/bz2/bz/zst/xz)` and their respective short forms `tgz/txz/tbz/tbz2/tzst`, support for these was added in v1.2+.

**Request Query Parameters:**

- `path` - The path of the archive to decompress. This is relative to the server's root directory.

**Request Body:**

A string containing the path to decompress the archive to (relative to the server's root directory). A folder will be created at this path, to which the contents of the archive will be extracted.

**Response:**

HTTP 200 JSON body response `{"success":true}` is returned on success.

⚠️ Tip when attempting to decompress unsupported archive formats like `tar` on Octyne v1.1 and older: Check if the error is `An error occurred when decompressing ZIP file!` v1.2+ says `archive` instead of `ZIP file` and explicitly blocks unsupported archive types. This can help you inform the user if their Octyne installation is out of date, since decompressing these archives will fail on older versions.
