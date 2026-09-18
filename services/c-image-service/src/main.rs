mod httpserver;
mod imageproc;
mod subprocess;

use httpserver::{HttpRequest, Route};
use imageproc::RgbaImage;
use serde_json::{json, Value};
use std::path::{Path, PathBuf};
use std::sync::OnceLock;

static ALLOWED_ROOT: OnceLock<String> = OnceLock::new();

fn normalize_slashes(s: &str) -> String {
    s.replace('\\', "/")
}

fn canonicalize_to_string(p: &Path) -> Option<String> {
    std::fs::canonicalize(p).ok().map(|pb| normalize_slashes(&pb.to_string_lossy()))
}

fn init_allowed_root() {
    let configured = std::env::var("IMAGE_SERVICE_ALLOWED_ROOT").ok().filter(|v| !v.is_empty());
    let raw = if let Some(c) = configured {
        c
    } else {
        let cwd = std::env::current_dir().unwrap_or_else(|_| PathBuf::from("."));
        format!("{}/../static", normalize_slashes(&cwd.to_string_lossy()))
    };
    let resolved = canonicalize_to_string(Path::new(&raw)).unwrap_or_else(|| normalize_slashes(&raw));
    let _ = ALLOWED_ROOT.set(resolved);
}

fn allowed_root() -> &'static str {
    ALLOWED_ROOT.get().map(|s| s.as_str()).unwrap_or("")
}

/// Mirrors the C service's resolve_under_allowed_root: resolves the parent
/// directory of the given path and requires it (plus filename) to live
/// under the configured allowed root. Returns the resolved absolute path.
fn resolve_under_allowed_root(raw_path: Option<&str>, field_name: &str) -> Result<String, String> {
    let raw_path = match raw_path {
        Some(p) if !p.is_empty() => p,
        _ => return Err(format!("{} 값이 없음", field_name)),
    };
    let normalized = normalize_slashes(raw_path);
    let path = Path::new(&normalized);
    let dir = path.parent().unwrap_or_else(|| Path::new("."));
    let base = match path.file_name() {
        Some(b) => b.to_string_lossy().into_owned(),
        None => return Err(format!("{} 값이 올바르지 않음: {}", field_name, raw_path)),
    };

    let resolved_dir = match canonicalize_to_string(dir) {
        Some(d) => d,
        None => return Err(format!("{}의 디렉터리가 존재하지 않음: {}", field_name, dir.display())),
    };

    let candidate = format!("{}/{}", resolved_dir, base);
    let root = allowed_root();
    let within_root = candidate == root || (candidate.starts_with(root) && candidate.as_bytes().get(root.len()) == Some(&b'/'));
    if !within_root {
        return Err(format!("허용된 경로({}) 밖을 가리킴 - {}: {}", root, field_name, candidate));
    }
    Ok(candidate)
}

fn file_exists(path: &str) -> bool {
    Path::new(path).exists()
}

fn strip_ext(path: &str) -> String {
    match path.rfind('.') {
        Some(dot) if !path[dot..].contains('/') => path[..dot].to_string(),
        _ => path.to_string(),
    }
}

fn ends_with_ci(s: &str, suffix: &str) -> bool {
    s.to_lowercase().ends_with(&suffix.to_lowercase())
}

fn parse_body(req: &HttpRequest) -> Option<Value> {
    serde_json::from_slice::<Value>(&req.body).ok().filter(|v| v.is_object())
}

fn get_str<'a>(j: &'a Value, key: &str) -> Option<&'a str> {
    j.get(key).and_then(|v| v.as_str())
}
fn get_num(j: &Value, key: &str, fallback: f64) -> f64 {
    j.get(key).and_then(|v| v.as_f64()).unwrap_or(fallback)
}
fn get_bool(j: &Value, key: &str, fallback: bool) -> bool {
    j.get(key).and_then(|v| v.as_bool()).unwrap_or(fallback)
}

fn err_json(message: &str) -> (i32, String) {
    (200, json!({ "success": false, "error": message }).to_string())
}

fn ok200(v: Value) -> (i32, String) {
    (200, v.to_string())
}

// ---------------- handlers ----------------

fn h_health(_req: &HttpRequest) -> (i32, String) {
    (200, "{\"status\":\"ok\"}".to_string())
}

fn h_process_image(req: &HttpRequest) -> (i32, String) {
    let j = match parse_body(req) {
        Some(v) => v,
        None => return err_json("요청 JSON 파싱 실패"),
    };
    let path = get_str(&j, "path");
    let max_dimension = get_num(&j, "maxDimension", 1920.0) as i32;
    let quality = get_num(&j, "quality", 85.0) as i32;
    let avif = get_bool(&j, "avif", false);
    let avif_quality = get_num(&j, "avifQuality", quality as f64) as i32;

    let src = match resolve_under_allowed_root(path, "path") {
        Ok(p) => p,
        Err(e) => {
            httpserver::log(&format!("[보안] process-image 거부: {}", e));
            return err_json(&e);
        }
    };
    if !file_exists(&src) {
        return err_json(&format!("파일이 존재하지 않음: {}", path.unwrap_or("")));
    }

    let img = match imageproc::read_with_orientation(&src) {
        Ok(i) => i,
        Err(e) => return err_json(&e),
    };
    let (width, height) = (img.width, img.height);
    let resized = imageproc::resize_to_max(&img, max_dimension);

    let lower_is_webp = ends_with_ci(&src, ".webp");
    let out_path = if lower_is_webp { src.clone() } else { format!("{}.webp", strip_ext(&src)) };

    if let Err(e) = imageproc::write_webp(&resized, &out_path, quality) {
        return err_json(&e);
    }

    let replaced = out_path != src;
    if replaced {
        let _ = std::fs::remove_file(&src);
    }

    let mut obj = json!({
        "success": true,
        "outputPath": out_path,
        "width": width,
        "height": height,
    });

    if avif {
        let avif_path = format!("{}.avif", strip_ext(&out_path));
        match imageproc::write_avif(&resized, &avif_path, avif_quality) {
            Ok(_) => obj["avifPath"] = json!(avif_path),
            Err(e) => {
                httpserver::log(&format!("process-image avif 생성 실패(webp는 정상 완료): {}", e));
                obj["avifPath"] = Value::Null;
            }
        }
    }

    httpserver::log(&format!("process-image 완료: {} -> {}", path.unwrap_or(""), out_path));
    ok200(obj)
}

fn h_thumbnail(req: &HttpRequest) -> (i32, String) {
    let j = match parse_body(req) {
        Some(v) => v,
        None => return err_json("요청 JSON 파싱 실패"),
    };
    let path = get_str(&j, "path");
    let output_path = get_str(&j, "outputPath");
    let max_dimension = get_num(&j, "maxDimension", 480.0) as i32;
    let quality = get_num(&j, "quality", 75.0) as i32;
    let avif = get_bool(&j, "avif", false);
    let avif_quality = get_num(&j, "avifQuality", quality as f64) as i32;

    let src = match resolve_under_allowed_root(path, "path") {
        Ok(p) => p,
        Err(e) => {
            httpserver::log(&format!("[보안] thumbnail 거부: {}", e));
            return err_json(&e);
        }
    };
    let out = match resolve_under_allowed_root(output_path, "outputPath") {
        Ok(p) => p,
        Err(e) => {
            httpserver::log(&format!("[보안] thumbnail 거부: {}", e));
            return err_json(&e);
        }
    };
    if !file_exists(&src) {
        return err_json(&format!("파일이 존재하지 않음: {}", path.unwrap_or("")));
    }

    let img = match imageproc::read_with_orientation(&src) {
        Ok(i) => i,
        Err(e) => return err_json(&e),
    };
    let resized = imageproc::resize_to_max(&img, max_dimension);

    if let Err(e) = imageproc::write_webp(&resized, &out, quality) {
        return err_json(&e);
    }

    let mut obj = json!({ "success": true, "outputPath": out });

    if avif {
        let avif_path = format!("{}.avif", strip_ext(&out));
        match imageproc::write_avif(&resized, &avif_path, avif_quality) {
            Ok(_) => obj["avifPath"] = json!(avif_path),
            Err(e) => {
                httpserver::log(&format!("thumbnail avif 생성 실패(webp는 정상 완료): {}", e));
                obj["avifPath"] = Value::Null;
            }
        }
    }

    httpserver::log(&format!("thumbnail 완료: {} -> {}", path.unwrap_or(""), out));
    ok200(obj)
}

fn h_lqip(req: &HttpRequest) -> (i32, String) {
    let j = match parse_body(req) {
        Some(v) => v,
        None => return err_json("요청 JSON 파싱 실패"),
    };
    let path = get_str(&j, "path");
    let box_size = get_num(&j, "box", 16.0) as i32;
    let quality = get_num(&j, "quality", 40.0) as i32;

    let src = match resolve_under_allowed_root(path, "path") {
        Ok(p) => p,
        Err(e) => {
            httpserver::log(&format!("[보안] lqip 거부: {}", e));
            return err_json(&e);
        }
    };
    if !file_exists(&src) {
        return err_json(&format!("파일이 존재하지 않음: {}", path.unwrap_or("")));
    }

    let img = match imageproc::read_with_orientation(&src) {
        Ok(i) => i,
        Err(e) => return err_json(&e),
    };
    match imageproc::build_lqip_data_uri(&img, box_size, quality) {
        Ok(uri) => ok200(json!({ "success": true, "dataUri": uri })),
        Err(e) => err_json(&e),
    }
}

fn h_clip_sprite(req: &HttpRequest) -> (i32, String) {
    let j = match parse_body(req) {
        Some(v) => v,
        None => return err_json("요청 JSON 파싱 실패"),
    };
    let path = get_str(&j, "path");
    let output_path = get_str(&j, "outputPath");
    let duration_seconds = get_num(&j, "durationSeconds", 15.0);
    let frame_count = get_num(&j, "frameCount", 10.0) as i32;
    let frame_width = get_num(&j, "frameWidth", 160.0) as i32;
    let quality = get_num(&j, "quality", 70.0) as i32;

    let src = match resolve_under_allowed_root(path, "path") {
        Ok(p) => p,
        Err(e) => {
            httpserver::log(&format!("[보안] clip-sprite 거부: {}", e));
            return err_json(&e);
        }
    };
    let out = match resolve_under_allowed_root(output_path, "outputPath") {
        Ok(p) => p,
        Err(e) => {
            httpserver::log(&format!("[보안] clip-sprite 거부: {}", e));
            return err_json(&e);
        }
    };
    if !file_exists(&src) {
        return err_json(&format!("파일이 존재하지 않음: {}", path.unwrap_or("")));
    }

    match imageproc::build_clip_sprite(&src, &out, frame_count, frame_width, duration_seconds, quality) {
        Ok(result) => {
            httpserver::log(&format!(
                "clip-sprite 완료: {} -> {} ({}프레임)",
                path.unwrap_or(""),
                out,
                result.frame_count
            ));
            ok200(json!({
                "success": true,
                "outputPath": out,
                "frameCount": result.frame_count,
                "frameWidth": result.frame_width,
                "frameHeight": result.frame_height,
            }))
        }
        Err(e) => err_json(&e),
    }
}

fn h_phash(req: &HttpRequest) -> (i32, String) {
    let j = match parse_body(req) {
        Some(v) => v,
        None => return err_json("요청 JSON 파싱 실패"),
    };
    let path = get_str(&j, "path");
    let src = match resolve_under_allowed_root(path, "path") {
        Ok(p) => p,
        Err(e) => {
            httpserver::log(&format!("[보안] phash 거부: {}", e));
            return err_json(&e);
        }
    };
    if !file_exists(&src) {
        return err_json(&format!("파일이 존재하지 않음: {}", path.unwrap_or("")));
    }
    let img: RgbaImage = match imageproc::read_with_orientation(&src) {
        Ok(i) => i,
        Err(e) => return err_json(&e),
    };
    let hash = imageproc::compute_dhash(&img);
    ok200(json!({ "success": true, "phash": format!("{:016x}", hash) }))
}

fn h_grayscale32(req: &HttpRequest) -> (i32, String) {
    let j = match parse_body(req) {
        Some(v) => v,
        None => return err_json("요청 JSON 파싱 실패"),
    };
    let path = get_str(&j, "path");
    let src = match resolve_under_allowed_root(path, "path") {
        Ok(p) => p,
        Err(e) => {
            httpserver::log(&format!("[보안] grayscale32 거부: {}", e));
            return err_json(&e);
        }
    };
    if !file_exists(&src) {
        return err_json(&format!("파일이 존재하지 않음: {}", path.unwrap_or("")));
    }
    let img = match imageproc::read_with_orientation(&src) {
        Ok(i) => i,
        Err(e) => return err_json(&e),
    };
    let gray = imageproc::compute_grayscale32_base64(&img);
    ok200(json!({ "success": true, "gray": gray }))
}

fn h_dominant_color(req: &HttpRequest) -> (i32, String) {
    let j = match parse_body(req) {
        Some(v) => v,
        None => return err_json("요청 JSON 파싱 실패"),
    };
    let path = get_str(&j, "path");
    let src = match resolve_under_allowed_root(path, "path") {
        Ok(p) => p,
        Err(e) => {
            httpserver::log(&format!("[보안] dominant-color 거부: {}", e));
            return err_json(&e);
        }
    };
    if !file_exists(&src) {
        return err_json(&format!("파일이 존재하지 않음: {}", path.unwrap_or("")));
    }
    let img = match imageproc::read_with_orientation(&src) {
        Ok(i) => i,
        Err(e) => return err_json(&e),
    };
    match imageproc::compute_dominant_color(&img) {
        Ok(hex) => {
            httpserver::log(&format!("dominant-color 완료: {} -> {}", path.unwrap_or(""), hex));
            ok200(json!({ "success": true, "color": hex }))
        }
        Err(e) => err_json(&e),
    }
}

fn h_gif_to_webp(req: &HttpRequest) -> (i32, String) {
    let j = match parse_body(req) {
        Some(v) => v,
        None => return err_json("요청 JSON 파싱 실패"),
    };
    let path = get_str(&j, "path");
    let output_path = get_str(&j, "outputPath");
    let quality = get_num(&j, "quality", 75.0) as i32;

    let src = match resolve_under_allowed_root(path, "path") {
        Ok(p) => p,
        Err(e) => {
            httpserver::log(&format!("[보안] gif-to-webp 거부: {}", e));
            return err_json(&e);
        }
    };
    let out = match resolve_under_allowed_root(output_path, "outputPath") {
        Ok(p) => p,
        Err(e) => {
            httpserver::log(&format!("[보안] gif-to-webp 거부: {}", e));
            return err_json(&e);
        }
    };
    if !file_exists(&src) {
        return err_json(&format!("파일이 존재하지 않음: {}", path.unwrap_or("")));
    }

    if let Err(e) = imageproc::convert_gif_to_webp(&src, &out, quality) {
        return err_json(&e);
    }
    httpserver::log(&format!("gif-to-webp 완료: {} -> {}", path.unwrap_or(""), out));
    ok200(json!({ "success": true, "outputPath": out }))
}

static ROUTES: &[Route] = &[
    Route { method: "GET", path: "/health", handler: h_health },
    Route { method: "POST", path: "/process-image", handler: h_process_image },
    Route { method: "POST", path: "/thumbnail", handler: h_thumbnail },
    Route { method: "POST", path: "/lqip", handler: h_lqip },
    Route { method: "POST", path: "/clip-sprite", handler: h_clip_sprite },
    Route { method: "POST", path: "/phash", handler: h_phash },
    Route { method: "POST", path: "/grayscale32", handler: h_grayscale32 },
    Route { method: "POST", path: "/gif-to-webp", handler: h_gif_to_webp },
    Route { method: "POST", path: "/dominant-color", handler: h_dominant_color },
];

fn main() {
    init_allowed_root();

    let port: u16 = std::env::var("IMAGE_SERVICE_PORT").ok().and_then(|v| v.parse().ok()).unwrap_or(8090);
    let max_body: usize = std::env::var("IMAGE_SERVICE_MAX_REQUEST_BYTES")
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(1024 * 1024);
    let max_subprocesses: i32 = std::env::var("IMAGE_SERVICE_MAX_SUBPROCESSES")
        .ok()
        .and_then(|v| v.parse().ok())
        .unwrap_or(4);
    subprocess::set_max_concurrent(max_subprocesses);

    httpserver::log(&format!("허용 경로 루트(allowed.root): {} (이 밖의 path/outputPath는 거부됩니다)", allowed_root()));
    httpserver::log(&format!("요청 본문 최대 크기: {} bytes", max_body));
    if max_subprocesses > 0 {
        httpserver::log(&format!("서브프로세스(cwebp/avifenc/ffmpeg 등) 동시 실행 제한: 최대 {}개", max_subprocesses));
    } else {
        httpserver::log("서브프로세스(cwebp/avifenc/ffmpeg 등) 동시 실행 제한: 무제한");
    }

    httpserver::run("127.0.0.1", port, ROUTES, max_body, 4);
}
