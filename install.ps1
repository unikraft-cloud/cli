# Install the Unikraft CLI on Windows.
#
# Usage:
#   irm https://unikraft.com/cli/install.ps1 | iex
#   & ([scriptblock]::Create((irm https://unikraft.com/cli/install.ps1))) -Channel staging
#
# Influential environment variables:
#   UNIKRAFT_CLI_INSTALL_URL       Override base download URL
#   UNIKRAFT_CLI_INSTALL_CHANNEL   Override default channel
#   UNIKRAFT_CLI_INSTALL_VERSION   Override version
#   UNIKRAFT_CLI_INSTALL_BIN_DIR   Override install directory

[CmdletBinding()]
param(
    # Release channel (stable or staging). Ignored if -Version is set.
    [string]$Channel = $(if ($env:UNIKRAFT_CLI_INSTALL_CHANNEL) { $env:UNIKRAFT_CLI_INSTALL_CHANNEL } else { 'stable' }),
    # A specific version tag to install (e.g., v1.2.3).
    [string]$Version = $env:UNIKRAFT_CLI_INSTALL_VERSION,
    # Directory to install unikraft.exe into.
    [string]$BinDir = $env:UNIKRAFT_CLI_INSTALL_BIN_DIR
)

# Run in a child scope, so that `irm | iex` keeps the preferences and
# variables of the script out of the session.
& {
    $ErrorActionPreference = 'Stop'
    # Windows PowerShell 5.1 downloads slowly while it draws progress.
    $ProgressPreference = 'SilentlyContinue'

    # Constrained language mode blocks the .NET calls below.
    $mode = $ExecutionContext.SessionState.LanguageMode
    if ($mode -ne 'FullLanguage') {
        throw "PowerShell runs in $mode mode, which blocks this script. Download the Windows zip from https://github.com/unikraft-cloud/cli/releases instead."
    }

    # The system default (0) has TLS 1.2 and later, so only add TLS 1.2 to an older setting.
    if ([Net.ServicePointManager]::SecurityProtocol -ne 0) {
        [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
    }
    # Windows PowerShell 5.1 does not send the user's credentials to the system proxy.
    if ([Net.WebRequest]::DefaultWebProxy) {
        [Net.WebRequest]::DefaultWebProxy.Credentials = [Net.CredentialCache]::DefaultCredentials
    }

    $BaseUrl = if ($env:UNIKRAFT_CLI_INSTALL_URL) { $env:UNIKRAFT_CLI_INSTALL_URL } else { 'https://pkg.unikraft.com' }

    function Get-TextFile([string]$Url, [string]$Path) {
        Invoke-WebRequest -UseBasicParsing -Uri $Url -OutFile $Path
        return "$(Get-Content -Raw -Path $Path)".Trim()
    }

    # The registry has the real architecture, also for a 32-bit or an emulated x64 shell.
    $arch = (Get-ItemProperty -Path 'HKLM:\SYSTEM\CurrentControlSet\Control\Session Manager\Environment' -ErrorAction SilentlyContinue).PROCESSOR_ARCHITECTURE
    if (-not $arch) {
        $arch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    }
    switch ($arch) {
        'AMD64' { $arch = 'amd64' }
        'ARM64' { $arch = 'arm64' }
        default { throw "Unsupported architecture: $arch" }
    }

    if (-not $BinDir) {
        $BinDir = Join-Path $env:LOCALAPPDATA 'Programs\Unikraft\bin'
    }
    # PATH needs a full path, but PowerShell also takes a relative path or ~.
    $BinDir = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($BinDir)

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ([IO.Path]::GetRandomFileName())
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        if (-not $Version) {
            Write-Host 'Fetching latest version'
            $Version = Get-TextFile "$BaseUrl/endpoints/cli/content/$Channel.txt" (Join-Path $tmp 'channel.txt')
            if (-not $Version) {
                throw "The $Channel channel at $BaseUrl has no version."
            }
        }

        $asset = "unikraft-windows-$arch.zip"
        $url = "$BaseUrl/endpoints/cli/content/$Version/$asset"
        $archive = Join-Path $tmp $asset

        Write-Host "Downloading Unikraft CLI $Version ($arch)"
        try {
            Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $archive
        } catch {
            if ($_.Exception.Response -and [int]$_.Exception.Response.StatusCode -eq 404) {
                throw "Unikraft CLI $Version has no Windows $arch build at $url"
            }
            throw
        }

        $expected = $null
        try {
            $expected = ((Get-TextFile "$url.sha256" (Join-Path $tmp 'archive.sha256')) -split '\s+')[0]
        } catch {
            # An HTTP error skips the check, as in install.sh, but a failed connection does not.
            if (-not $_.Exception.Response) {
                throw
            }
            Write-Warning 'Checksum file not available, skipping verification'
        }
        if ($null -ne $expected) {
            if ($expected -notmatch '^[0-9a-fA-F]{64}$') {
                throw "The checksum file at $url.sha256 is not valid."
            }
            # Get-FileHash and Expand-Archive are script modules, which the
            # default execution policy blocks, so use .NET directly.
            $sha256 = [Security.Cryptography.SHA256]::Create()
            $stream = [IO.File]::OpenRead($archive)
            try {
                $actual = [BitConverter]::ToString($sha256.ComputeHash($stream)) -replace '-'
            } finally {
                $stream.Dispose()
                $sha256.Dispose()
            }
            if ($actual -ne $expected) {
                throw "Checksum mismatch: expected $expected, got $actual. The download may be corrupted."
            }
            Write-Host 'Verified checksum'
        }

        Add-Type -AssemblyName System.IO.Compression.FileSystem
        [IO.Compression.ZipFile]::ExtractToDirectory($archive, (Join-Path $tmp 'out'))
        New-Item -ItemType Directory -Force -Path $BinDir | Out-Null

        # Windows cannot write over a running binary, but it can rename it.
        # unikraft removes the backup when it starts or upgrades.
        $exe = Join-Path $BinDir 'unikraft.exe'
        $backup = $null
        if (Test-Path -LiteralPath $exe) {
            Get-ChildItem -LiteralPath $BinDir -Filter '.unikraft.exe.old*' -Force |
                Remove-Item -Force -ErrorAction SilentlyContinue
            $backup = Join-Path $BinDir '.unikraft.exe.old'
            if (Test-Path -LiteralPath $backup) {
                $backup += '.' + [IO.Path]::GetRandomFileName()
            }
            Move-Item -Force -LiteralPath $exe -Destination $backup
        }
        try {
            Copy-Item -Force -LiteralPath (Join-Path $tmp 'out\unikraft.exe') -Destination $exe
        } catch {
            if ($backup) {
                Move-Item -Force -LiteralPath $backup -Destination $exe
            }
            throw
        }
        if ($backup) {
            Remove-Item -Force -LiteralPath $backup -ErrorAction SilentlyContinue
        }
        Write-Host "Installed to $BinDir"
    } finally {
        Remove-Item -Recurse -Force -Path $tmp -ErrorAction SilentlyContinue
    }

    # Use the registry directly, because [Environment] expands the %VAR% entries in the user PATH.
    $envKey = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey('Environment')
    try {
        $userPath = $envKey.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
        $entries = $userPath -split ';' | ForEach-Object { [Environment]::ExpandEnvironmentVariables($_).TrimEnd('\') }
        if ($entries -notcontains $BinDir.TrimEnd('\')) {
            $newPath = if ($userPath) { "$($userPath.TrimEnd(';'));$BinDir" } else { $BinDir }
            $envKey.SetValue('Path', $newPath, [Microsoft.Win32.RegistryValueKind]::ExpandString)

            # .NET sends WM_SETTINGCHANGE when it removes a user variable, so
            # that Explorer gives the new PATH to new terminals.
            [Environment]::SetEnvironmentVariable('UNIKRAFT_CLI_INSTALL_NOTIFY', $null, 'User')
            Write-Host "Added $BinDir to your user PATH; new terminals pick it up."
        }
    } finally {
        $envKey.Dispose()
    }
    if (($env:Path -split ';' | ForEach-Object { $_.TrimEnd('\') }) -notcontains $BinDir.TrimEnd('\')) {
        $env:Path = "$env:Path;$BinDir"
    }

    Write-Host ''
    Write-Host 'Run unikraft login to get started.'
}
