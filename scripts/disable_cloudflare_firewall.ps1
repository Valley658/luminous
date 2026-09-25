#Requires -RunAsAdministrator
# setup_cloudflare_firewall.ps1로 만든 제한을 되돌린다: 새로 만든 Cloudflare
# 전용 허용 규칙을 지우고, 그때 비활성화했던 기존 규칙들을 다시 켠다.
# (기존 규칙을 "삭제"가 아니라 "비활성화"만 했었기 때문에 이 스크립트로
# 정확히 되돌릴 수 있음.)
$ErrorActionPreference = 'Continue'
try {
    [Console]::OutputEncoding = [System.Text.Encoding]::UTF8
    $OutputEncoding = [System.Text.Encoding]::UTF8
} catch {}

$RuleNamePrefix = "Pastellive-CloudflareOnly"

Write-Host "Cloudflare 전용 제한 되돌리는 중..."

Remove-NetFirewallRule -DisplayName "$RuleNamePrefix-Allow" -ErrorAction SilentlyContinue
Write-Host "  '$RuleNamePrefix-Allow' 규칙 제거 완료."

$disabledByUs = Get-NetFirewallRule -Direction Inbound | Where-Object {
    $_.Enabled -eq "False" -and (
        (Get-NetFirewallPortFilter -AssociatedNetFirewallRule $_ -ErrorAction SilentlyContinue) |
        Where-Object { $_.LocalPort -eq "80" -or $_.LocalPort -eq "443" }
    )
}
if ($disabledByUs) {
    Write-Host "다음 규칙들이 비활성화 상태입니다 - 원래 80/443을 열어주던 규칙이라면 다시 켜세요:"
    foreach ($r in $disabledByUs) {
        Write-Host "  - $($r.Name) ($($r.DisplayName))"
    }
    Write-Host ""
    Write-Host "전부 다시 켜려면: Get-NetFirewallRule -Direction Inbound | Where-Object {`$_.Enabled -eq 'False'} | Enable-NetFirewallRule"
    Write-Host "(단, 다른 이유로 원래부터 꺼져있던 규칙까지 같이 켜질 수 있으니 목록을 먼저 확인하세요.)"
} else {
    Write-Host "비활성화된 80/443 관련 규칙을 찾지 못했습니다."
}

Write-Host ""
Write-Host "완료. https://pastellive.co.kr 와 Origin IP:80 직접 접근 둘 다 다시 확인해보세요."
