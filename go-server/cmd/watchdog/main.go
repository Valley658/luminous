package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

const (
	fastFailThreshold    = 15 * time.Second
	fastFailStreakLimit  = 5
	normalRestartDelay   = 2 * time.Second
	backoffRestartDelay  = 300 * time.Second
	rebuildCheckInterval = 3 * time.Second
	healthCheckGrace     = 10 * time.Second
	healthCheckFailMax   = 3
	healthCheckTimeout   = 3 * time.Second
	logMaxBytes          = 10 * 1024 * 1024
	logBackupCount       = 3
)

func main() {
	exePath := flag.String("exe", "", "감시할 실행 파일 경로 (필수)")
	logPath := flag.String("log", "", "로그 파일 경로 (필수)")
	healthURL := flag.String("health", "", "선택 - 이 URL을 주기적으로 호출해 응답 없으면 강제 재시작")
	workDirFlag := flag.String("cwd", "", "자식 프로세스 작업 디렉터리 (기본값: -exe가 있는 폴더)")
	healthGraceFlag := flag.Duration("health-grace", healthCheckGrace,
		"선택 - 헬스체크를 시작하기 전 봐줄 유예 시간(예: XTTS-v2처럼 최초 기동 시 모델을 "+
			"수 GB 내려받아야 해서 오래 걸리는 서비스는 이 값을 늘려야 그 사이에 강제 재시작으로 "+
			"영원히 못 뜨는 무한 재시작 루프에 빠지지 않음). 기본값은 기존과 동일하게 10s.")
	healthFailMaxFlag := flag.Int("health-fail-max", healthCheckFailMax,
		"선택 - 이 횟수만큼 연속으로 헬스체크에 실패하면 강제 재시작. 기본값은 기존과 동일하게 3.")
	flag.Parse()

	if *exePath == "" || *logPath == "" {
		fmt.Fprintln(os.Stderr, "usage: watchdog -exe <path> -log <path> [-health <url>] [-cwd <dir>] [-health-grace <dur>] [-health-fail-max <n>]")
		os.Exit(2)
	}
	if *healthGraceFlag <= 0 {
		*healthGraceFlag = healthCheckGrace
	}
	if *healthFailMaxFlag <= 0 {
		*healthFailMaxFlag = healthCheckFailMax
	}

	workDir := *workDirFlag
	if workDir == "" {
		workDir = filepath.Dir(*exePath)
	}
	if err := os.MkdirAll(filepath.Dir(*logPath), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "로그 디렉터리 생성 실패: %v\n", err)
	}

	if _, err := os.Stat(*exePath); err != nil {
		appendLog(*logPath, fmt.Sprintf("%s를 찾을 수 없습니다. 먼저 빌드해주세요.", *exePath))
		os.Exit(1)
	}

	consecutiveFastFails := 0
	for {
		rotateLogIfNeeded(*logPath)
		elapsed, reason := runOnce(*exePath, workDir, *logPath, *healthURL, *healthGraceFlag, *healthFailMaxFlag)

		var wait time.Duration
		var reasonMsg string
		switch reason {
		case "rebuild":
			consecutiveFastFails = 0
			wait = normalRestartDelay
			reasonMsg = "재빌드로 인한 재시작"
		case "health":
			consecutiveFastFails = 0
			wait = normalRestartDelay
			reasonMsg = "헬스체크 실패로 인한 강제 재시작"
		default:
			if elapsed < fastFailThreshold {
				consecutiveFastFails++
			} else {
				consecutiveFastFails = 0
			}
			if consecutiveFastFails >= fastFailStreakLimit {
				wait = backoffRestartDelay
			} else {
				wait = normalRestartDelay
			}
			reasonMsg = fmt.Sprintf("자연 종료 (연속 빠른 실패 %d회)", consecutiveFastFails)
		}
		appendLog(*logPath, fmt.Sprintf("%s - %.0f초 후 재시작 (이번 실행 %.1f초)", reasonMsg, wait.Seconds(), elapsed.Seconds()))
		time.Sleep(wait)
	}
}

func runOnce(exePath, workDir, logPath, healthURL string, healthGrace time.Duration, healthFailMax int) (time.Duration, string) {
	start := time.Now()
	launchMtime := exeMtime(exePath)

	logf, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return time.Since(start), ""
	}
	defer logf.Close()

	appendLogF(logf, fmt.Sprintf("실행: %s", exePath))

	cmd := exec.Command(exePath)
	cmd.Dir = workDir
	cmd.Stdout = logf
	cmd.Stderr = logf
	if err := cmd.Start(); err != nil {
		appendLogF(logf, fmt.Sprintf("실행 실패: %v", err))
		return time.Since(start), ""
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	ticker := time.NewTicker(rebuildCheckInterval)
	defer ticker.Stop()
	consecutiveHealthFails := 0

	for {
		select {
		case waitErr := <-done:
			appendLogF(logf, describeExit(waitErr))
			return time.Since(start), ""
		case <-ticker.C:
			if exeMtime(exePath).After(launchMtime) {
				appendLogF(logf, "재빌드 감지(실행 파일 변경됨) - 서비스를 재시작합니다.")
				terminate(cmd, done)
				return time.Since(start), "rebuild"
			}
			if healthURL != "" && time.Since(start) >= healthGrace {
				if healthCheckOK(healthURL) {
					consecutiveHealthFails = 0
				} else {
					consecutiveHealthFails++
					if consecutiveHealthFails >= healthFailMax {
						appendLogF(logf, fmt.Sprintf("헬스체크 %d회 연속 실패(%s) - 프로세스가 떠있지만 죽은 것으로 보고 강제 재시작합니다.", healthFailMax, healthURL))
						terminate(cmd, done)
						return time.Since(start), "health"
					}
				}
			}
		}
	}
}

func describeExit(waitErr error) string {
	if waitErr == nil {
		return "종료 감지: 정상 종료(exit code 0) - 앱이 스스로 종료함(비정상, 조사 필요)"
	}
	if exitErr, ok := waitErr.(*exec.ExitError); ok {
		code := exitErr.ExitCode()
		return fmt.Sprintf("종료 감지: exit code=%d (0x%X), 상세=%v - 외부 강제종료면 대개 비정상 코드로 나타남", code, uint32(code), waitErr)
	}
	return fmt.Sprintf("종료 감지: cmd.Wait() 오류=%v (프로세스를 아예 못 띄웠거나 핸들 오류)", waitErr)
}

func terminate(cmd *exec.Cmd, done chan error) {
	// 감시 대상(-exe)이 .bat 파일이면 실제로 뜨는 건 cmd.exe -> (그 안에서 또
	// python.exe 같은) 손자 프로세스 구조가 됨. cmd.Process.Kill()은 직계
	// 자식(cmd.exe)만 죽이고 그 밑의 손자 프로세스는 그대로 살아남아서, MeloTTS
	// 처럼 무거운 파이썬 프로세스가 파일(.pyd/.dll)을 계속 붙잡은 채 고아로
	// 남는 문제가 있었음 - taskkill /T로 프로세스 트리 전체를 같이 죽인다.
	if cmd.Process != nil {
		killTree(cmd.Process.Pid)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
}

func killTree(pid int) {
	killCmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
	_ = killCmd.Run()
}

func exeMtime(path string) time.Time {
	fi, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

func healthCheckOK(url string) bool {
	client := &http.Client{Timeout: healthCheckTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func appendLog(path, msg string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	appendLogF(f, msg)
}

func appendLogF(f *os.File, msg string) {
	line := fmt.Sprintf("[%s] [watchdog] %s\n", time.Now().Format("2006-01-02 15:04:05"), msg)
	_, _ = f.WriteString(line)
}

func rotateLogIfNeeded(path string) {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() < logMaxBytes {
		return
	}
	for i := logBackupCount - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", path, i)
		dst := fmt.Sprintf("%s.%d", path, i+1)
		if _, err := os.Stat(src); err == nil {
			os.Remove(dst)
			os.Rename(src, dst)
		}
	}
	backup1 := path + ".1"
	os.Remove(backup1)
	os.Rename(path, backup1)
}
