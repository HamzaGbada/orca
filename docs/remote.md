# Remote Docker hosts

Orca can inspect a Docker daemon on another machine: `orca report`,
`volumes`, `graph` and every other command work the same way. One command
talks to one daemon, and everything stays **read-only**.

## Choosing the daemon

`--host` accepts a URL or a name from the configuration:

```sh
orca report --host ssh://admin@prod.example.com     # a URL
orca report --host prod                             # a name defined under hosts:
DOCKER_HOST=ssh://admin@prod.example.com orca report  # the environment, like the docker CLI
```

With no `--host`, Orca uses `DOCKER_HOST`, then the local socket
`unix:///var/run/docker.sock`.

| URL | Transport |
|---|---|
| `unix:///path/docker.sock` | local Unix socket |
| `ssh://[user@]host[:port]` | SSH tunnel (recommended for remote hosts) |
| `tcp://host:port` | TCP, with TLS certificates (`tls_cert_path` or `DOCKER_CERT_PATH`) |

## Named hosts

Define servers once in `~/.config/orca/config.yaml`:

```yaml
hosts:
  prod:
    url: ssh://admin@prod.example.com
  build:
    url: tcp://build.example.com:2376
    tls_cert_path: ~/.docker/build     # directory with ca.pem, cert.pem, key.pem
```

Then run `orca report --host prod`. An unknown name is an error that lists the
configured names. Orca never guesses.

## SSH (recommended)

Orca runs, like the docker CLI:

```text
ssh -o ConnectTimeout=30 -T [-l user] [-p port] -- host docker system dial-stdio
```

and speaks the Docker API over that connection. Nothing is exposed on the
network, and authentication is your normal SSH setup. It uses your
`~/.ssh/config` host aliases, keys, agent, jump hosts and known_hosts
checking, because it is your `ssh` binary.

Requirements:

1. `ssh` works non-interactively: `ssh admin@prod.example.com docker version`
   must succeed without typing a password (use a key or the agent).
2. On the remote machine, that user can use Docker (member of the `docker`
   group, or root) and the `docker` CLI is installed (18.09+).

If a connection is slow to set up (a jump host, first contact), raise the
timeout: `--timeout 30s`.

Passwords in the URL (`ssh://user:pass@host`) are rejected, and so are ssh://
URLs with a path.

## TCP with TLS

For daemons exposed with `dockerd --tlsverify`:

```yaml
hosts:
  build:
    url: tcp://build.example.com:2376
    tls_cert_path: ~/.docker/build
```

or the docker CLI's environment variables: `DOCKER_HOST=tcp://…:2376
DOCKER_TLS_VERIFY=1 DOCKER_CERT_PATH=~/.docker/build`.

> **Never expose a Docker daemon on plain `tcp://` without TLS.** Access to
> the Docker API is equivalent to root on that machine. Orca only reads, but
> anyone else who can reach the port can do anything. Prefer SSH.

## What changes for a remote daemon

Everything that comes from the Docker API works remotely: containers, images,
layers, volumes (with sizes), networks, build cache, the safety model, the
reclaim estimate and the graph. Measurements that read the daemon's
filesystem are **skipped**, with a warning, because that filesystem is on
another machine:

| Skipped remotely | Reason |
|---|---|
| Disk pressure | `statfs` of the remote data root isn't possible from here |
| Container log sizes | the log files live on the remote host |
| Volume inode counts | same |
| `orca storage` overlay2 measurement | same |

To get them, run Orca on the server itself (for example
`ssh prod 'sudo orca report'` after installing Orca there).

The report and graph show which daemon they describe (`ssh://…`), so saved
outputs stay attributable.
