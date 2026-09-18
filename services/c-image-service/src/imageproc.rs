use crate::subprocess::run_subprocess;
use image::{GenericImageView, ImageEncoder};
use std::fs;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{SystemTime, UNIX_EPOCH};

pub type ProcResult<T> = Result<T, String>;

#[derive(Clone)]
pub struct RgbaImage {
    pub width: i32,
    pub height: i32,
    pub pixels: Vec<u8>,
}

pub struct SpriteResult {
    pub frame_count: i32,
    pub frame_width: i32,
    pub frame_height: i32,
}

fn bin_path(env_name: &str, default_name: &str) -> String {
    std::env::var(env_name).ok().filter(|v| !v.is_empty()).unwrap_or_else(|| default_name.to_string())
}

fn subprocess_timeout_seconds() -> u64 {
    std::env::var("IMAGE_SERVICE_SUBPROCESS_TIMEOUT")
        .ok()
        .and_then(|v| v.parse::<u64>().ok())
        .filter(|n| *n > 0)
        .unwrap_or(60)
}

static TMP_COUNTER: AtomicU64 = AtomicU64::new(0);

fn make_temp_path(suffix: &str) -> ProcResult<PathBuf> {
    let dir = std::env::temp_dir();
    let pid = std::process::id();
    let nanos = SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_nanos()).unwrap_or(0);
    let c = TMP_COUNTER.fetch_add(1, Ordering::SeqCst);
    let name = format!("pastellive_img_{}_{}_{}{}", pid, nanos, c, suffix);
    let path = dir.join(name);
    fs::File::create(&path).map_err(|e| format!("임시 파일 생성 실패: {}", e))?;
    Ok(path)
}

fn has_suffix_ci(s: &str, suffix: &str) -> bool {
    s.to_lowercase().ends_with(&suffix.to_lowercase())
}

// ---------- EXIF orientation ----------

fn read_u16(p: &[u8], big_endian: bool) -> u32 {
    if big_endian { (p[0] as u32) << 8 | p[1] as u32 } else { (p[1] as u32) << 8 | p[0] as u32 }
}
fn read_u32(p: &[u8], big_endian: bool) -> u32 {
    if big_endian {
        (p[0] as u32) << 24 | (p[1] as u32) << 16 | (p[2] as u32) << 8 | p[3] as u32
    } else {
        (p[3] as u32) << 24 | (p[2] as u32) << 16 | (p[1] as u32) << 8 | p[0] as u32
    }
}

fn parse_tiff_orientation(data: &[u8], tiff_start: usize) -> i32 {
    if tiff_start + 8 > data.len() { return 0; }
    let big_endian = if &data[tiff_start..tiff_start + 2] == b"II" {
        false
    } else if &data[tiff_start..tiff_start + 2] == b"MM" {
        true
    } else {
        return 0;
    };
    let ifd_offset = read_u32(&data[tiff_start + 4..], big_endian) as usize;
    let ifd_pos = tiff_start + ifd_offset;
    if ifd_pos + 2 > data.len() { return 0; }
    let entry_count = read_u16(&data[ifd_pos..], big_endian) as usize;
    for i in 0..entry_count {
        let entry_pos = ifd_pos + 2 + i * 12;
        if entry_pos + 12 > data.len() { break; }
        let tag = read_u16(&data[entry_pos..], big_endian);
        if tag == 0x0112 {
            let value = read_u16(&data[entry_pos + 8..], big_endian) as i32;
            if (1..=8).contains(&value) { return value; }
            return 0;
        }
    }
    0
}

fn read_jpeg_exif_orientation(jpeg: &[u8]) -> i32 {
    if jpeg.len() < 4 || jpeg[0] != 0xFF || jpeg[1] != 0xD8 { return 1; }
    let mut pos = 2usize;
    while pos + 4 <= jpeg.len() {
        if jpeg[pos] != 0xFF { break; }
        let marker = jpeg[pos + 1];
        if marker == 0xD8 || marker == 0xD9 { pos += 2; continue; }
        if marker == 0x01 || (0xD0..=0xD7).contains(&marker) { pos += 2; continue; }
        let seg_len = ((jpeg[pos + 2] as usize) << 8) | jpeg[pos + 3] as usize;
        if marker == 0xE1 {
            let exif_start = pos + 4;
            if exif_start + 6 <= jpeg.len() && &jpeg[exif_start..exif_start + 6] == b"Exif\0\0" {
                let tiff_start = exif_start + 6;
                let orientation = parse_tiff_orientation(jpeg, tiff_start);
                if orientation != 0 { return orientation; }
            }
        }
        if marker == 0xDA { break; }
        pos += 2 + seg_len;
    }
    1
}

fn apply_orientation(src: RgbaImage, orientation: i32) -> RgbaImage {
    if orientation <= 1 { return src; }
    let (w, h) = (src.width, src.height);
    let swap = orientation >= 5;
    let (nw, nh) = if swap { (h, w) } else { (w, h) };
    let mut out = vec![0u8; (nw as usize) * (nh as usize) * 4];

    for y in 0..h {
        for x in 0..w {
            let (sx, sy) = match orientation {
                2 => (w - 1 - x, y),
                3 => (w - 1 - x, h - 1 - y),
                4 => (x, h - 1 - y),
                5 => (y, x),
                6 => (y, h - 1 - x),
                7 => (w - 1 - y, h - 1 - x),
                8 => (w - 1 - y, x),
                _ => (x, y),
            };
            let sp = ((sy as usize) * (w as usize) + sx as usize) * 4;
            let (dx, dy) = if !swap {
                (x, y)
            } else {
                match orientation {
                    5 => (y, x),
                    6 => (h - 1 - y, x),
                    7 => (h - 1 - y, w - 1 - x),
                    8 => (y, w - 1 - x),
                    _ => (x, y),
                }
            };
            let dp = ((dy as usize) * (nw as usize) + dx as usize) * 4;
            out[dp..dp + 4].copy_from_slice(&src.pixels[sp..sp + 4]);
        }
    }
    RgbaImage { width: nw, height: nh, pixels: out }
}

// ---------- decode ----------

fn decode_dynamic(bytes: &[u8]) -> ProcResult<RgbaImage> {
    let img = image::load_from_memory(bytes)
        .map_err(|e| format!("이미지 디코딩 실패(지원 포맷: JPEG/PNG/BMP/WebP/HEIC, 그 외는 지원 밖): {}", e))?;
    let (w, h) = img.dimensions();
    let rgba = img.to_rgba8();
    Ok(RgbaImage { width: w as i32, height: h as i32, pixels: rgba.into_raw() })
}

fn load_png_file(path: &Path) -> ProcResult<RgbaImage> {
    let img = image::open(path).map_err(|e| format!("PNG 파일을 읽을 수 없음: {}", e))?;
    let (w, h) = img.dimensions();
    let rgba = img.to_rgba8();
    Ok(RgbaImage { width: w as i32, height: h as i32, pixels: rgba.into_raw() })
}

pub fn read_webp(path: &str) -> ProcResult<RgbaImage> {
    let temp_png = make_temp_path(".png")?;
    let argv = vec![bin_path("DWEBP_PATH", "dwebp"), path.to_string(), "-o".to_string(), temp_png.to_string_lossy().into_owned()];
    let res = run_subprocess(&argv, subprocess_timeout_seconds());
    let result = if res.spawn_failed {
        Err(format!("dwebp 실행 파일을 찾을 수 없음({}). DWEBP_PATH 환경변수로 경로를 지정하세요.", argv[0]))
    } else if res.timed_out {
        Err("dwebp 실행이 타임아웃 초과로 강제 종료됨".to_string())
    } else if res.exit_code != 0 {
        Err(format!("dwebp 디코딩 실패(exit={}): {:.150}", res.exit_code, res.output))
    } else {
        load_png_file(&temp_png)
    };
    let _ = fs::remove_file(&temp_png);
    result
}

pub fn read_heic(path: &str) -> ProcResult<RgbaImage> {
    let temp_png = make_temp_path(".png")?;
    let argv = vec![bin_path("HEIC_CONVERT_PATH", "heif-convert"), path.to_string(), temp_png.to_string_lossy().into_owned()];
    let res = run_subprocess(&argv, subprocess_timeout_seconds());
    let result = if res.spawn_failed {
        Err(format!(
            "heif-convert 실행 파일을 찾을 수 없음({}). HEIC_CONVERT_PATH 환경변수로 libheif의 heif-convert 경로를 지정하세요.",
            argv[0]
        ))
    } else if res.timed_out {
        Err("heif-convert 실행이 타임아웃 초과로 강제 종료됨".to_string())
    } else if res.exit_code != 0 {
        Err(format!("heic 디코딩 실패(exit={}): {:.150}", res.exit_code, res.output))
    } else {
        load_png_file(&temp_png)
    };
    let _ = fs::remove_file(&temp_png);
    result
}

pub fn read_with_orientation(path: &str) -> ProcResult<RgbaImage> {
    if has_suffix_ci(path, ".gif") {
        return Err("gif는 이 서비스에서 처리하지 않음(애니메이션 보존은 기존 경로가 담당)".to_string());
    }
    if has_suffix_ci(path, ".webp") {
        return read_webp(path);
    }
    if has_suffix_ci(path, ".heic") || has_suffix_ci(path, ".heif") {
        return read_heic(path);
    }

    let filebuf = fs::read(path).map_err(|e| format!("파일을 열 수 없음: {} ({})", path, e))?;
    let img = decode_dynamic(&filebuf)?;
    let orientation = read_jpeg_exif_orientation(&filebuf);
    Ok(apply_orientation(img, orientation))
}

// ---------- resize ----------

fn clamp_u8(v: f64) -> u8 {
    if v < 0.0 { 0 } else if v > 255.0 { 255 } else { (v + 0.5) as u8 }
}

fn scale_image(src: &RgbaImage, new_w: i32, new_h: i32) -> RgbaImage {
    let mut out = vec![0u8; (new_w as usize) * (new_h as usize) * 4];
    let x_ratio = src.width as f64 / new_w as f64;
    let y_ratio = src.height as f64 / new_h as f64;

    for y in 0..new_h {
        let sy = (y as f64 + 0.5) * y_ratio - 0.5;
        let y0 = sy.floor() as i32;
        let fy = sy - y0 as f64;
        let y0c = y0.clamp(0, src.height - 1);
        let y1c = (y0 + 1).clamp(0, src.height - 1);

        for x in 0..new_w {
            let sx = (x as f64 + 0.5) * x_ratio - 0.5;
            let x0 = sx.floor() as i32;
            let fx = sx - x0 as f64;
            let x0c = x0.clamp(0, src.width - 1);
            let x1c = (x0 + 1).clamp(0, src.width - 1);

            let p00 = ((y0c as usize) * (src.width as usize) + x0c as usize) * 4;
            let p10 = ((y0c as usize) * (src.width as usize) + x1c as usize) * 4;
            let p01 = ((y1c as usize) * (src.width as usize) + x0c as usize) * 4;
            let p11 = ((y1c as usize) * (src.width as usize) + x1c as usize) * 4;

            let dp = ((y as usize) * (new_w as usize) + x as usize) * 4;
            for c in 0..4 {
                let top = src.pixels[p00 + c] as f64 * (1.0 - fx) + src.pixels[p10 + c] as f64 * fx;
                let bot = src.pixels[p01 + c] as f64 * (1.0 - fx) + src.pixels[p11 + c] as f64 * fx;
                out[dp + c] = clamp_u8(top * (1.0 - fy) + bot * fy);
            }
        }
    }
    RgbaImage { width: new_w, height: new_h, pixels: out }
}

pub fn resize_to_max(src: &RgbaImage, max_dimension: i32) -> RgbaImage {
    let long_side = src.width.max(src.height);
    if long_side <= max_dimension {
        return src.clone();
    }
    let ratio = max_dimension as f64 / long_side as f64;
    let nw = ((src.width as f64 * ratio + 0.5) as i32).max(1);
    let nh = ((src.height as f64 * ratio + 0.5) as i32).max(1);
    scale_image(src, nw, nh)
}

pub fn resize_to_width(src: &RgbaImage, target_width: i32) -> RgbaImage {
    if src.width == target_width {
        return src.clone();
    }
    let ratio = target_width as f64 / src.width as f64;
    let nh = ((src.height as f64 * ratio + 0.5) as i32).max(1);
    scale_image(src, target_width, nh)
}

// ---------- encode ----------

fn write_png_file(img: &RgbaImage, path: &Path) -> ProcResult<()> {
    let f = fs::File::create(path).map_err(|e| format!("임시 PNG 생성 실패: {}", e))?;
    let w = std::io::BufWriter::new(f);
    let encoder = image::codecs::png::PngEncoder::new(w);
    encoder
        .write_image(&img.pixels, img.width as u32, img.height as u32, image::ExtendedColorType::Rgba8)
        .map_err(|e| format!("임시 PNG 인코딩 실패: {}", e))?;
    Ok(())
}

pub fn write_webp(img: &RgbaImage, out_path: &str, quality: i32) -> ProcResult<()> {
    let temp_png = make_temp_path(".png")?;
    write_png_file(img, &temp_png)?;

    let argv = vec![
        bin_path("CWEBP_PATH", "cwebp"),
        "-quiet".to_string(),
        "-q".to_string(),
        quality.to_string(),
        temp_png.to_string_lossy().into_owned(),
        "-o".to_string(),
        out_path.to_string(),
    ];
    let res = run_subprocess(&argv, subprocess_timeout_seconds());
    let result = if res.spawn_failed {
        Err(format!("cwebp 실행 파일을 찾을 수 없음({}). CWEBP_PATH 환경변수로 경로를 지정하세요.", argv[0]))
    } else if res.timed_out {
        Err("cwebp 실행이 타임아웃 초과로 강제 종료됨".to_string())
    } else if res.exit_code != 0 || !Path::new(out_path).exists() {
        Err(format!("cwebp 인코딩 실패(exit={}): {:.150}", res.exit_code, res.output))
    } else {
        Ok(())
    };
    let _ = fs::remove_file(&temp_png);
    result
}

pub fn write_avif(img: &RgbaImage, out_path: &str, quality: i32) -> ProcResult<()> {
    let temp_png = make_temp_path(".png")?;
    write_png_file(img, &temp_png)?;

    let argv = vec![
        bin_path("AVIFENC_PATH", "avifenc"),
        "-q".to_string(),
        quality.to_string(),
        temp_png.to_string_lossy().into_owned(),
        out_path.to_string(),
    ];
    let res = run_subprocess(&argv, subprocess_timeout_seconds());
    let result = if res.spawn_failed {
        Err(format!("avifenc 실행 파일을 찾을 수 없음({}). AVIFENC_PATH 환경변수로 경로를 지정하세요.", argv[0]))
    } else if res.timed_out {
        Err("avifenc 실행이 타임아웃 초과로 강제 종료됨".to_string())
    } else if res.exit_code != 0 || !Path::new(out_path).exists() {
        Err(format!("avifenc 인코딩 실패(exit={}): {:.150}", res.exit_code, res.output))
    } else {
        Ok(())
    };
    let _ = fs::remove_file(&temp_png);
    result
}

pub fn convert_gif_to_webp(in_path: &str, out_path: &str, quality: i32) -> ProcResult<()> {
    let argv = vec![
        bin_path("GIF2WEBP_PATH", "gif2webp"),
        "-quiet".to_string(),
        "-q".to_string(),
        quality.to_string(),
        in_path.to_string(),
        "-o".to_string(),
        out_path.to_string(),
    ];
    let res = run_subprocess(&argv, subprocess_timeout_seconds());
    if res.spawn_failed {
        Err(format!("gif2webp 실행 파일을 찾을 수 없음({}). GIF2WEBP_PATH 환경변수로 경로를 지정하세요.", argv[0]))
    } else if res.timed_out {
        Err("gif2webp 실행이 타임아웃 초과로 강제 종료됨".to_string())
    } else if res.exit_code != 0 || !Path::new(out_path).exists() {
        Err(format!("gif2webp 변환 실패(exit={}): {:.150}", res.exit_code, res.output))
    } else {
        Ok(())
    }
}

// ---------- analysis ----------

pub fn compute_dhash(img: &RgbaImage) -> u64 {
    let (w, h) = (9i32, 8i32);
    let small = scale_image(img, w, h);
    let mut hash: u64 = 0;
    let mut bit = 0u32;
    for y in 0..h {
        for x in 0..w - 1 {
            let pl = ((y as usize) * (w as usize) + x as usize) * 4;
            let pr = ((y as usize) * (w as usize) + x as usize + 1) * 4;
            let gl = (small.pixels[pl] as u32 + small.pixels[pl + 1] as u32 + small.pixels[pl + 2] as u32) / 3;
            let gr = (small.pixels[pr] as u32 + small.pixels[pr + 1] as u32 + small.pixels[pr + 2] as u32) / 3;
            if gl < gr {
                hash |= 1u64 << bit;
            }
            bit += 1;
        }
    }
    hash
}

pub fn compute_grayscale32_base64(img: &RgbaImage) -> String {
    let small = scale_image(img, 32, 32);
    let mut out = Vec::with_capacity(32 * 32);
    for i in 0..(32 * 32) {
        let p = i * 4;
        let a = small.pixels[p + 3] as f32 / 255.0;
        let r = small.pixels[p] as f32 * a + 255.0 * (1.0 - a);
        let g = small.pixels[p + 1] as f32 * a + 255.0 * (1.0 - a);
        let b = small.pixels[p + 2] as f32 * a + 255.0 * (1.0 - a);
        let luma = 0.299 * r + 0.587 * g + 0.114 * b;
        out.push(luma.round().clamp(0.0, 255.0) as u8);
    }
    base64_encode(&out)
}

pub fn compute_dominant_color(img: &RgbaImage) -> ProcResult<String> {
    if img.pixels.is_empty() || img.width <= 0 || img.height <= 0 {
        return Err("이미지가 비어 있음".to_string());
    }
    let small = scale_image(img, 32, 32);
    let mut sum_r = 0f64;
    let mut sum_g = 0f64;
    let mut sum_b = 0f64;
    let mut counted = 0i64;
    for i in 0..(small.width as usize * small.height as usize) {
        let p = i * 4;
        if small.pixels[p + 3] < 16 {
            continue;
        }
        sum_r += small.pixels[p] as f64;
        sum_g += small.pixels[p + 1] as f64;
        sum_b += small.pixels[p + 2] as f64;
        counted += 1;
    }
    let (r, g, b) = if counted == 0 {
        (128, 128, 128)
    } else {
        (
            (sum_r / counted as f64 + 0.5) as i32,
            (sum_g / counted as f64 + 0.5) as i32,
            (sum_b / counted as f64 + 0.5) as i32,
        )
    };
    Ok(format!("#{:02x}{:02x}{:02x}", r, g, b))
}

const B64_TABLE: &[u8; 64] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

fn base64_encode(data: &[u8]) -> String {
    let mut out = String::with_capacity(4 * ((data.len() + 2) / 3));
    let mut chunks = data.chunks_exact(3);
    for chunk in &mut chunks {
        let n = ((chunk[0] as u32) << 16) | ((chunk[1] as u32) << 8) | chunk[2] as u32;
        out.push(B64_TABLE[((n >> 18) & 0x3F) as usize] as char);
        out.push(B64_TABLE[((n >> 12) & 0x3F) as usize] as char);
        out.push(B64_TABLE[((n >> 6) & 0x3F) as usize] as char);
        out.push(B64_TABLE[(n & 0x3F) as usize] as char);
    }
    let rem = chunks.remainder();
    if rem.len() == 1 {
        let n = (rem[0] as u32) << 16;
        out.push(B64_TABLE[((n >> 18) & 0x3F) as usize] as char);
        out.push(B64_TABLE[((n >> 12) & 0x3F) as usize] as char);
        out.push('=');
        out.push('=');
    } else if rem.len() == 2 {
        let n = ((rem[0] as u32) << 16) | ((rem[1] as u32) << 8);
        out.push(B64_TABLE[((n >> 18) & 0x3F) as usize] as char);
        out.push(B64_TABLE[((n >> 12) & 0x3F) as usize] as char);
        out.push(B64_TABLE[((n >> 6) & 0x3F) as usize] as char);
        out.push('=');
    }
    out
}

pub fn build_lqip_data_uri(img: &RgbaImage, box_size: i32, quality: i32) -> ProcResult<String> {
    let small = resize_to_max(img, box_size);
    let temp_webp = make_temp_path(".webp")?;
    let temp_webp_str = temp_webp.to_string_lossy().into_owned();
    let result = write_webp(&small, &temp_webp_str, quality);
    if let Err(e) = result {
        let _ = fs::remove_file(&temp_webp);
        return Err(e);
    }
    let buf = fs::read(&temp_webp).map_err(|_| "LQIP webp 파일을 읽을 수 없음".to_string())?;
    let _ = fs::remove_file(&temp_webp);
    let b64 = base64_encode(&buf);
    Ok(format!("data:image/webp;base64,{}", b64))
}

pub fn build_clip_sprite(
    video_path: &str,
    out_path: &str,
    frame_count_req: i32,
    frame_width: i32,
    duration_seconds: f64,
    quality: i32,
) -> ProcResult<SpriteResult> {
    let frame_count = frame_count_req.max(1);
    let duration_seconds = if duration_seconds <= 0.0 { 1.0 } else { duration_seconds };

    let frame_dir = std::env::temp_dir().join(format!(
        "pastellive_sprite_{}_{}",
        std::process::id(),
        SystemTime::now().duration_since(UNIX_EPOCH).map(|d| d.as_nanos()).unwrap_or(0)
    ));
    fs::create_dir_all(&frame_dir).map_err(|_| "임시 프레임 디렉터리 생성 실패".to_string())?;

    let cleanup = |dir: &Path| {
        if let Ok(rd) = fs::read_dir(dir) {
            for entry in rd.flatten() {
                let _ = fs::remove_file(entry.path());
            }
        }
        let _ = fs::remove_dir(dir);
    };

    let fps = frame_count as f64 / duration_seconds;
    let out_pattern = frame_dir.join("frame_%03d.png").to_string_lossy().into_owned();
    let argv = vec![
        bin_path("FFMPEG_PATH", "ffmpeg"),
        "-y".to_string(),
        "-i".to_string(),
        video_path.to_string(),
        "-vf".to_string(),
        format!("fps={}", fps),
        "-frames:v".to_string(),
        frame_count.to_string(),
        out_pattern,
    ];
    let res = run_subprocess(&argv, subprocess_timeout_seconds());
    if res.spawn_failed {
        cleanup(&frame_dir);
        return Err(format!("ffmpeg 실행 파일을 찾을 수 없음({}). FFMPEG_PATH 환경변수로 경로를 지정하세요.", argv[0]));
    } else if res.timed_out {
        cleanup(&frame_dir);
        return Err("ffmpeg 실행이 타임아웃 초과로 강제 종료됨".to_string());
    } else if res.exit_code != 0 {
        cleanup(&frame_dir);
        return Err(format!("ffmpeg 프레임 추출 실패(exit={}): {:.150}", res.exit_code, res.output));
    }

    let mut names: Vec<String> = fs::read_dir(&frame_dir)
        .map_err(|_| "프레임 디렉터리를 열 수 없음".to_string())?
        .flatten()
        .filter_map(|e| {
            let n = e.file_name().to_string_lossy().into_owned();
            if has_suffix_ci(&n, ".png") { Some(n) } else { None }
        })
        .collect();
    if names.is_empty() {
        cleanup(&frame_dir);
        return Err("ffmpeg이 프레임을 하나도 뽑지 못함".to_string());
    }
    names.sort();

    let mut frames: Vec<RgbaImage> = Vec::new();
    let mut frame_h = 0i32;
    for name in &names {
        let full = frame_dir.join(name);
        if let Ok(img) = load_png_file(&full) {
            let resized = resize_to_width(&img, frame_width);
            if frame_h == 0 {
                frame_h = resized.height;
            }
            frames.push(resized);
        }
    }
    if frames.is_empty() {
        cleanup(&frame_dir);
        return Err("추출된 프레임을 읽을 수 없음".to_string());
    }

    let loaded = frames.len() as i32;
    let sprite_w = frame_width * loaded;
    let mut sprite_pixels = vec![0u8; sprite_w as usize * frame_h as usize * 4];
    for (i, frame) in frames.iter().enumerate() {
        let copy_h = frame_h.min(frame.height) as usize;
        for y in 0..copy_h {
            let dst_start = (y * sprite_w as usize + i * frame_width as usize) * 4;
            let src_start = y * frame.width as usize * 4;
            let len = frame_width as usize * 4;
            sprite_pixels[dst_start..dst_start + len].copy_from_slice(&frame.pixels[src_start..src_start + len]);
        }
    }
    let sprite = RgbaImage { width: sprite_w, height: frame_h, pixels: sprite_pixels };

    let write_result = write_webp(&sprite, out_path, quality);
    cleanup(&frame_dir);
    write_result?;

    Ok(SpriteResult { frame_count: loaded, frame_width, frame_height: frame_h })
}
