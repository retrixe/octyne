# octyne

A system to manage multiple different servers on the same system.

Octyne allows running multiple apps on a remote server and providing an HTTP API to manage them. This allows for hosting many web servers, game servers, bots and so on on remote servers without having to mess with SSH, using `screen` and `systemd` whenever you want to make any change, in a highly manageable and secure way.

It incorporates the ability to manage files and access the terminal output and input over HTTP remotely. For further security, it is recommended to use an HTTPS reverse proxy to ensure end-to-end secure transmission.

[retrixe/ecthelion](https://github.com/retrixe/ecthelion) complements octyne by providing a web interface to control servers on octyne remotely.

## Configuration

Octyne depends on two files in the same directory to get configuration from.

### config.json

Used to configure the servers Octyne should start along with Redis-based authentication for allowing more than a single node.

*NOTE: Remove the comments when creating the file as JSON does not support comments!*

```json
{
  "redis": {
    "enabled": false, // whether the authentication tokens should sync to Redis for more than 1 node
    "url": "redis://localhost" // link to Redis server
  },
  "servers": {
    "test1": { // each key has the name of the server
      "directory": "/home/test/server", // the directory in which the server is located
      "command": "java -jar spigot-1.12.2.jar" // the command to run to start the server
    }
  }
}
```

### users.json

Contains users who can log into Octyne. Use a secure method to hash your passwords as Octyne does not handle account management at the moment.

```json
{
  "username": "password hashed with SHA-256"
}
```
