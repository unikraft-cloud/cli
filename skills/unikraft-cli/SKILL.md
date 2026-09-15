---
name: unikraft-cli
description: Manage Unikraft Cloud resources including instances, images, volumes, and services. Use when the user needs to deploy applications, manage cloud infrastructure, or interact with Unikraft Cloud.
allowed-tools: Bash(unikraft*)
---

# Unikraft Cloud CLI

The `unikraft` command provides a unified interface for managing resources on Unikraft Cloud. It enables deploying instances, managing persistent storage, and configuring services.

## Installation

```bash
curl --proto '=https' --tlsv1.2 -fsSL https://unikraft.com/cli/install.sh | sh
```

## Quick Start

```bash
# Start by logging into Unikraft Cloud
unikraft login --no-browser

# Run a simple NGINX instance
unikraft run --metro=fra --scale-to-zero policy=on,stateful=true,cooldown-time=10 -p 8080:80 nginx:latest

# List running instances
unikraft instances list
```

## Core Resources

- **Instances**: MicroVMs on Unikraft Cloud run from `Dockerfile`'s.
- **Images**: Unikernel images stored in the registry.
- **Volumes**: Persistent storage that can be attached to instances.
- **Services**: Networking abstractions for exposing instances.
- **Certificates**: TLS certificates for secure connections.
- **Metros**: Geographic locations where resources are deployed.

## Commands

### Building (`unikraft build`)

The `build` command compiles a project from a Kraftfile into a unikernel image.
A directory without a Kraftfile is built from its `Dockerfile`, and `<path>`
can also point directly at a Kraftfile or a Dockerfile. Without a Kraftfile,
`--arch` is required.

```bash
unikraft build [flags] [<path>]
```

**Common Flags:**

- `-o, --output <dest>`: Output destination (registry tag or local OCI archive path).
- `--arch <arch>`: Architectures to build (`x86_64`, `arm64`). Required when no Kraftfile targets are declared.
- `--build-arg <key=val>`: Set build-time variables.
- `--no-cache`: Do not use cache when building the image.
- `--secret <spec>`: Secret to expose to the build (format: `id=mysecret[,src=/local/secret]`).
- `--ssh <spec>`: SSH agent socket or keys to expose to the build (format: `default|<id>[=<socket>|<key>[,<key>]]`).

### Deployment (`unikraft run`)

The `run` command is the primary entry point for deploying applications.

```bash
unikraft run [flags] <image> [<args>...]
```

**Common Flags:**

- `--metro <code`>: Metro to deploy in (e.g., `fra`, `dal`, `sin`).
- `-p, --publish <port>`: Publish a port (e.g., `443:8080/http+tls`).
- `-e, --env <key=val>`: Set environment variables.
- `-v, --volume <vol>`: Attach a volume.
- `-m, --memory <size>`: Set memory size (e.g., `512MiB`).
- `--scale-to-zero`: Enable scale-to-zero policies.
- `--dry-run`: Preview creation without deploying.

### Instance Management

```bash
unikraft instances list                 # List all instances
unikraft instances get <name|uuid>      # Inspect instance details
unikraft instances logs <name|uuid>     # View instance logs
unikraft instances stop <name|uuid>     # Stop an instance
unikraft instances start <name|uuid>    # Start a stopped instance
unikraft instances restart <name|uuid>  # Restart an instance
unikraft instances rm <name|uuid>       # Remove an instance
```

### Volume Management

```bash
unikraft volumes list                   # List volumes
unikraft volumes create <name> --size 1G # Create a volume
unikraft volumes clone <name|uuid>      # Clone a volume
unikraft volumes rm <name|uuid>         # Delete a volume
```

### Service Management

```bash
unikraft services list                  # List services
unikraft services create <name>         # Create a service (usually done via run)
unikraft services rm <name|uuid>        # Delete a service
```

### Image Management

```bash
unikraft images list                    # List images
unikraft images get <ref>               # Inspect image details
unikraft images copy <src> <dst>        # Copy an image
```

### Certificate Management

```bash
unikraft certificates list              # List certificates
unikraft certificates get <name|uuid>   # Inspect certificate details
unikraft certificates create            # Create a certificate
unikraft certificates rm <name|uuid>    # Delete a certificate
```

### Metro Management

```bash
unikraft metros list                    # List available metros
unikraft metros get <name>              # Inspect metro details
```

### Profile Management

```bash
unikraft profile list                   # List profiles
unikraft profile get <name>             # Inspect a profile
unikraft profile use <name>             # Switch the active profile
```

### Config

```bash
unikraft config get                     # Show current configuration
unikraft config get <path>              # Load a config file by path
```

### Authentication

```bash
unikraft login                          # interactive login
unikraft logout                         # logout
```

### Upgrade

```bash
unikraft upgrade                        # Upgrade to the latest stable release
unikraft upgrade --version v1.2.3       # Upgrade to a specific version
unikraft upgrade --channel staging      # Upgrade from the staging channel
```

## Global Options

| Option | Description |
|--------|-------------|
| `--metro <code`> | Target metro for the command. |
| `--config <file>` | Path to configuration file. |
| `--profile <name>` | Use a specific profile. |
| `--log-level <level>` | Set logging verbosity (info, debug, trace). |
| `--json` | Output (log-type) as JSON. |

## Examples

### Build and publish an image

```bash
unikraft build . --output my-org/my-app:latest
```

### Build with secrets and custom args

```bash
unikraft build ./app \
  --build-arg VERSION=1.2.3 \
  --secret id=npm,src=$HOME/.npmrc \
  --ssh default=$SSH_AUTH_SOCK
```

### Deploy with HTTPS and Redirect

```bash
unikraft run \
  --metro=fra \
  -p 443:8080/http+tls \
  -p 80:443/http+redirect \
  nginx:latest
```

### Deploy with Persistent Volume

```bash
# Attach existing volume
unikraft run \
  --metro=sin \
  -v my-data:/data \
  my-app:latest
```

### Auto-scaling deployment

```bash
unikraft run \
  --metro=fra \
  --scale-to-zero policy=on,cooldown-time=300 \
  my-server:latest
```

### Debugging

```bash
# Follow logs immediately after run
unikraft run --metro=fra --follow my-app:latest

# Get detailed info
unikraft instances inspect my-instance-name
```

## Troubleshooting

**Authentication Issues?**
Run `unikraft login` to refresh credentials.

**Deployment Failures?**
Use `--dry-run` to validate configuration.
Check `unikraft instances logs <id>` for application startup errors.

**Resource Not Found?**
Ensure you are targeting the correct metro with `--metro`. Resources are often metro-specific.

## Tasks (Developer)

For developers working on this CLI repository:

- `task cli`: Build the binary.
- `task run`: Run specific dev tasks (see Taskfile.yml).
- `task lint`: Run linters.
- `task test`: Run unit tests.
