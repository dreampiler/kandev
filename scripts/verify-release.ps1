#Requires -Version 5.1
<#
.SYNOPSIS
  Fork release confirmation for restart-resume: build a revision, start an
  isolated instance, run the release scenarios, check the web assets, and emit
  a machine-readable result.

.DESCRIPTION
  This is the approved fork release-confirmation entry point. It is not a
  general-purpose gate for ordinary tasks. It never touches the operating
  instance, home, or installation, and it calls no real provider: it runs the
  mock agent in an isolated instance only.

  Covered scope (v09 section 2.2):
    A  new run emits an execution and its first event and completes one turn.
    B  a completed-turn session continues on the same task session after a
       backend restart.
    C  an idle waiting session is preserved across the restart with no
       automatic re-dispatch.
    E  a mid-turn session interrupted by a restart starts a new turn, or
       reports an explicit failure, with no permanent pending route.

  Not covered (listed in unverified): D (delivery rebinding during a primary
  handoff) and any real-provider resume behavior.

.PARAMETER Commit
  Revision label recorded in the result. Informational; the script verifies the
  current worktree, not an arbitrary checkout.

.PARAMETER ReleaseRoot
  Declared release directory recorded in the result.

.PARAMETER BackendPort
  Isolated backend port. Defaults to the first free port at or above 48629.

.PARAMETER TimeoutSeconds
  Health and scenario wait budget in seconds.

.PARAMETER NoBuild
  Use the existing binaries under apps/backend/bin instead of rebuilding.

.PARAMETER OutputPath
  Where to write the result JSON. Defaults to a file under the temp directory.
#>
[CmdletBinding()]
param(
  [string]$Commit,
  [string]$ReleaseRoot,
  [string]$InstanceKind = 'isolated',
  [int]$BackendPort,
  [int]$TimeoutSeconds = 150,
  [switch]$NoBuild,
  [string]$OutputPath
)

$ErrorActionPreference = 'Stop'
# No Set-StrictMode: the script reads dynamic JSON whose optional fields are
# legitimately absent, and strict mode would turn those reads into errors.

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RepoRoot = Split-Path -Parent $ScriptDir
$BackendDir = Join-Path $RepoRoot 'apps\backend'
$BinDir = Join-Path $BackendDir 'bin'
$DevIsolated = Join-Path $ScriptDir 'dev-isolated.ps1'
$KandevKill = Join-Path $ScriptDir 'kandev-kill.ps1'
$GuardedPorts = @(38429, 37429, 39429, 38430)
$GuardedActions = @('task.state', 'task.move', 'session.recover', 'session.launch', 'message.add')

function Write-Log {
  param([string]$Message)
  Write-Host ("verify-release: " + $Message)
}

# Native git writes line-ending and advice notices to stderr; under
# $ErrorActionPreference='Stop' those become terminating errors. Run git with a
# scoped non-terminating preference and stderr discarded.
function Invoke-GitQuiet {
  param([string]$RepoDir, [string[]]$GitArgs)
  $prev = $ErrorActionPreference
  $ErrorActionPreference = 'SilentlyContinue'
  try { & git -C $RepoDir @GitArgs 2>$null | Out-Null } finally { $ErrorActionPreference = $prev }
}

function Get-LastScenarioFailure {
  param($Scenarios)
  ($Scenarios | Where-Object { $_.status -eq 'failed' } | ForEach-Object { "$($_.id): $($_.reason)" }) -join '; '
}

# --- Revision identity -----------------------------------------------------
if (-not $Commit) { $Commit = (& git -C $RepoRoot rev-parse HEAD).Trim() }
$HeadSha = ($Commit -split '\s+')[0]
if (-not $ReleaseRoot) { $ReleaseRoot = Join-Path $env:TEMP 'kandev-verify-release' }

# --- Port selection (never a guarded production port) ----------------------
function Test-PortInUse {
  param([int]$Port)
  return ($null -ne (Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue))
}
if (-not $BackendPort) {
  $p = 48629
  while ($GuardedPorts -contains $p -or (Test-PortInUse -Port $p)) { $p++ }
  $BackendPort = $p
}
if ($GuardedPorts -contains $BackendPort) { throw "verify-release: refusing guarded port $BackendPort" }
$Base = "http://127.0.0.1:$BackendPort"
$Pidfile = Join-Path $env:TEMP ("kandev-dev-isolated-" + $BackendPort + '.pid')
$HomeFile = $Pidfile -replace '\.pid$', '.home'

# --- Build -----------------------------------------------------------------
if (-not $NoBuild) {
  Write-Log 'building kandev, agentctl, mock-agent'
  & make -C $BackendDir build-kandev build-agentctl build-mock-agent
  if ($LASTEXITCODE -ne 0) { throw "verify-release: build failed ($LASTEXITCODE)" }
}

# --- Isolated instance helpers --------------------------------------------
$Script:IsolatedHome = $null
function Start-Isolated {
  param([int]$TimeoutSec, [string]$HomeDirOverride)
  $devArgs = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $DevIsolated, '-NoBuild', '-BackendPort', "$BackendPort", '-Timeout', "$TimeoutSec")
  if ($HomeDirOverride) { $devArgs += @('-HomeDir', $HomeDirOverride) }
  & powershell.exe @devArgs
  if ($LASTEXITCODE -ne 0) { throw "verify-release: dev-isolated failed ($LASTEXITCODE)" }
  if (Test-Path -LiteralPath $HomeFile) { $Script:IsolatedHome = (Get-Content -LiteralPath $HomeFile -Raw).Trim() }
  $deadline = (Get-Date).AddSeconds($TimeoutSec)
  while ((Get-Date) -lt $deadline) {
    try {
      $r = Invoke-WebRequest -UseBasicParsing -Uri "$Base/api/v1/system/health" -TimeoutSec 3
      if ($r.StatusCode -ge 200 -and $r.StatusCode -lt 500) { return }
    } catch { }
    Start-Sleep -Seconds 1
  }
  throw 'verify-release: backend did not become healthy'
}

function Stop-Isolated {
  if (Test-Path -LiteralPath $Pidfile) {
    & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $KandevKill -Pidfile $Pidfile -Yes | Out-Null
    Start-Sleep -Seconds 2
  }
}

# --- HTTP ------------------------------------------------------------------
$Interlock = ''
function Invoke-KandevHttp {
  param([string]$Method, [string]$Path, $Body, [int]$Timeout = 30)
  $headers = @{ 'Content-Type' = 'application/json' }
  if ($Method -ne 'GET' -and $Path -like '/api/v1/agents*') {
    if (-not $Interlock) {
      $a = Invoke-RestMethod -Method Get -Uri "$Base/api/v1/app-state?path=%2Fsettings%2Fagents" -TimeoutSec $Timeout
      $Interlock = [string]$a.interimSettingsInterlockToken
    }
    $headers['X-Kandev-Interim-Settings-Interlock'] = $Interlock
  }
  $data = $null
  if ($null -ne $Body) { $data = [Text.Encoding]::UTF8.GetBytes((ConvertTo-Json -Depth 12 -Compress $Body)) }
  try {
    $resp = Invoke-WebRequest -UseBasicParsing -Method $Method -Uri ($Base + $Path) -Headers $headers -Body $data -TimeoutSec $Timeout
    if ([string]::IsNullOrWhiteSpace($resp.Content)) { return $null }
    return ($resp.Content | ConvertFrom-Json)
  } catch {
    $resp = $_.Exception.Response
    if ($resp) {
      $code = [int]$resp.StatusCode
      return @{ __error = $true; __status = $code }
    }
    throw
  }
}

# --- WebSocket -------------------------------------------------------------
function Invoke-KandevWs {
  param([string]$Action, $Payload, [int]$Timeout = 90)
  $wsUrl = $Base.Replace('http', 'ws') + '/ws'
  $ws = New-Object System.Net.WebSockets.ClientWebSocket
  $ct = [System.Threading.CancellationToken]::None
  try {
    $ws.ConnectAsync([Uri]$wsUrl, $ct).GetAwaiter().GetResult()
    $rid = [guid]::NewGuid().ToString()
    $msg = ConvertTo-Json -Depth 12 -Compress @{ id = $rid; type = 'request'; action = $Action; payload = $Payload }
    $bytes = [Text.Encoding]::UTF8.GetBytes($msg)
    $seg = New-Object 'System.ArraySegment[byte]' -ArgumentList @(, $bytes)
    $ws.SendAsync($seg, [System.Net.WebSockets.WebSocketMessageType]::Text, $true, $ct).GetAwaiter().GetResult()
    $deadline = (Get-Date).AddSeconds($Timeout)
    while ((Get-Date) -lt $deadline) {
      $sb = New-Object System.Text.StringBuilder
      $done = $false
      while (-not $done) {
        $buf = New-Object byte[] 65536
        $rseg = New-Object 'System.ArraySegment[byte]' -ArgumentList @(, $buf)
        $task = $ws.ReceiveAsync($rseg, $ct)
        if (-not $task.Wait([int]([Math]::Max(1000, ($deadline - (Get-Date)).TotalMilliseconds)))) {
          return @{ type = 'timeout'; payload = $null }
        }
        $res = $task.Result
        [void]$sb.Append([Text.Encoding]::UTF8.GetString($buf, 0, $res.Count))
        $done = $res.EndOfMessage
      }
      $text = $sb.ToString()
      if ([string]::IsNullOrWhiteSpace($text)) { continue }
      $m = $text | ConvertFrom-Json
      if ($m.id -eq $rid) { return $m }
    }
    return @{ type = 'timeout'; payload = $null }
  } finally {
    try { $ws.Dispose() } catch { }
  }
}

# --- Fixture ---------------------------------------------------------------
function New-Fixture {
  $repo = Join-Path $env:TEMP ("vrfixture-" + $BackendPort + '-' + [guid]::NewGuid().ToString('N').Substring(0, 6))
  New-Item -ItemType Directory -Path $repo -Force | Out-Null
  Invoke-GitQuiet -RepoDir $repo -GitArgs @('init', '-b', 'main')
  Set-Content -LiteralPath (Join-Path $repo 'README.txt') -Value 'verify-release' -Encoding ASCII
  Invoke-GitQuiet -RepoDir $repo -GitArgs @('-c', 'user.name=v', '-c', 'user.email=v@x', 'add', 'README.txt')
  Invoke-GitQuiet -RepoDir $repo -GitArgs @('-c', 'user.name=v', '-c', 'user.email=v@x', 'commit', '-m', 'init')

  $ws = Invoke-KandevHttp -Method POST -Path '/api/v1/workspaces' -Body @{ name = 'VR WS' }
  if ($ws.__error) { throw "workspace create failed" }
  $wf = Invoke-KandevHttp -Method POST -Path '/api/v1/workflows' -Body @{ workspace_id = $ws.id; name = 'VR WF'; workflow_template_id = 'simple' }
  if ($wf.__error) { throw "workflow create failed" }
  $steps = Invoke-KandevHttp -Method GET -Path "/api/v1/workflows/$($wf.id)/workflow/steps"
  $start = ($steps.steps | Sort-Object position | Where-Object { $_.is_start_step } | Select-Object -First 1)
  if (-not $start) { $start = ($steps.steps | Sort-Object position | Select-Object -First 1) }
  $rp = Invoke-KandevHttp -Method POST -Path "/api/v1/workspaces/$($ws.id)/repositories" -Body @{ name = 'VR Repo'; source_type = 'local'; local_path = $repo; default_branch = 'main' }
  if ($rp.__error) { throw "repository create failed" }

  $prof = $null
  for ($i = 0; $i -lt 60; $i++) {
    $agents = Invoke-KandevHttp -Method GET -Path '/api/v1/agents'
    $mock = $agents.agents | Where-Object { $_.name -eq 'mock-agent' } | Select-Object -First 1
    if ($mock -and $mock.profiles -and $mock.profiles.Count -gt 0) { $prof = $mock.profiles[0].id; break }
    Start-Sleep -Seconds 1
  }
  if (-not $prof) { throw 'no mock-agent profile' }
  $execs = Invoke-KandevHttp -Method GET -Path '/api/v1/executors'
  $wt = $execs.executors | Where-Object { $_.type -eq 'worktree' } | Select-Object -First 1
  $exprof = $wt.profiles[0].id
  return @{ workspace_id = $ws.id; workflow_id = $wf.id; step_id = $start.id; repository_id = $rp.id; profile_id = $prof; executor_profile_id = $exprof }
}

function New-Task {
  param($Fixture, [string]$Title, [string]$Prompt)
  $t = Invoke-KandevHttp -Method POST -Path '/api/v1/tasks' -Timeout 120 -Body @{
    workspace_id = $Fixture.workspace_id; title = $Title; description = $Prompt; start_agent = $true
    agent_profile_id = $Fixture.profile_id; workflow_id = $Fixture.workflow_id; workflow_step_id = $Fixture.step_id
    repositories = @(@{ repository_id = $Fixture.repository_id }); executor_profile_id = $Fixture.executor_profile_id
  }
  if ($t.__error) { throw "task create failed ($($t.__status))" }
  return $t
}

function Get-Session {
  param([string]$TaskId, [string]$SessionId)
  $s = Invoke-KandevHttp -Method GET -Path "/api/v1/tasks/$TaskId/sessions"
  return ($s.sessions | Where-Object { $_.id -eq $SessionId } | Select-Object -First 1)
}

function Get-AgentTurnCount {
  param([string]$SessionId)
  $m = Invoke-KandevHttp -Method GET -Path "/api/v1/task-sessions/$SessionId/messages?limit=200"
  if (-not $m.messages) { return 0 }
  return ($m.messages | Where-Object { $_.author_type -eq 'agent' -and $_.type -notin @('status', 'script_execution') }).Count
}

function Wait-TurnCount {
  param([string]$TaskId, [string]$SessionId, [int]$Before, [int]$Timeout)
  $deadline = (Get-Date).AddSeconds($Timeout)
  while ((Get-Date) -lt $deadline) {
    if ((Get-AgentTurnCount -SessionId $SessionId) -gt $Before) { return $true }
    Start-Sleep -Seconds 2
  }
  return $false
}

function Wait-SessionState {
  param([string]$TaskId, [string]$SessionId, [string[]]$Want, [int]$Timeout)
  $deadline = (Get-Date).AddSeconds($Timeout)
  $last = $null
  while ((Get-Date) -lt $deadline) {
    $last = Get-Session -TaskId $TaskId -SessionId $SessionId
    if ($last -and $Want -contains $last.state) { return $last }
    Start-Sleep -Seconds 1
  }
  return $last
}

# --- Run -------------------------------------------------------------------
$scenarios = @()
$started = $false
try {
  Write-Log "starting isolated instance on :$BackendPort"
  Start-Isolated -TimeoutSec $TimeoutSeconds
  $started = $true
  $fixture = New-Fixture

  # A: new run -> execution + first event + one turn complete
  try {
    $t = New-Task -Fixture $fixture -Title 'A_new_run' -Prompt '/e2e:simple-message'
    $sid = $t.session_id
    $ok = Wait-TurnCount -TaskId $t.id -SessionId $sid -Before 0 -Timeout 90
    $sess = Get-Session -TaskId $t.id -SessionId $sid
    $execId = if ($sess) { $sess.agent_execution_id } else { $null }
    if ($ok -and $execId) {
      $scenarios += @{ id = 'A'; definition = 'v09'; status = 'passed'; execution_id = $execId; first_event_at = (Get-Date).ToUniversalTime().ToString('o') }
    } else {
      $scenarios += @{ id = 'A'; definition = 'v09'; status = 'failed'; reason = "turn=$ok execution=$execId" }
    }
  } catch { $scenarios += @{ id = 'A'; definition = 'v09'; status = 'failed'; reason = $_.Exception.Message } }

  # B: completed session, restart, explicit continuation
  $bTask = $null; $bSid = $null; $bBefore = 0
  try {
    $t = New-Task -Fixture $fixture -Title 'B_continue' -Prompt '/e2e:simple-message'
    $bTask = $t.id; $bSid = $t.session_id
    [void](Wait-SessionState -TaskId $bTask -SessionId $bSid -Want @('WAITING_FOR_INPUT') -Timeout 90)
    $bBefore = Get-AgentTurnCount -SessionId $bSid
    $scenarios += @{ id = 'B'; definition = 'v09'; status = 'pending'; task = $bTask; session = $bSid; before = $bBefore }
  } catch { $scenarios += @{ id = 'B'; definition = 'v09'; status = 'failed'; reason = $_.Exception.Message } }

  # C: idle waiting session preserved
  $cTask = $null; $cSid = $null
  try {
    $t = New-Task -Fixture $fixture -Title 'C_idle_wait' -Prompt '/e2e:simple-message'
    $cTask = $t.id; $cSid = $t.session_id
    [void](Wait-SessionState -TaskId $cTask -SessionId $cSid -Want @('WAITING_FOR_INPUT') -Timeout 90)
    $scenarios += @{ id = 'C'; definition = 'v09'; status = 'pending'; task = $cTask; session = $cSid; before = (Get-AgentTurnCount -SessionId $cSid) }
  } catch { $scenarios += @{ id = 'C'; definition = 'v09'; status = 'failed'; reason = $_.Exception.Message } }

  # E: mid-turn open turn at restart
  $eTask = $null; $eSid = $null
  try {
    $t = New-Task -Fixture $fixture -Title 'E_midturn' -Prompt '/slow 3600'
    $eTask = $t.id; $eSid = $t.session_id
    [void](Wait-SessionState -TaskId $eTask -SessionId $eSid -Want @('RUNNING') -Timeout 90)
    $scenarios += @{ id = 'E'; definition = 'v09'; status = 'pending'; task = $eTask; session = $eSid }
  } catch { $scenarios += @{ id = 'E'; definition = 'v09'; status = 'failed'; reason = $_.Exception.Message } }

  # Restart the backend for B, C, E
  Write-Log 'restarting isolated backend'
  Stop-Isolated
  Start-Isolated -TimeoutSec $TimeoutSeconds -HomeDirOverride $Script:IsolatedHome

  # B: explicit continuation must begin a real turn
  $b = $scenarios | Where-Object { $_.id -eq 'B' }
  if ($b.status -eq 'pending') {
    $r = Invoke-KandevWs -Action 'session.recover' -Payload @{ task_id = $bTask; session_id = $bSid; action = 'continue_from_history' } -Timeout 120
    $ok = Wait-TurnCount -TaskId $bTask -SessionId $bSid -Before $bBefore -Timeout 90
    $sess = Get-Session -TaskId $bTask -SessionId $bSid
    if ($sess -and $sess.error_message) { $ok = $false }
    if ($ok) { $b.status = 'passed'; $b.execution_id = $sess.agent_execution_id } else { $b.status = 'failed'; $b.reason = "ws=$($r.type) error=$($sess.error_message)" }
  }

  # C: preserved with zero auto re-dispatch
  $c = $scenarios | Where-Object { $_.id -eq 'C' }
  if ($c.status -eq 'pending') {
    $sess = Get-Session -TaskId $cTask -SessionId $cSid
    $turns = Get-AgentTurnCount -SessionId $cSid
    if ($sess.state -eq 'WAITING_FOR_INPUT' -and $turns -le [int]$c.before) { $c.status = 'passed' } else { $c.status = 'failed'; $c.reason = "state=$($sess.state) turns=$turns before=$($c.before)" }
  }

  # E: message starts a turn or explicit failure; no permanent pending route
  $e = $scenarios | Where-Object { $_.id -eq 'E' }
  if ($e.status -eq 'pending') {
    $before = Get-AgentTurnCount -SessionId $eSid
    $r = Invoke-KandevWs -Action 'message.add' -Payload @{ task_id = $eTask; session_id = $eSid; content = '/e2e:simple-message' } -Timeout 120
    $turn = Wait-TurnCount -TaskId $eTask -SessionId $eSid -Before $before -Timeout 90
    $sess = Get-Session -TaskId $eTask -SessionId $eSid
    $route = [string]$sess.route_state
    $pendingTerminal = ($sess.state -eq 'WAITING_FOR_INPUT' -or $sess.state -eq 'FAILED') -and $route -eq 'starting'
    if ($turn -and -not $pendingTerminal) { $e.status = 'passed' } else { $e.status = 'failed'; $e.reason = "turn=$turn state=$($sess.state) route=$route ws=$($r.type)" }
  }

  # Web assets
  $webOk = $false
  try {
    $page = Invoke-WebRequest -UseBasicParsing -Uri "$Base/" -TimeoutSec 15
    $m = [regex]::Match($page.Content, 'assets/index-[^"\s]+\.js')
    if ($m.Success) {
      $asset = Invoke-WebRequest -UseBasicParsing -Uri ("$Base/" + $m.Value) -TimeoutSec 15
      $webOk = ($asset.StatusCode -ge 200 -and $asset.StatusCode -lt 400)
    }
  } catch { $webOk = $false }
} catch {
  Write-Log ("error: " + $_.Exception.Message)
} finally {
  if ($started) { Stop-Isolated }
}

# D is not exercised deterministically here.
$scenarios += @{ id = 'D'; definition = 'v09'; status = 'unverified'; reason = 'primary handoff delivery rebinding is not induced by this check' }

$coveredIds = @('A', 'B', 'C', 'E')
$failedCount = @($scenarios | Where-Object { $_.status -eq 'failed' }).Count
$coveredPassed = @($scenarios | Where-Object { $coveredIds -contains $_.id -and $_.status -eq 'passed' }).Count
# result is passed only when every covered scenario passed and none failed; a
# covered scenario left pending (for example because the restart step did not
# run to completion) must not be reported as covered.
$passed = (($failedCount -eq 0) -and ($coveredPassed -eq $coveredIds.Count))
$result = @{
  revision = $HeadSha
  release_dir = $ReleaseRoot
  web_assets_ok = [bool]$webOk
  scenarios = $scenarios
  result = $(if ($passed) { 'passed' } else { 'failed' })
  unverified = @(
    'D delivery rebinding during primary handoff',
    'real-provider resume and relaunch behavior'
  )
}

if (-not $OutputPath) { $OutputPath = Join-Path $env:TEMP 'kandev-verify-release-result.json' }
$result | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $OutputPath -Encoding UTF8
$result | ConvertTo-Json -Depth 8
Write-Log ("result=" + $result.result + " -> " + $OutputPath)
if (-not $passed) { exit 1 }
