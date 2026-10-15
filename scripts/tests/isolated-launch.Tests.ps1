#Requires -Version 5.1
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Assert-Launch {
  param([bool]$Condition, [string]$Message)
  if (-not $Condition) { throw $Message }
}

function Wait-Until {
  param([scriptblock]$Condition, [string]$Message, [int]$Seconds = 20)
  $deadline = [datetime]::UtcNow.AddSeconds($Seconds)
  do {
    if (& $Condition) { return }
    Start-Sleep -Milliseconds 100
  } while ([datetime]::UtcNow -lt $deadline)
  throw $Message
}

function Get-FreeTestPort {
  $socket = New-Object Net.Sockets.TcpListener([Net.IPAddress]::Loopback, 0)
  $socket.Start()
  try { return $socket.LocalEndpoint.Port } finally { $socket.Stop() }
}

function Quote-PowerShellLiteral {
  param([string]$Value)
  return "'" + $Value.Replace("'", "''") + "'"
}

function Start-TestLauncher {
  param([string]$Extra = '', [int]$Timeout = 4)
  $command = '$ErrorActionPreference = ''Stop''; & ' + (Quote-PowerShellLiteral $script:launcher) +
    ' -NoBuild -BackendPort ' + $script:backendPort + ' -WebPort ' + $script:webPort +
    ' -HomeDir ' + (Quote-PowerShellLiteral $script:homeDir) + ' -Timeout ' + $Timeout + ' ' + $Extra
  $encoded = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($command))
  $start = New-Object Diagnostics.ProcessStartInfo
  $start.FileName = Join-Path $PSHOME 'powershell.exe'
  $start.Arguments = '-NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand ' + $encoded
  $start.UseShellExecute = $false
  $start.CreateNoWindow = $true
  $start.RedirectStandardOutput = $true
  $start.RedirectStandardError = $true
  $start.EnvironmentVariables['PATH'] = $script:binDir + ';' + $env:PATH
  $start.EnvironmentVariables['KANDEV_DATABASE_DRIVER'] = 'must-not-inherit'
  $start.EnvironmentVariables['DOCKER_HOST'] = 'must-not-inherit'
  $start.EnvironmentVariables['VITE_SECRET_FOR_TEST'] = 'must-not-inherit'
  $process = [Diagnostics.Process]::Start($start)
  return [pscustomobject]@{
    Process = $process
    Output = $process.StandardOutput.ReadToEndAsync()
    Error = $process.StandardError.ReadToEndAsync()
  }
}

function Complete-TestLauncher {
  param($Run)
  Assert-Launch ($Run.Process.WaitForExit(45000)) 'Launcher exceeded its bounded startup budget.'
  Assert-Launch ($Run.Output.Wait(5000) -and $Run.Error.Wait(5000)) `
    'Caller pipes must reach EOF while the detached listeners remain alive.'
  $script:lastOutput = $Run.Output.Result + $Run.Error.Result
  Write-Output $script:lastOutput
  return $Run.Process.ExitCode
}

function Read-FixturePid {
  param([string]$Role)
  return [int](Get-Content -LiteralPath (Join-Path $script:homeDir ($Role + '.pid')) -Raw)
}

function Assert-FixtureGone {
  foreach ($file in @(Get-ChildItem -LiteralPath $script:homeDir -Filter '*.pid' -ErrorAction SilentlyContinue)) {
    $target = [int](Get-Content -LiteralPath $file.FullName -Raw)
    Wait-Until { -not (Get-Process -Id $target -ErrorAction SilentlyContinue) } `
      "Owned fixture $($file.Name) PID $target survived failed launch cleanup." -Seconds 8
  }
}

function Invoke-LaunchCase {
  param([string]$Name, [scriptblock]$Body)
  $script:homeDir = Join-Path $script:testRoot ([guid]::NewGuid().ToString('N'))
  New-Item -ItemType Directory -Path $script:homeDir | Out-Null
  $script:backendPort = Get-FreeTestPort
  do { $script:webPort = Get-FreeTestPort } while ($script:webPort -eq $script:backendPort)
  $script:pidfile = Join-Path $env:TEMP ("kandev-dev-isolated-$($script:backendPort).pid")
  $script:lastOutput = ''
  Set-Content -LiteralPath (Join-Path $script:binDir 'backend.mode') -Value 'healthy' -Encoding ASCII
  Set-Content -LiteralPath (Join-Path $script:binDir 'web.mode') -Value 'healthy' -Encoding ASCII
  try {
    & $Body
    Write-Output "PASS: $Name"
  } catch {
    $script:failures += $Name + ': ' + $_.Exception.Message
    Write-Output "FAIL: $Name : $($_.Exception.Message)"
    Write-Output $script:lastOutput
  } finally {
    # Only processes with fixture identities in this case's owned directory.
    foreach ($file in @(Get-ChildItem -LiteralPath $script:homeDir -Filter '*.pid' -ErrorAction SilentlyContinue)) {
      $target = [int](Get-Content -LiteralPath $file.FullName -Raw)
      $info = Get-CimInstance Win32_Process -Filter "ProcessId = $target" -ErrorAction SilentlyContinue
      if ($info -and $info.ExecutablePath -and $info.ExecutablePath.StartsWith($script:testRoot)) {
        & "$env:SystemRoot\System32\taskkill.exe" /PID $target /T /F 2>&1 | Out-Null
      }
    }
    foreach ($suffix in @('.pid', '.backend.started', '.home', '.web.pid', '.web.started')) {
      $metadata = $script:pidfile -replace '\.pid$', $suffix
      Remove-Item -LiteralPath $metadata -Force -Recurse -ErrorAction SilentlyContinue
    }
  }
}

$scriptsDir = Split-Path -Parent $PSScriptRoot
$script:testRoot = Join-Path $env:TEMP ('isolated launch %TEMP% ! & ^ (x) [literal] ' + [guid]::NewGuid().ToString('N'))
$script:binDir = Join-Path $script:testRoot 'repo\apps\backend\bin'
$webDir = Join-Path $script:testRoot 'repo\apps\web\node_modules\vite\bin'
$copyScripts = Join-Path $script:testRoot 'repo\scripts'
$script:launcher = Join-Path $copyScripts 'dev-isolated.ps1'
$script:failures = @()
New-Item -ItemType Directory -Path $script:binDir, $webDir, $copyScripts, (Join-Path $script:testRoot 'repo\apps\node_modules') -Force | Out-Null
try {
  Get-ChildItem -LiteralPath $scriptsDir -File | Where-Object { $_.Extension -in @('.ps1', '.psm1', '.cs') } |
    Copy-Item -Destination $copyScripts
  New-Item -ItemType File -Path (Join-Path $webDir 'vite.js') | Out-Null
  $fixtureExe = Join-Path $script:binDir 'kandev.exe'
  Add-Type -TypeDefinition (Get-Content -LiteralPath (Join-Path $PSScriptRoot 'isolated-launch-fixture.cs') -Raw) `
    -OutputAssembly $fixtureExe -OutputType ConsoleApplication
  foreach ($name in @('node.exe', 'agentctl.exe', 'mock-agent.exe')) {
    Copy-Item -LiteralPath $fixtureExe -Destination (Join-Path $script:binDir $name)
  }

  Invoke-LaunchCase 'real backend and Vite listeners outlive caller pipe EOF; literal paths and isolated environment' {
    $run = Start-TestLauncher -Extra '-Web'
    $exitCode = @(Complete-TestLauncher $run)[-1]
    Assert-Launch ($exitCode -eq 0) 'Healthy backend and Vite launch must succeed.'
    foreach ($role in @('backend', 'web')) {
      $target = Read-FixturePid $role
      $port = if ($role -eq 'backend') { $script:backendPort } else { $script:webPort }
      $record = if ($role -eq 'backend') { $script:pidfile } else { $script:pidfile -replace '\.pid$', '.web.pid' }
      Assert-Launch ([int](Get-Content -LiteralPath $record -Raw) -eq $target) "$role metadata must record the actual fixture listener."
      Assert-Launch (@(Get-NetTCPConnection -State Listen -LocalPort $port).OwningProcess -contains $target) "$role must actually listen."
      Assert-Launch ((Get-Content -LiteralPath (Join-Path $script:homeDir ($role + '.environment')) -Raw) -eq '||') `
        "$role inherited a forbidden environment variable."
    }
    & (Join-Path $copyScripts 'kandev-kill.ps1') -Pidfile $script:pidfile -Yes
    Assert-FixtureGone
  }

  Invoke-LaunchCase 'a concurrently started compatible unrelated listener is never adopted or stopped' {
    Set-Content -LiteralPath (Join-Path $script:binDir 'backend.mode') -Value 'delay' -Encoding ASCII
    $run = Start-TestLauncher
    Wait-Until { Test-Path -LiteralPath (Join-Path $script:homeDir 'backend.pid') } 'Owned backend never reached the launch barrier.'
    $competitorHome = Join-Path $script:homeDir 'competitor'
    New-Item -ItemType Directory -Path $competitorHome | Out-Null
    Set-Content -LiteralPath (Join-Path $competitorHome 'release') -Value 'go'
    $start = New-Object Diagnostics.ProcessStartInfo
    $start.FileName = $fixtureExe
    $start.Arguments = '__backend'
    $start.UseShellExecute = $false
    $start.CreateNoWindow = $true
    $start.EnvironmentVariables['KANDEV_HOME_DIR'] = $competitorHome
    $start.EnvironmentVariables['KANDEV_SERVER_PORT'] = [string]$script:backendPort
    $competitor = [Diagnostics.Process]::Start($start)
    try {
      Wait-Until { @(Get-NetTCPConnection -State Listen -LocalPort $script:backendPort -ErrorAction SilentlyContinue | ForEach-Object { $_.OwningProcess }) -contains $competitor.Id } `
        'Concurrent compatible listener failed to bind.'
      Set-Content -LiteralPath (Join-Path $script:homeDir 'release') -Value 'go'
      $exitCode = @(Complete-TestLauncher $run)[-1]
      Assert-Launch ($exitCode -ne 0) 'Launcher adopted the concurrent unrelated backend listener.'
      Assert-Launch (-not $competitor.HasExited) 'Failure cleanup stopped the unrelated listener.'
      Assert-Launch (-not (Test-Path -LiteralPath $script:pidfile)) 'Failed launch left adopted metadata.'
      Assert-FixtureGone
    } finally {
      if (-not $competitor.HasExited) { $competitor.Kill(); $competitor.WaitForExit() }
      $competitor.Dispose()
    }
  }

  Invoke-LaunchCase 'health timeout cleans the full owned descendant tree' {
    Set-Content -LiteralPath (Join-Path $script:binDir 'backend.mode') -Value 'unhealthy tree' -Encoding ASCII
    $run = Start-TestLauncher
    $exitCode = @(Complete-TestLauncher $run)[-1]
    Assert-Launch ($exitCode -ne 0) 'Unhealthy backend must fail startup.'
    Assert-Launch (Test-Path -LiteralPath (Join-Path $script:homeDir 'descendant-grandchild.pid')) 'Grandchild positive control did not start.'
    Assert-FixtureGone
  }

  Invoke-LaunchCase 'discovery timeout cleans the full owned descendant tree' {
    Set-Content -LiteralPath (Join-Path $script:binDir 'backend.mode') -Value 'no-listener tree' -Encoding ASCII
    $run = Start-TestLauncher
    $exitCode = @(Complete-TestLauncher $run)[-1]
    Assert-Launch ($exitCode -ne 0) 'Backend without a listener must fail startup.'
    Assert-Launch (Test-Path -LiteralPath (Join-Path $script:homeDir 'descendant-grandchild.pid')) 'Grandchild positive control did not start.'
    Assert-FixtureGone
  }

  Invoke-LaunchCase 'backend metadata write failure cleans launched processes' {
    New-Item -ItemType Directory -Path ($script:pidfile -replace '\.pid$', '.backend.started') | Out-Null
    $run = Start-TestLauncher
    $exitCode = @(Complete-TestLauncher $run)[-1]
    Assert-Launch ($exitCode -ne 0) 'Injected metadata write failure must fail startup.'
    Assert-Launch (Test-Path -LiteralPath (Join-Path $script:homeDir 'backend.pid')) 'Backend positive control did not start.'
    Assert-FixtureGone
  }

  Invoke-LaunchCase 'WebHost shell metacharacters are refused before launching' {
    $marker = Join-Path $script:homeDir 'injected.txt'
    $hostArgument = '127.0.0.1 & echo INJECTED>"' + $marker + '" & rem '
    $run = Start-TestLauncher -Extra ('-Web -WebHost ' + (Quote-PowerShellLiteral $hostArgument))
    $exitCode = @(Complete-TestLauncher $run)[-1]
    Assert-Launch ($exitCode -ne 0) 'Shell-source host argument must be rejected.'
    Assert-Launch (-not (Test-Path -LiteralPath $marker)) 'WebHost was evaluated as shell source.'
    Assert-Launch (-not (Test-Path -LiteralPath (Join-Path $script:homeDir 'backend.pid'))) 'Invalid host must fail before backend launch.'
  }
} finally {
  Remove-Item -LiteralPath $script:testRoot -Recurse -Force -ErrorAction SilentlyContinue
}
if ($script:failures.Count) { throw ($script:failures -join [Environment]::NewLine) }
Write-Output 'Windows isolated launch regressions passed.'
