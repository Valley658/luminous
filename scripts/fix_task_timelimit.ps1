#Requires -RunAsAdministrator
# ============================================================================
# PastelliveApp 작업이 정해진 시간(실행 시간 제한)이 지나면 Windows 작업
# 스케줄러가 자동으로 강제 종료해버리는 문제를 고친다.
#
# 증상: service.log에 "자연 종료 (연속 빠른 실패 0회) - 2초 후 재시작
# (이번 실행 359.9초)"가 정확히 6분(360초)마다 반복됨, exit code는 항상
# 0xC000013A(STATUS_CONTROL_C_EXIT - 콘솔 강제종료 신호를 받았을 때 나오는
# 코드) - 이건 앱 자체가 죽는 게 아니라 Windows 작업 스케줄러가 "실행 시간
# 제한"(Settings.ExecutionTimeLimit) 설정 때문에 6분마다 프로세스를 강제로
# 끊어버리는 것으로 보인다. schtasks /create로 처음 등록할 때 이 값을
# 명시적으로 안 껐으면, 일부 Windows 버전/환경에서 기본값이 예상보다 짧게
# 적용되는 경우가 있다.
#
# 영향: 서버가 6분마다 강제 재시작되면서, 시작할 때마다 유튜브 영상 목록을
# 다시 불러오는 작업이 계속 겹쳐서(원래 30분에 한 번이면 될 걸 훨씬 자주)
# 유튜브 API 할당량이 하루 일찍 바닥나고, 그러면 그 뒤로는 최신 영상 몇 개
# (RSS 기본분)만 겨우 뜨는 상태가 되어 "칸나 빼고 다른 멤버 영상이 하나도
# 안 뜬다"처럼 보이는 문제로 이어진 것으로 추정됨.
# ============================================================================
$ErrorActionPreference = 'Continue'
try { [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch {}

$TaskName = 'PastelliveApp'

Write-Host "=============================================="
Write-Host " $TaskName 작업의 실행 시간 제한 확인/제거"
Write-Host "=============================================="

$task = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
if (-not $task) {
    Write-Host "[오류] '$TaskName' 작업을 찾을 수 없습니다. install_all.bat 14번으로 먼저 등록해주세요."
    Read-Host "Enter 키를 누르면 창이 닫힙니다"
    exit 1
}

$currentLimit = $task.Settings.ExecutionTimeLimit
Write-Host "현재 실행 시간 제한: $currentLimit"
Write-Host ""

if ($currentLimit -eq 'PT0S' -or [string]::IsNullOrEmpty($currentLimit)) {
    Write-Host "이미 제한이 없는 상태입니다(PT0S = 무제한). 다른 원인일 수 있습니다."
} else {
    Write-Host "실행 시간 제한을 제거합니다 (무제한으로 변경)..."
    $task.Settings.ExecutionTimeLimit = 'PT0S'
    try {
        Set-ScheduledTask -TaskName $TaskName -Settings $task.Settings -ErrorAction Stop | Out-Null
        Write-Host "변경 완료. 확인:"
        (Get-ScheduledTask -TaskName $TaskName).Settings.ExecutionTimeLimit
        Write-Host ""
        Write-Host "지금 실행 중인 프로세스는 기존 제한이 이미 걸려있을 수 있으니,"
        Write-Host "한 번 재시작해서 새 설정으로 다시 띄웁니다..."
        schtasks /end /tn $TaskName *> $null
        Start-Sleep -Seconds 2
        schtasks /run /tn $TaskName *> $null
        Write-Host "재시작 완료."
    } catch {
        Write-Host "[오류] 설정 변경 실패: $($_.Exception.Message)"
    }
}

Write-Host ""
Write-Host "이제 6분 이상 계속 떠 있는지 logs\service.log 에서 지켜봐주세요"
Write-Host "(다음 [watchdog] 실행: 줄이 이전 줄에서 6분보다 더 지나서 찍히면 성공)."
Read-Host "Enter 키를 누르면 창이 닫힙니다"
