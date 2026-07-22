`socks` outbound is a socks4/socks4a/socks5 client.

### Structure

```json
{
  "type": "socks",
  "tag": "socks-out",
  
  "server": "127.0.0.1",
  "server_port": 1080,
  "version": "5",
  "username": "sekai",
  "password": "admin",
  "private_auth_method": "0x80",
  "network": "udp",
  "udp_over_tcp": false | {},

  ... // Dial Fields
}
```

### Fields

#### server

==Required==

The server address.

#### server_port

==Required==

The server port.

#### version

The SOCKS version, one of `4` `4a` `5`.

SOCKS5 used by default.

#### username

SOCKS username.

#### password

SOCKS5 password.

#### private_auth_method

Enables the private SOCKS5 authentication and one-way XOR variant when set to `0x80` or `0x82`.

This option requires SOCKS5, a username of exactly 19 bytes, and a non-empty password. All data sent from the client to the server is XORed with `0xFF`; server responses are not transformed.

Leave this field empty for standard SOCKS behavior.

#### network

Enabled network

One of `tcp` `udp`.

Both is enabled by default.

#### udp_over_tcp

UDP over TCP protocol settings.

See [UDP Over TCP](/configuration/shared/udp-over-tcp/) for details.

### Dial Fields

See [Dial Fields](/configuration/shared/dial/) for details.
