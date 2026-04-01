param(
    [Parameter(Position=0)]
    [string]$Target = "build",
    [string]$Config = "devmux.yaml"
)

$BinDir = "bin"
$DaemonBin = "devmuxd.exe"
$CliBin = "devmux.exe"
$ThirdParty = "third_party"
$GhosttySrc = "$ThirdParty/ghostty-src"
$GhosttyOut = "$ThirdParty/ghostty"
$GhosttyRepo = "https://github.com/ghostty-org/ghostty.git"
$GhosttyTag = "bebca84668947bfc92b9a30ed58712e1c34eee1d"

function Ensure-BinDir {
    if (-not (Test-Path $BinDir)) {
        New-Item -ItemType Directory -Path $BinDir | Out-Null
    }
}

function Ghostty-Fetch {
    if (-not (Test-Path $ThirdParty)) {
        New-Item -ItemType Directory -Path $ThirdParty | Out-Null
    }
    if (-not (Test-Path $GhosttySrc)) {
        Write-Host "Cloning ghostty repository..."
        git clone --depth 1 $GhosttyRepo $GhosttySrc
        Push-Location $GhosttySrc
        git fetch --depth 1 origin $GhosttyTag
        git checkout $GhosttyTag
        Pop-Location
    }
}

function Ghostty-Build {
    if ((Test-Path "$GhosttyOut/lib/ghostty-vt.lib") -or (Test-Path "$GhosttyOut/lib/libghostty-vt.so")) {
        Write-Host "libghostty-vt already built"
        return
    }
    if (-not (Get-Command zig -ErrorAction SilentlyContinue)) {
        Write-Error "Zig compiler not found. Install Zig 0.15.x from https://ziglang.org/download/"
        return
    }
    Ghostty-Fetch
    if (-not (Test-Path "$GhosttyOut/lib")) { New-Item -ItemType Directory -Path "$GhosttyOut/lib" -Force | Out-Null }
    if (-not (Test-Path "$GhosttyOut/include")) { New-Item -ItemType Directory -Path "$GhosttyOut/include" -Force | Out-Null }
    
    Write-Host "Building libghostty-vt..."
    Push-Location $GhosttySrc
    zig build -Demit-lib-vt=true -Doptimize=ReleaseFast
    Pop-Location
    
    Write-Host "Copying library files..."
    Copy-Item "$GhosttySrc/zig-out/lib/*.lib" "$GhosttyOut/lib/" -ErrorAction SilentlyContinue
    Copy-Item "$GhosttySrc/zig-out/bin/*.dll" "$GhosttyOut/lib/" -ErrorAction SilentlyContinue
    Copy-Item "$GhosttySrc/zig-out/bin/*.dll" "$BinDir/" -ErrorAction SilentlyContinue
    if (Test-Path "$GhosttySrc/zig-out/include") {
        if (Test-Path "$GhosttyOut/include/ghostty") { Remove-Item -Recurse -Force "$GhosttyOut/include/ghostty" }
        Copy-Item -Recurse "$GhosttySrc/zig-out/include/ghostty" "$GhosttyOut/include/"
    }
}

function Build-Daemon {
    Ghostty-Build
    Write-Host "Building daemon with libghostty..."
    Ensure-BinDir
    $env:CGO_ENABLED = "1"
    $env:CC = "zig cc"
    go build -tags ghostty -ldflags="-s -w" -o "$BinDir/$DaemonBin" ./cmd/devmuxd
}

function Build-Cli {
    Write-Host "Building CLI (pure Go)..."
    Ensure-BinDir
    $env:CGO_ENABLED = "0"
    go build -ldflags="-s -w" -o "$BinDir/$CliBin" ./cmd/devmux
}

function Build-TestApps {
    Write-Host "Building test apps..."
    $apps = "http-app", "log-app", "tcp-app"
    foreach ($appName in $apps) {
        Write-Host "  Building $appName..."
        go build -o "bin/$appName.exe" "./test-apps/$appName"
    }
}

function Clean {
    if (Test-Path $BinDir) { Remove-Item -Recurse -Force $BinDir }
    go clean
}

switch ($Target) {
    "build" { Build-Daemon; Build-Cli; Build-TestApps }
    "ghostty-build" { Ghostty-Build }
    "run" { Build-Daemon; Build-Cli; Build-TestApps; & "$BinDir/$CliBin" start $Config }
    "ui" { Build-Cli; & "$BinDir/$CliBin" ui }
    "stop" { & "$BinDir/$CliBin" stop }
    "clean" { Clean }
    "test" { go test -v ./... }
    default { Write-Host "Unknown target: $Target" }
}
