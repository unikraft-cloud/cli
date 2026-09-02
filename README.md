# Unikraft CLI

> [!NOTE]
>
> This is the new **Unikraft CLI**. It is actively worked upon to deliver the latest **Unikraft Cloud** features to your CLI. [Feedback appreciated](https://unikraft.link/devsurvey)!

The official command-line interface for [Unikraft Cloud](https://unikraft.cloud) — deploy and manage unikernels globally in milliseconds.

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for build instructions, testing, and
release process.

## Features

- **Deploy Instantly** — Run unikernel images as serverless instances with `unikraft run`
- **Global Infrastructure** — Deploy across multiple metros with automatic multi-region support
- **Resource Management** — Full CRUD operations for instances, volumes, services, certificates, and images
- **Scale-to-Zero** — Built-in support for scale-to-zero policies to optimize costs
- **Multiple Output Formats** — Machine-readable JSON or human-friendly text output
- **Profile Management** — Switch between multiple accounts and configurations
- **Shell Completions** — Tab completion for Bash, Zsh, Fish, and PowerShell

## Installation

<details open>
<summary><strong>1-liner (macOS & Linux)</strong></summary>

```bash
curl --proto '=https' --tlsv1.2 -fsSL https://unikraft.com/cli/install.sh | sh
```

Installs into the first preferred `bin` directory found in your `PATH` (falling back to `$HOME/.local/bin` if none are available).
Use environment variable `UNIKRAFT_CLI_INSTALL_BIN_DIR` to customize the installation directory.

</details>

<details>
<summary><strong>Homebrew (macOS & Linux)</strong></summary>

```bash
brew install unikraft/tap/unikraft
```

</details>

<details>
<summary><strong>Debian/Ubuntu</strong></summary>

```bash
# Update and install dependencies
sudo apt update
sudo apt install ca-certificates curl

# Download and add the GPG key
sudo install -d -m 0755 /etc/apt/keyrings

sudo curl -fsSL \
  -o /etc/apt/keyrings/unikraft-cli.gpg \
  https://pkg.unikraft.com/debian/cli-apt/keys/cli-apt.gpg

sudo tee /etc/apt/sources.list.d/unikraft-cli.sources <<EOF
Types: deb
URIs: https://pkg.unikraft.com/debian/cli-apt/
Suites: $(. /etc/os-release && echo "$VERSION_CODENAME")
Components: stable
Signed-By: /etc/apt/keyrings/unikraft-cli.gpg
EOF

# Update and install the CLI
sudo apt update
sudo apt install unikraft-cli
```

</details>

<details>
<summary><strong>Fedora/RHEL/Rocky/Alma</strong></summary>

```sh
# Add the Unikraft CLI repository
sudo tee /etc/yum.repos.d/unikraft-cli-rpm.repo <<EOF
[unikraft]
name=unikraft
baseurl=https://pkg.unikraft.com/rpm/cli-rpm/
gpgcheck=0
enabled=1
EOF

# Install the CLI at the specific pre-release version
yum install unikraft-cli
```

</details>

<details>
<summary><strong>Nix (flake)</strong></summary>

The CLI is packaged in the [Unikraft NUR](https://github.com/unikraft/nur)
(Nix User Repository), so there are no release hashes to vendor by hand — each
release is published there automatically. Supported systems:
`x86_64-linux`, `aarch64-linux`, `x86_64-darwin`, and `aarch64-darwin`.

The repository is consumed directly as a flake input or overlay, as shown
below. It is not registered in the
[nix-community NUR index](https://github.com/nix-community/NUR), so
`nur.repos.unikraft.*` does not resolve.

Run it once without installing:

```sh
nix run github:unikraft/nur#unikraft-cli -- version
```

Install it into your user profile (upgrade later with
`nix profile upgrade unikraft-cli`):

```sh
nix profile add github:unikraft/nur#unikraft-cli
```

On Nix older than 2.30 the subcommand is `nix profile install`, which newer
versions still accept as a deprecated alias for `add`.

Add it to a flake dev shell as an input:

```nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    unikraft-nur.url = "github:unikraft/nur";
    # Optional: reuse the nixpkgs above instead of evaluating a second one.
    unikraft-nur.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs = { self, nixpkgs, flake-utils, unikraft-nur }:
    flake-utils.lib.eachDefaultSystem (system:
      let pkgs = import nixpkgs { inherit system; };
      in {
        devShells.default = pkgs.mkShell {
          packages = [ unikraft-nur.packages.${system}.unikraft-cli ];
        };
      });
}
```

Or apply the overlay to expose the packages on `pkgs` directly — replace the
`let ... in` body of the example above with:

```nix
let
  pkgs = import nixpkgs {
    inherit system;
    overlays = [ unikraft-nur.overlays.default ];
  };
in {
  devShells.default = pkgs.mkShell {
    packages = [ pkgs.unikraft-cli ];
  };
}
```

The NUR exposes the following package attributes:

| Attribute              | Description                                                 |
| ---------------------- | ----------------------------------------------------------- |
| `unikraft-cli`         | Latest **stable** release of the Unikraft CLI (recommended) |
| `unikraft-cli-staging` | Latest **pre-release** (`-staging`) build                   |
| `kraftkit`             | Legacy KraftKit, installed as `kraft`                       |

</details>

<details>
<summary><strong>GitHub Actions</strong></summary>

- Requires [GitHub Actions](https://docs.github.com/en/actions).

Use the official [`unikraft/setup-action`](https://github.com/unikraft/setup-action)
to install the CLI in your workflow:

```yaml
- name: Install Unikraft CLI
  uses: unikraft/setup-action@v1
```

</details>

<details>
<summary><strong>From Source</strong></summary>

- Requires [Go](https://golang.org/dl/);
- Git;
- GNU Make or [Task](https://taskfile.dev/).

```sh
# Clone the repository
git clone https://github.com/unikraft-cloud/cli.git
cd cli

# Build the CLI
make cli

# The binary is available at dist/unikraft
./dist/unikraft --version
```

</details>

### `unikraft build` dependencies

To use `unikraft build` to build and publish your own images from
`Dockerfile`s, you need a BuildKit builder. The easiest way to get one is via
Docker, see <https://docs.docker.com/engine/install/> for installation
instructions.

Alternatively, you can also directly setup and use BuildKit, see <https://github.com/moby/buildkit#quick-start>.

## Quick Start

### 1. Login to [Unikraft Cloud](https://console.unikraft.cloud/auth/signin)

```sh
unikraft login
```

This opens your browser for authentication. Alternatively, provide a token file:

```sh
unikraft login --token /path/to/token
```

Or, if you need to directly specify a metro endpoint, you can manually create a
profile:

```yaml
# Linux: ~/.config/unikraft/config.yaml
# MacOS: ~/Library/Application\ Support/unikraft/config.yaml
profile: default
profiles:
  default:
    type: cloud
    organization: <org-name>
    token: <api-token>
    metros:
      - name: <metro-name> # e.g. fra
        endpoint: <metro-endpoint> # e.g. https://api.fra.unikraft.cloud
        country: <metro-country> # e.g. de
        insecure: false # skip tls verification (avoid for production use)
```

You can also use your old, already set, `UKC_METRO`/`UKC_TOKEN` by setting also this variable:

```sh
export UNIKRAFT_PROFILE_ENV="true"
```

### 2. Deploy Your First Instance

```sh
unikraft run --metro=fra -p 443:8080/http+tls --image=nginx:latest
```

This deploys an NGINX instance in Frankfurt with HTTPS enabled.

### 3. List Your Instances

```sh
unikraft instances list
```

### 4. View Instance Logs

```sh
unikraft instances logs my-instance
```

## Commands

| Command        | Description                                                                    |
| -------------- | ------------------------------------------------------------------------------ |
| `run`          | Run an image as an instance                                                    |
| `build`        | Build a Unikraft project into a VM image                                       |
| `tui`          | Browse resources in a TUI                                                      |
| `metros`       | List available metro locations                                                 |
| `instances`    | Manage instances (list, get, create, edit, delete, logs, start, stop, restart) |
| `volumes`      | Manage persistent volumes (list, get, create, edit, delete, clone)             |
| `services`     | Manage service groups (list, get, create, edit, delete)                        |
| `certificates` | Manage TLS certificates (list, get, create, delete)                            |
| `images`       | Manage images (list, get, copy)                                                |
| `login`        | Login to Unikraft Cloud                                                        |
| `logout`       | Logout from Unikraft Cloud                                                     |
| `profile`      | Manage profiles (list, get, use)                                               |
| `config`       | Manage CLI configuration                                                       |
| `completion`   | Generate shell completion scripts                                              |
| `version`      | Show version information                                                       |
| `upgrade`      | Upgrade the Unikraft CLI to the latest version                                 |

### Examples

**Deploy with environment variables:**

```sh
unikraft run --metro=sfo -e KEY=VALUE -e DEBUG=true --image=my-app:latest
```

**Deploy with an attached volume:**

```sh
unikraft run --metro=was -v my-volume:/data --image=my-app:latest
```

**Deploy with custom resources:**

```sh
unikraft run --metro=dal -m 512MiB --vcpus 2 --image=my-app:latest
```

**Deploy with scale-to-zero:**

```sh
unikraft run --metro=sin --scale-to-zero policy=on,cooldown-time=300 --image=my-app:latest
```

**Create an instance with multiple service ports:**

```sh
unikraft run --metro=fra -p 443:8080/http+tls -p 80:443/http+redirect --image=nginx:latest
```

**Edit an existing instance:**

```sh
unikraft instances edit my-instance --set image=nginx:1.27
```

**Clone a volume:**

```sh
unikraft volumes clone my-volume --set name=my-volume-backup
```

**Build and publish an image from a Kraftfile:**

```sh
unikraft build . --output my-org/my-app:latest
```

## Configuration

The CLI stores configuration in `~/.config/unikraft/config.yaml` (or the path specified by `UNIKRAFT_CONFIG`).

### Environment Variables

| Variable                | Description                                                                                          |
| ----------------------- | ---------------------------------------------------------------------------------------------------- |
| `UNIKRAFT_CONFIG`       | Path to the configuration file                                                                       |
| `UNIKRAFT_PROFILE`      | Override the current profile                                                                         |
| `UNIKRAFT_PROFILE_ENV`  | Use a virtual profile from `UKC_*` env vars instead of config (`true`, `false`)                      |
| `UNIKRAFT_LOG_LEVEL`    | Set log level (`trace`, `debug`, `info`, `warn`, `error`, `fatal`)                                   |
| `UNIKRAFT_LOG_TYPE`     | Set output format (`text`, `json`)                                                                   |
| `UNIKRAFT_TELEMETRY`    | Enable/disable usage analytics (`true`, `false`)                                                     |
| `UKC_TOKEN`             | API token for legacy profiles / `UNIKRAFT_PROFILE_ENV`                                               |
| `UKC_METRO`             | Metro name (e.g. `fra`) or full endpoint URL for legacy profiles / `UNIKRAFT_PROFILE_ENV`            |
| `UKC_ALLOW_INSECURE`    | Skip TLS verification for the metro from `UKC_METRO` (`true`, `false`)                               |

### Profiles

Manage multiple accounts or configurations with profiles:

```sh
# List profiles
unikraft profile list

# Switch profile
unikraft profile use staging

# Use a profile for a single command
unikraft --profile=staging instances list
```

## Output Formats

The CLI supports multiple output formats via the `-o` flag:

```sh
# Default table output
unikraft instances list

# JSON output
unikraft instances list -o json

# Show specific fields
unikraft instances get my-instance --fields name,state,image

# Include verbose/detailed fields
unikraft instances get my-instance -v
```

## Shell Completion

Generate completion scripts for your shell:

```sh
# Bash
unikraft completion -c bash > /etc/bash_completion.d/unikraft

# Zsh
unikraft completion -c zsh > "${fpath[1]}/_unikraft"

# Fish
unikraft completion -c fish > ~/.config/fish/completions/unikraft.fish

# PowerShell
unikraft completion -c powershell > unikraft.ps1
```

## Telemetry

The CLI collects usage analytics to improve the product.
When you are logged in, events are keyed to the user and organization UUIDs of the active profile.
Otherwise, events use an anonymous machine hash.
To opt out:

```sh
# Disable for a single command
unikraft --no-telemetry instances list

# Disable permanently
export UNIKRAFT_TELEMETRY=false

# Or
export DO_NOT_TRACK=1
```

## License

BSD-3-Clause. See [LICENSE.md](LICENSE.md) for details.
Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.

## Links

- [Unikraft Cloud Documentation](https://unikraft.com/docs)
- [Report Issues](https://github.com/unikraft-cloud/cli/issues)
- [Unikraft Website](https://unikraft.com)
