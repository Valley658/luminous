"""사진을 보고 그 안에 스텔라이브 어떤 멤버가 있는지 알아맞히는 아주 작은
로컬 서비스.

CLIP(이미지-텍스트 임베딩 모델)의 이미지 인코더 부분만 떼어써서, 업로드된
사진과 각 멤버의 대표 사진을 같은 벡터 공간에 올려놓고 코사인 유사도로 제일
가까운 멤버를 찾는다. 루미가 쓰는 대화용 로컬 LLM(qwen2.5:3b)한테 사진을
직접 보여주고 "이 사람 누구야?" 하고 맞혀보라고 시키는 방식은 쓰지 않는데,
그 이유는:
  1) 그 모델은 애초에 글자만 이해하는 텍스트 전용 모델이라 사진 자체를
     못 봄(비전 모델이 아님).
  2) 설령 비전 모델로 바꾸더라도, 스텔라이브 멤버들은 그 모델이 학습할 때
     본 적 없는 캐릭터라 이름을 맞히지 못하고 지어낼 가능성이 큼.
그래서 "우리가 미리 등록해둔 대표 사진과 얼마나 닮았는지"를 직접 계산하는
이 방식이 훨씬 정확하고 빠르다. 식별된 이름만 텍스트로 뽑아서, 실제 답변은
기존 텍스트 LLM(+멤버 상세 정보)이 만든다 - go-server의 lumi_ai.go 참고.

melo-tts-service(제거됨)/nsfw-service와 같은 패턴: 외부 웹 프레임워크 없이
표준 라이브러리 http.server만으로 돌아가는 로컬 전용 HTTP 서비스, Go 서버가
로컬호스트로만 호출한다.
"""

import io
import json
import os
import sys
import threading
import time
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import numpy as np
from PIL import Image

# Windows 로캘(한국어 Windows는 보통 CP949)이 파이썬 표준출력의 기본 인코딩이
# 되면서, watchdog.exe가 리다이렉트해 쌓는 service.log에 한글 print()가
# 깨진 바이트로 저장되는 문제가 있었다(로그 뷰어/에디터에서 다 깨져 보임).
# 콘솔이든 파일 리다이렉트든 항상 UTF-8로 쓰도록 강제해서 고친다.
try:
    sys.stdout.reconfigure(encoding="utf-8")
    sys.stderr.reconfigure(encoding="utf-8")
except Exception:
    pass

MODEL_URL = (
    "https://huggingface.co/Xenova/clip-vit-base-patch32/resolve/main/"
    "onnx/vision_model_quantized.onnx?download=true"
)
BASE_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
MODEL_PATH = os.path.join(BASE_DIR, "models", "vision_model_quantized.onnx")
REF_CACHE_DIR = os.path.join(BASE_DIR, "reference_cache")
# member-id-service/src/ -> member-id-service/ -> services/ -> 프로젝트 루트 -> static/
# (2026-09-18: 서비스 폴더들을 services/ 밑으로 모으면서 한 단계 더 깊어짐)
STATIC_DIR = os.path.normpath(os.path.join(BASE_DIR, "..", "..", "static"))

CLIP_MEAN = np.array([0.48145466, 0.4578275, 0.40821073], dtype=np.float32)
CLIP_STD = np.array([0.26862954, 0.26130258, 0.27577711], dtype=np.float32)

# go-server/internal/data/members.go의 SIDEBAR_MEMBERS와 같이 맞춰서 관리할
# 것 - 두 프로그램이 서로 다른 언어(Go/Python)라 자동으로 공유가 안 되니,
# 멤버가 추가/교체되면 여기도 같이 고쳐야 함. (강지/김블루는 members.go에도
# 아직 제대로 된 개인 사진이 없이 둘이 같은 임시 사진을 쓰고 있어서, 그
# 사진으론 서로 구분이 안 되니 일단 여기선 뺐음 - 진짜 사진이 등록되면
# 추가하면 됨.)
MEMBERS = [
    {"name": "칸나", "img": "/static/images/members/kanna.webp"},
    {"name": "유니", "img": "/static/images/members/yuni.webp"},
    {"name": "후야", "img": "/static/images/members/huya.webp"},
    {"name": "히나", "img": "/static/images/members/hina.webp"},
    {"name": "리제", "img": "/static/images/members/lize.webp"},
    {"name": "마시로", "img": "/static/images/members/mashiro.webp"},
    {"name": "타비", "img": "/static/images/members/tabi.webp"},
    {"name": "린", "img": "/static/images/members/rin.webp"},
    {"name": "시부키", "img": "/static/images/members/shibuki.webp"},
    {"name": "나나", "img": "/static/images/members/nana.webp"},
    {"name": "리코", "img": "/static/images/members/riko.webp"},
]

# 코사인 유사도가 이보다 낮으면 "확실치 않다"고 보고 이름을 알려주지 않는다
# (아예 관련 없는 사진인데도 그나마 제일 가까운 멤버를 억지로 갖다붙이는
# 걸 막기 위함). CLIP은 애니메이션풍 캐릭터 구분용으로 학습된 모델이 아니라서
# 생각보다 이 값을 너그럽게 잡으면 엉뚱한 멤버로 확신에 차서 오답하는 경우가
# 있어(예: 자꾸 같은 멤버로 쏠림) - 0.72 -> 0.75로 좀 더 보수적으로 올림.
MATCH_THRESHOLD = 0.75
# 1등과 2등 유사도 차이가 이보다 작으면 "어느 쪽인지 애매하다"고 보고 미확정
# 처리한다(1등만 보고 판단하면, 여러 멤버가 고만고만하게 가까운 애매한
# 사진에서도 매번 같은 멤버로 확신에 차 답해버리는 문제가 있었음).
MATCH_MARGIN = 0.03

_session = None
_ready = threading.Event()
_load_error = None
_reference_names = []
_reference_matrix = None  # shape (N, 512), 각 행이 이미 L2 정규화된 임베딩


def log(msg: str):
    ts = time.strftime("%Y-%m-%d %H:%M:%S")
    print(f"[{ts}] {msg}", flush=True)


def ensure_model():
    if os.path.exists(MODEL_PATH) and os.path.getsize(MODEL_PATH) > 0:
        return
    os.makedirs(os.path.dirname(MODEL_PATH), exist_ok=True)
    log("이미지 인식 모델(CLIP, 약 90MB) 다운로드 중... (최초 1회만)")
    tmp_path = MODEL_PATH + ".part"
    urllib.request.urlretrieve(MODEL_URL, tmp_path)
    os.replace(tmp_path, MODEL_PATH)
    log("모델 다운로드 완료.")


def resolve_reference_image_path(spec: str) -> str:
    """멤버 대표 사진의 실제 로컬 파일 경로를 돌려준다. /static/ 로 시작하면
    이 프로젝트의 static 폴더에서 바로 찾고, http(s)면 한 번 받아서
    reference_cache/에 캐시해둔다(그 다음부터는 다시 안 받음)."""
    if spec.startswith("http://") or spec.startswith("https://"):
        os.makedirs(REF_CACHE_DIR, exist_ok=True)
        ext = os.path.splitext(spec.split("?")[0])[1] or ".img"
        safe_name = "".join(c for c in spec if c.isalnum())[-40:] + ext
        cached = os.path.join(REF_CACHE_DIR, safe_name)
        if not os.path.exists(cached):
            log(f"참고 사진 다운로드: {spec}")
            # namu.wiki 같은 위키 이미지 CDN은 Referer 없이(즉 브라우저에서 그
            # 페이지를 열어서 받아온 게 아니라고 판단되면) 핫링크 방지로 요청을
            # 막는 경우가 있음 - User-Agent만으론 못 뚫는 경우가 있어서 Referer도
            # 같이 보냄. 이게 조용히 실패하면 그 멤버만 인식 대상에서 빠지게 되고
            # (build_references 참고) 겉보기엔 "이 멤버는 사진을 올려도 항상
            # 모르는 사람이라고 함" 처럼 보이는 원인이 될 수 있어서 중요함.
            req = urllib.request.Request(
                spec,
                headers={
                    "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36",
                    "Referer": "https://namu.wiki/",
                },
            )
            with urllib.request.urlopen(req, timeout=15) as resp:
                data = resp.read()
            with open(cached, "wb") as f:
                f.write(data)
        return cached
    if spec.startswith("/static/"):
        return os.path.join(STATIC_DIR, spec[len("/static/"):])
    return spec


def preprocess_image(img: Image.Image) -> np.ndarray:
    """CLIP(openai/clip-vit-base-patch32)의 표준 전처리: 224x224로 리사이즈,
    0~1로 스케일, CLIP 전용 평균/표준편차로 정규화, CHW로 변환."""
    img = img.convert("RGB").resize((224, 224), Image.BICUBIC)
    arr = np.asarray(img).astype(np.float32) / 255.0
    arr = (arr - CLIP_MEAN) / CLIP_STD
    arr = arr.transpose(2, 0, 1)[None, ...].astype(np.float32)
    return arr


def embed_image(img: Image.Image) -> np.ndarray:
    pixel_values = preprocess_image(img)
    (embedding,) = _session.run(None, {"pixel_values": pixel_values})
    vec = embedding[0]
    norm = np.linalg.norm(vec)
    if norm > 0:
        vec = vec / norm
    return vec


def embed_image_bytes(image_bytes: bytes) -> np.ndarray:
    return embed_image(Image.open(io.BytesIO(image_bytes)))


# ---- 같은/거의 같은 사진이 반복 업로드될 때를 위한 캐시 -----------------------
# 여러 방문자가 같은 팬아트나 같은 스크린샷을 올릴 수 있으니, 매번 CLIP
# 임베딩을 새로 계산하지 않고 "지각적 해시(perceptual hash)"로 먼저 대충
# 비교해서 사실상 같은 사진이면 이전 결과를 그대로 재사용한다 - phash-service가
# 팬아트 재업로드를 잡아내는 것과 같은 아이디어를 여기 로컬 캐시 용도로 작게
# 재구현한 것(별도 서비스를 새로 두기엔 너무 작은 기능이라 이 파일 안에 둠).
_id_cache_lock = threading.Lock()
_id_cache = []  # [(phash:int, member:str|None, score:float), ...], 오래된 것부터
_ID_CACHE_MAX = 300
_ID_CACHE_HAMMING_THRESHOLD = 4  # 8x8 평균 해시(64비트) 기준 - 완전 동일하거나
                                  # 재압축/미세한 리사이즈 정도만 다른 "사실상 같은
                                  # 사진"만 재사용하도록 일부러 타이트하게 잡음.


def compute_phash(img: Image.Image) -> int:
    """아주 단순한 8x8 평균 해시. 정밀한 유사도 판정이 목적이 아니라, 같은
    파일이 재업로드되거나 JPEG로 다시 저장되는 정도의 차이만 흡수하면 되는
    캐시 키라서 이 정도로 충분함."""
    small = img.convert("L").resize((8, 8), Image.LANCZOS)
    pixels = list(small.getdata())
    avg = sum(pixels) / len(pixels)
    bits = 0
    for i, p in enumerate(pixels):
        if p >= avg:
            bits |= 1 << i
    return bits


def _hamming(a: int, b: int) -> int:
    return bin(a ^ b).count("1")


def _cache_lookup(phash: int):
    with _id_cache_lock:
        for cached_hash, member, score in _id_cache:
            if _hamming(phash, cached_hash) <= _ID_CACHE_HAMMING_THRESHOLD:
                return member, score
    return None


def _cache_store(phash: int, member, score: float):
    with _id_cache_lock:
        _id_cache.append((phash, member, score))
        if len(_id_cache) > _ID_CACHE_MAX:
            del _id_cache[: len(_id_cache) - _ID_CACHE_MAX]


def build_references():
    global _reference_names, _reference_matrix
    names = []
    vectors = []
    for m in MEMBERS:
        try:
            local_path = resolve_reference_image_path(m["img"])
            with open(local_path, "rb") as f:
                data = f.read()
            vec = embed_image_bytes(data)
            names.append(m["name"])
            vectors.append(vec)
            log(f"참고 사진 등록 완료: {m['name']}")
        except Exception as e:  # noqa: BLE001
            log(f"[경고] {m['name']} 참고 사진 처리 실패(이 멤버는 인식 대상에서 빠짐): {e}")
    _reference_names = names
    _reference_matrix = np.stack(vectors, axis=0) if vectors else np.zeros((0, 512), dtype=np.float32)


def load_everything():
    global _session, _load_error
    try:
        import onnxruntime as ort

        ensure_model()
        log("모델 불러오는 중...")
        _session = ort.InferenceSession(MODEL_PATH, providers=["CPUExecutionProvider"])
        log("멤버 참고 사진들 등록 중...")
        build_references()
        if not _reference_names:
            raise RuntimeError("등록된 참고 사진이 하나도 없음")
        _ready.set()
        log(f"준비 완료 - 인식 가능한 멤버 {len(_reference_names)}명: {', '.join(_reference_names)}")
    except Exception as e:  # noqa: BLE001
        _load_error = str(e)
        log(f"[오류] 초기화 실패: {e}")


def identify(image_bytes: bytes):
    """반환: (멤버 이름 또는 None, 유사도 점수, 지각적 해시)"""
    img = Image.open(io.BytesIO(image_bytes))
    phash = compute_phash(img)
    cached = _cache_lookup(phash)
    if cached is not None:
        return cached[0], cached[1], phash

    vec = embed_image(img)
    sims = _reference_matrix @ vec
    order = np.argsort(sims)[::-1]
    best_idx = int(order[0])
    best_score = float(sims[best_idx])
    second_score = float(sims[int(order[1])]) if len(order) > 1 else -1.0

    top3 = ", ".join(f"{_reference_names[int(i)]}={float(sims[int(i)]):.3f}" for i in order[:3])
    log(f"인식 순위: {top3}")

    if best_score < MATCH_THRESHOLD:
        member = None
    elif best_score - second_score < MATCH_MARGIN:
        # 1등과 2등이 너무 붙어있으면(=애매하면) 특정 멤버 하나로 계속 쏠려서
        # 자꾸 같은 멤버로 오답나는 것보다, 모르겠다고 솔직히 말하는 쪽이 낫다고
        # 판단해서 미확정 처리한다.
        log(f"[안내] 1등({_reference_names[best_idx]})과 2등 차이가 {best_score - second_score:.3f}로 너무 작아 애매한 걸로 보고 '모르는 사람' 처리함")
        member = None
    else:
        member = _reference_names[best_idx]

    _cache_store(phash, member, best_score)
    return member, best_score, phash


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        log("%s - %s" % (self.address_string(), fmt % args))

    def _send_json(self, status: int, payload: dict):
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/health":
            if _ready.is_set():
                self._send_json(200, {"status": "ok", "ready": True, "members": _reference_names})
            elif _load_error:
                self._send_json(503, {"status": "error", "error": _load_error})
            else:
                self._send_json(503, {"status": "loading", "ready": False})
            return
        self._send_json(404, {"error": "not found"})

    def do_POST(self):
        if self.path != "/identify":
            self._send_json(404, {"error": "not found"})
            return
        if not _ready.is_set():
            self._send_json(503, {"error": "model still loading"})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if length <= 0 or length > 15 * 1024 * 1024:  # 15MB 상한
                self._send_json(400, {"error": "invalid image size"})
                return
            raw = self.rfile.read(length)
            name, score, phash = identify(raw)
        except Exception as e:  # noqa: BLE001
            log(f"[오류] 이미지 인식 실패: {e}")
            self._send_json(400, {"error": "이미지를 처리할 수 없어(지원 안 되는 형식일 수 있음)"})
            return
        # phash는 Go 서버가 "완전히 같은/거의 같은 사진 + 같은 질문"이면 루미
        # 답변 자체를 다시 만들지 않고 캐시해둔 걸 그대로 재사용하는 데 씀
        # (member-id-service 자체 캐시는 이 요청 안에서 이미 활용됐고, 이건
        # 그보다 한 단계 위 - LLM 응답까지 건너뛰기 위한 키).
        self._send_json(200, {"member": name, "score": score, "phash": f"{phash:016x}"})


def main():
    port = int(os.environ.get("MEMBER_ID_SERVICE_PORT", "8098"))
    threading.Thread(target=load_everything, daemon=True).start()
    server = ThreadingHTTPServer(("127.0.0.1", port), Handler)
    log(f"멤버 사진 인식 서비스 시작 - http://127.0.0.1:{port}")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
