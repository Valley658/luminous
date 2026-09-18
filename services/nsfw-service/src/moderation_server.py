import json
import os
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

# Windows 로캘(한국어 Windows는 보통 CP949)이 파이썬 표준출력의 기본 인코딩이
# 되면서, watchdog.exe가 리다이렉트해 쌓는 service.log에 한글 print()가
# 깨진 바이트로 저장되는 문제가 있었다(로그 뷰어/에디터에서 다 깨져 보임).
# 콘솔이든 파일 리다이렉트든 항상 UTF-8로 쓰도록 강제해서 고친다.
try:
    sys.stdout.reconfigure(encoding="utf-8")
    sys.stderr.reconfigure(encoding="utf-8")
except Exception:
    pass

_detector = None
_detector_lock = threading.Lock()

HIGH_RISK_LABELS = {"FEMALE_GENITALIA_EXPOSED", "MALE_GENITALIA_EXPOSED", "ANUS_EXPOSED"}
AMBIGUOUS_LABELS = {"FEMALE_BREAST_EXPOSED", "BUTTOCKS_EXPOSED", "MALE_BREAST_EXPOSED"}

ALLOWED_ROOT = None


def log(msg: str):
    ts = time.strftime("%Y-%m-%d %H:%M:%S")
    print(f"[{ts}] {msg}", flush=True)


def init_allowed_root():
    global ALLOWED_ROOT
    configured = os.environ.get("NSFW_SERVICE_ALLOWED_ROOT", "").strip()
    if configured:
        raw = configured
    else:
        exe_dir = os.path.dirname(os.path.abspath(sys.argv[0]))
        raw = os.path.normpath(os.path.join(exe_dir, "..", "static"))
    try:
        resolved = os.path.realpath(raw)
    except OSError:
        resolved = raw
    ALLOWED_ROOT = resolved.replace("\\", "/")
    log(f"허용 루트 경로: {ALLOWED_ROOT}")


def resolve_under_allowed_root(raw_path: str):
    if not raw_path:
        return None, "path 값이 없음"
    normalized = raw_path.replace("\\", "/")
    directory, base = os.path.split(normalized)
    if not base:
        return None, f"path 값이 올바르지 않음: {raw_path}"
    directory = directory or "."
    try:
        resolved_dir = os.path.realpath(directory).replace("\\", "/")
    except OSError:
        return None, f"path의 디렉터리가 존재하지 않음: {directory}"

    candidate = f"{resolved_dir}/{base}"
    root = ALLOWED_ROOT or ""
    within_root = candidate == root or (
        candidate.startswith(root) and len(candidate) > len(root) and candidate[len(root)] == "/"
    )
    if not within_root:
        return None, f"허용된 경로({root}) 밖을 가리킴 - path: {candidate}"
    return candidate, None


def get_detector():
    global _detector
    with _detector_lock:
        if _detector is None:
            from nudenet import NudeDetector
            _detector = NudeDetector()
    return _detector


def classify_image(path: str):
    detector = get_detector()
    raw = detector.detect(path)
    detections = [{"label": d["class"], "score": float(d["score"])} for d in raw]

    high_risk_score = max(
        (d["score"] for d in detections if d["label"] in HIGH_RISK_LABELS), default=0.0
    )
    ambiguous_candidates = [d["score"] for d in detections if d["label"] in AMBIGUOUS_LABELS]
    ambiguous_candidates.append(high_risk_score)
    ambiguous_score = max(ambiguous_candidates, default=0.0)

    high_risk_threshold = float(os.environ.get("NSFW_HIGH_RISK_THRESHOLD", "0.5"))
    ambiguous_threshold = float(os.environ.get("NSFW_AMBIGUOUS_THRESHOLD", "0.3"))

    if high_risk_score >= high_risk_threshold:
        verdict = "block"
    elif ambiguous_score >= ambiguous_threshold:
        verdict = "flag"
    else:
        verdict = "clear"

    return {
        "verdict": verdict,
        "detections": detections,
        "highRiskScore": high_risk_score,
        "ambiguousScore": ambiguous_score,
    }


class Handler(BaseHTTPRequestHandler):
    server_version = "nsfw-service/1.0"

    def log_message(self, fmt, *args):
        log("%s - %s" % (self.address_string(), fmt % args))

    def _send_json(self, status: int, payload: dict):
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _err(self, message: str, status: int = 200):
        self._send_json(status, {"success": False, "error": message})

    def do_GET(self):
        if self.path == "/health":
            self._send_json(200, {"status": "ok"})
            return
        self._err("not found", 404)

    def do_POST(self):
        if self.path != "/moderate":
            self._err("not found", 404)
            return

        length = int(self.headers.get("Content-Length", "0") or "0")
        max_body = 1 << 20
        if length <= 0 or length > max_body:
            self._err("요청 본문 크기가 올바르지 않음")
            return
        raw_body = self.rfile.read(length)

        try:
            body = json.loads(raw_body)
        except (json.JSONDecodeError, UnicodeDecodeError):
            self._err("요청 JSON 파싱 실패")
            return
        if not isinstance(body, dict):
            self._err("요청 JSON 파싱 실패")
            return

        raw_path = body.get("path")
        resolved, err = resolve_under_allowed_root(raw_path if isinstance(raw_path, str) else "")
        if err:
            log(f"[보안] moderate 거부: {err}")
            self._err(err)
            return
        if not os.path.isfile(resolved):
            self._err(f"파일이 존재하지 않음: {raw_path}")
            return

        try:
            result = classify_image(resolved)
        except Exception as e:
            log(f"[오류] 추론 실패 ({resolved}): {e}")
            self._err(f"이미지 분석 실패: {e}")
            return

        self._send_json(200, {"success": True, **result})


def main():
    init_allowed_root()
    port = int(os.environ.get("NSFW_SERVICE_PORT", "8095"))
    log("nsfw-service 시작 준비 중 (모델 로딩)...")
    get_detector()
    log(f"nsfw-service 기동 완료: 127.0.0.1:{port}")
    server = ThreadingHTTPServer(("127.0.0.1", port), Handler)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
