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
- [POST /server/{id}/compress?path=path&compress=boolean](#post-serveridcompresspathpathcompressboolean)
- [POST /server/{id}/decompress?path=path](#post-serveriddecompresspathpath)

### GET /

Get the running version of Octyne. Added in v1.1.0.

**Response:**

HTTP 200 JSON body response containing the Octyne version e.g. `{"version":"1.2.0"}`.

⚠️ Warning: On v1.0, this will return a non-JSON response `Hi, octyne is online and listening to this port successfully!`

---

### GET /login

This is the only endpoint which doesn't require authentication, since it is the login endpoint to obtain authentication tokens from.

**Request Query Parameters:**

- `cookie` - Optional, defaults to `false`. If set to `true`, the token will be returned in a cookie in addition to the response body. Added in v1.1.0.

**Request Headers:**

- `Username` - The username of the account being logged into.
- `Password` - The password of the account being logged into.

**Response:**

HTTP 200 JSON body response with the token is returned on success, e.g. `{"token":"RCuRbzzSa51lNByCu+aeYXxoSeaO4HQgMJQ82gWqdSTPm7cHWCQxk7LoQEa8AIkiLBUQXCkkYF8gLHC3lOPfMVU4oU8rXGhQ1EB3VFP30VP2Dv7MG9clAsxuv2x+0jP5"}`

---

### GET /logout

Logout from Octyne. This invalidates your authentication token.

**Response:**

HTTP 200 JSON body response `{"success":true}` is returned on success.

---

### GET /ott (one-time ticket)

Provides you with a one-time ticket which can be used for authenticating with certain endpoints (see the s[Authentication](#authentication) section for more details).

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

⚠️ *Warning:* If no username is provided in the query parameters, the username in the body is used instead. This means that if you want to rename an account, you must use the `username` query parameter to specify the old username. However, the query parameter is only available since v1.2+! On older versions, you cannot rename accounts, and this query parameter will not be recognised for changing passwords either!

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

### POST /server/{id}

TODO: document that STOP is deprecated in favour of KILL and TERM in v1.1

### WS /server/{id}/console?ticket=ticket

### GET /server/{id}/files?path=path

### GET /server/{id}/file?path=path&ticket=ticket

### POST /server/{id}/file?path=path

TODO: document upload limit changes

### POST /server/{id}/folder?path=path

### DELETE /server/{id}/file?path=path

### PATCH /server/{id}/file

TODO: document new JSON request format

### POST /server/{id}/compress?path=path&compress=boolean

TODO: note that compress was broken in v1.0, and recursive folder compression was only supported in v1.1, therefore v1.0 api is incomplete

### POST /server/{id}/decompress?path=path

## Note

This documentation is still being worked on. While all endpoints are listed, they are not yet fully documented e.g. basic information for many endpoints has not been filled, and possible errors are not yet documented.
