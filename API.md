# Octyne API Documentation

## Authentication

Retrieve a token using the [GET /login](#get-login) endpoint and store it safely. You can then pass this token to all subsequent requests to Octyne in the `Authorization` header or as an `X-Authentication` cookie.

If using [the console API endpoint](#ws-serveridconsoleticketticket) or [the file download API endpoint](#get-serveridfilepathpathticketticket), you can use the one-time ticket system to make the use of these endpoints in the browser JavaScript environment convenient. Use [GET /ott (one-time ticket)](#get-ott-one-time-ticket) to retrieve a ticket using your token (same as requests to any other endpoint), then pass it in the URL query parameters. A ticket is valid for 30 seconds, tied to your account and IP address, and can only be used once.

## Endpoints

All endpoints may return an error. Errors are formatted in JSON in the following format: `{"error": "error description here"}`.

- [GET /login](#get-login)
- [GET /logout](#get-logout)
- [GET /ott (one-time ticket)](#get-ott-one-time-ticket)
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

### GET /login

This is the only endpoint which doesn't require authentication, since it is the login endpoint to obtain authentication tokens from.

**Request Headers:**

- `Username` - The username of the account being logged into.
- `Password` - The password of the account being logged into.

**Response:**

HTTP 200 JSON body response with the token is returned on success, e.g. `{"token":"RCuRbzzSa51lNByCu+aeYXxoSeaO4HQgMJQ82gWqdSTPm7cHWCQxk7LoQEa8AIkiLBUQXCkkYF8gLHC3lOPfMVU4oU8rXGhQ1EB3VFP30VP2Dv7MG9clAsxuv2x+0jP5"}`

### GET /logout

**Response:**

HTTP 200 JSON body response `{"success":true}` is returned on success.

### GET /ott (one-time ticket)

**Response:**

HTTP 200 JSON body response with the ticket e.g. `{"ticket":"UTGA3Q=="}` is returned on success. This ticket is tied to your account, IP address, can be used for one request only, and will expire in 30 seconds.

### GET /servers

**Response:**

HTTP 200 JSON body response with the status of all servers e.g. `{"servers":{"app1":0, "app1": 1}}` is returned on success.

- `0` means the returned server is not running.
- `1` means the returned server is currently running.
- `2` means the returned server has crashed and is not currently running.

### GET /server/{id}

### POST /server/{id}

### WS /server/{id}/console?ticket=ticket

### GET /server/{id}/files?path=path

### GET /server/{id}/file?path=path&ticket=ticket

### POST /server/{id}/file?path=path

### POST /server/{id}/folder?path=path

### DELETE /server/{id}/file?path=path

### PATCH /server/{id}/file

### POST /server/{id}/compress?path=path&compress=boolean

### POST /server/{id}/decompress?path=path

## Note

This documentation is still being worked on. While all endpoints are listed, they are not yet fully documented e.g. basic information for many endpoints has not been filled, and possible errors are not yet documented.
