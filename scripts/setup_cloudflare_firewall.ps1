#Requires -RunAsAdministrator
# ============================================================================
# [2026-09-25 보안 점검] Origin 직접 접근 차단
#
# 지금 상황: 인터넷에서 이 서버의 공인 IP로 80(그리고 443을 쓰게 되면 443도)
# 포트에 직접 접속해서 Host: pastellive.co.kr 헤더를 주면, Cloudflare를
# 거치지 않고도 실제 웹 애플리케이션이 그대로 응답한다. 이러면 Cloudflare의
# WAF/rate limiting/DDoS 방어를 전부 우회할 수 있다.
#
# 이 스크립트가 하는 일: Windows 방화벽에 "TCP 80/443은 Cloudflare의 공식
# IP 대역에서 오는 연결만 허용" 규칙을 추가한다. Cloudflare IP 목록은
# https://www.cloudflare.com/ips-v4 / ips-v6 에서 매번 새로 받아온다(하드코딩
# 안 함 - Cloudflare가 대역을 바꿀 수 있어서).
#
# 안전장치:
#   - 80/443 포트에만 영향을 준다. 원격 접속(RDP 등)이나 다른 서비스가 쓰는
#     포트는 절대 건드리지 않는다.
#   - 기본은 DRY RUN(미리보기)이다. 실제로 방화벽 규칙을 만들거나 바꾸려면
#     -Apply 스위치를 붙여서 다시 실행해야 한다.
#   - 기존에 80/443을 열어주던 규칙이 있으면 "비활성화"만 한다(삭제하지
#     않음) - 문제가 생기면 scripts\disable_cloudflare_firewall.ps1로 바로
#     되돌릴 수 있게.
#
# 사용법:
#   1) 먼저 미리보기로 확인:  powershell -ExecutionPolicy Bypass -File scripts\setup_cloudflare_firewall.ps1
#   2) 문제 없어 보이면 적용:  powershell -ExecutionPolicy Bypass -File scripts\setup_cloudflare_firewall.ps1 -Apply
#   3) 적용 후 반드시 외부(예: 스마트폰 데이터, 다른 네트워크)에서
#      https://pastellive.co.kr 가 정상 접속되는지 확인할 것.
#   4) 문제가 생기면:  powershell -ExecutionPolicy Bypass -File scripts\disable_cloudflare_firewall.ps1
# ============================================================================
param(
    [switch]$Apply
)

$ErrorActionPreference = 'Stop'
try {
    [Console]::OutputEncoding = [System.Text.Encoding]::UTF8
    $OutputEncoding = [System.Text.Encoding]::UTF8
} catch {}

$RuleNamePrefix = "Pastellive-CloudflareOnly"
$TargetPorts = @(80, 443)

Write-Host "=============================================="
Write-Host " Origin 직접 접근 차단 설정"
Write-Host " 모드: $(if ($Apply) { 'APPLY (실제 적용)' } else { 'DRY RUN (미리보기만, 변경 없음)' })"
Write-Host "=============================================="
Write-Host ""

# ---- 1) Cloudflare 공식 IP 대역 가져오기 ------------------------------------
Write-Host "[1/4] Cloudflare 공식 IP 대역 가져오는 중..."
try {
    $ipv4 = (Invoke-WebRequest -Uri "https://www.cloudflare.com/ips-v4" -UseBasicParsing -TimeoutSec 15).Content -split "`n" | Where-Object { $_.Trim() -ne "" }
    $ipv6 = (Invoke-WebRequest -Uri "https://www.cloudflare.com/ips-v6" -UseBasicParsing -TimeoutSec 15).Content -split "`n" | Where-Object { $_.Trim() -ne "" }
} catch {
    Write-Host "[오류] Cloudflare IP 목록을 가져오지 못했습니다: $($_.Exception.Message)"
    Write-Host "네트워크 연결을 확인하고 다시 시도해주세요. (하드코딩된 목록을 쓰지 않는 이유: 오래된 목록으로 막으면 나중에 Cloudflare가 새 IP로 요청을 보낼 때 정상 트래픽까지 막힐 수 있음)"
    exit 1
}
$allRanges = @($ipv4) + @($ipv6) | ForEach-Object { $_.Trim() } | Where-Object { $_ -ne "" }
Write-Host "  IPv4 대역 $($ipv4.Count)개, IPv6 대역 $($ipv6.Count)개 확인됨"
Write-Host ""

# ---- 2) 기존에 80/443을 열어주는 규칙 찾기 ----------------------------------
Write-Host "[2/4] 기존 방화벽 규칙 중 80/443 포트에 영향을 주는 것 찾는 중..."
$existingRules = @()
foreach ($port in $TargetPorts) {
    $portFilters = Get-NetFirewallPortFilter -Protocol TCP | Where-Object { $_.LocalPort -eq "$port" -or $_.LocalPort -eq "Any" }
    foreach ($pf in $portFilters) {
        $rule = $pf | Get-NetFirewallRule
        if ($rule.Direction -eq "Inbound" -and $rule.Enabled -eq "True" -and $rule.Name -notlike "$RuleNamePrefix*") {
            $existingRules += [PSCustomObject]@{ Rule = $rule; Port = $port }
        }
    }
}
if ($existingRules.Count -eq 0) {
    Write-Host "  80/443을 명시적으로 여는 기존 인바운드 규칙을 못 찾았습니다."
    Write-Host "  (Windows 기본 정책상 인바운드가 기본 차단이면 그동안 어떻게 열려있었는지 직접 확인이 필요합니다 - 공유기 포트포워딩만으로 열려있었을 수도 있음. 이 스크립트는 Windows 방화벽만 다룹니다.)"
} else {
    Write-Host "  다음 규칙들이 80/443에 영향을 주고 있습니다 (모두 포트 필터 기준으로만 찾음, 이름과 무관):"
    foreach ($e in $existingRules) {
        Write-Host "    - [$($e.Rule.Name)] 포트 $($e.Port), 프로필=$($e.Rule.Profile), 방향=$($e.Rule.Direction)"
    }
}
Write-Host ""

# ---- 3) 계획 요약 ------------------------------------------------------------
Write-Host "[3/4] 적용할 내용:"
Write-Host "  - 새 규칙 '$RuleNamePrefix-Allow' 생성: TCP 80,443 인바운드 허용, RemoteAddress = Cloudflare 대역만"
if ($existingRules.Count -gt 0) {
    Write-Host "  - 위에서 찾은 기존 규칙 $($existingRules.Count)개를 '비활성화'(삭제 아님)"
}
Write-Host "  - 80/443 이외의 포트(원격 접속 포함)는 전혀 건드리지 않습니다."
Write-Host ""

if (-not $Apply) {
    Write-Host "DRY RUN 모드라 아무것도 바꾸지 않았습니다."
    Write-Host "실제 적용하려면: powershell -ExecutionPolicy Bypass -File scripts\setup_cloudflare_firewall.ps1 -Apply"
    exit 0
}

# ---- 4) 실제 적용 ------------------------------------------------------------
Write-Host "[4/4] 적용 중..."

Remove-NetFirewallRule -DisplayName "$RuleNamePrefix-Allow" -ErrorAction SilentlyContinue

New-NetFirewallRule -DisplayName "$RuleNamePrefix-Allow" `
    -Direction Inbound -Action Allow -Protocol TCP -LocalPort $TargetPorts `
    -RemoteAddress $allRanges -Profile Any -Enabled True `
    -Description "[$(Get-Date -Format 'yyyy-MM-dd')] Cloudflare IP 대역에서만 80/443 허용 - scripts/setup_cloudflare_firewall.ps1로 생성됨" | Out-Null

Write-Host "  Cloudflare 전용 허용 규칙 생성 완료."

foreach ($e in $existingRules) {
    try {
        Disable-NetFirewallRule -Name $e.Rule.Name -ErrorAction Stop
        Write-Host "  기존 규칙 비활성화: $($e.Rule.Name)"
    } catch {
        Write-Host "  [경고] 기존 규칙 비활성화 실패($($e.Rule.Name)): $($_.Exception.Message) - 수동으로 확인해주세요."
    }
}

Write-Host ""
Write-Host "적용 완료. 지금 바로 확인하세요:"
Write-Host "  1) 스마트폰 데이터(와이파이 끄고) 등 이 네트워크가 아닌 곳에서 https://pastellive.co.kr 접속 확인"
Write-Host "  2) 문제가 있으면 즉시:  powershell -ExecutionPolicy Bypass -File scripts\disable_cloudflare_firewall.ps1"
Write-Host ""
Write-Host "참고: 원격 데스크톱(RDP)이나 다른 관리용 접속 포트는 이 스크립트가 전혀 건드리지 않았습니다."
Write-Host "      Cloudflare IP 대역은 주기적으로(월 1회 정도) 이 스크립트를 -Apply로 다시 실행해서 최신으로 갱신하는 걸 권장합니다."
