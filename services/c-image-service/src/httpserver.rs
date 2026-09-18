use std::io::{Read, Write};
use std::net::{TcpListener, TcpStream};
use std::sync::{Arc, Condvar, Mutex};
use std::thread;

#[allow(dead_code)]
pub struct HttpRequest {
    pub method: String,
    pub path: String,
    pub body: Vec<u8>,
}

pub type Handler = fn(&HttpRequest) -> (i32, String);

pub struct Route {
    pub method: &'static str,
    pub path: &'static str,
    pub handler: Handler,
}

fn log_line(msg: &str) {
    let now = std::time::SystemTime::now();
    let datetime: chrono_lite::Local = chrono_lite::Local::from(now);
    println!("[{}] {}", datetime, msg);
}

// Minimal local time formatter without external crates.
mod chrono_lite {
    use std::fmt;
    use std::time::SystemTime;

    pub struct Local {
        secs_since_epoch: i64,
    }

    impl From<SystemTime> for Local {
        fn from(t: SystemTime) -> Self {
            let dur = t.duration_since(std::time::UNIX_EPOCH).unwrap_or_default();
            Local { secs_since_epoch: dur.as_secs() as i64 }
        }
    }

    impl fmt::Display for Local {
        fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
            // UTC-based formatting (no external tz DB available); good enough for log timestamps.
            let days_since_epoch = self.secs_since_epoch.div_euclid(86400);
            let secs_of_day = self.secs_since_epoch.rem_euclid(86400);
            let (h, m, s) = (secs_of_day / 3600, (secs_of_day % 3600) / 60, secs_of_day % 60);

            // civil_from_days algorithm (Howard Hinnant).
            let z = days_since_epoch + 719468;
            let era = if z >= 0 { z } else { z - 146096 } / 146097;
            let doe = (z - era * 146097) as i64;
            let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
            let y = yoe + era * 400;
            let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
            let mp = (5 * doy + 2) / 153;
            let d = doy - (153 * mp + 2) / 5 + 1;
            let m2 = if mp < 10 { mp + 3 } else { mp - 9 };
            let y2 = if m2 <= 2 { y + 1 } else { y };

            write!(f, "{:04}-{:02}-{:02} {:02}:{:02}:{:02}", y2, m2, d, h, m, s)
        }
    }
}

struct Semaphore {
    count: Mutex<i32>,
    cond: Condvar,
}

impl Semaphore {
    fn new(n: i32) -> Self {
        Semaphore { count: Mutex::new(n), cond: Condvar::new() }
    }
    fn acquire(&self) {
        let mut c = self.count.lock().unwrap();
        while *c <= 0 {
            c = self.cond.wait(c).unwrap();
        }
        *c -= 1;
    }
    fn release(&self) {
        let mut c = self.count.lock().unwrap();
        *c += 1;
        self.cond.notify_one();
    }
}

fn status_text(status: i32) -> &'static str {
    match status {
        200 => "OK",
        204 => "No Content",
        400 => "Bad Request",
        403 => "Forbidden",
        404 => "Not Found",
        405 => "Method Not Allowed",
        413 => "Payload Too Large",
        500 => "Internal Server Error",
        _ => "OK",
    }
}

fn send_response(stream: &mut TcpStream, status: i32, body: &str) {
    let header = format!(
        "HTTP/1.1 {} {}\r\nContent-Type: application/json; charset=utf-8\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
        status,
        status_text(status),
        body.len()
    );
    let _ = stream.write_all(header.as_bytes());
    if !body.is_empty() {
        let _ = stream.write_all(body.as_bytes());
    }
    let _ = stream.flush();
}

fn read_until_headers_end(stream: &mut TcpStream) -> Option<Vec<u8>> {
    let mut buf = Vec::with_capacity(4096);
    let mut byte = [0u8; 1];
    loop {
        match stream.read(&mut byte) {
            Ok(0) => return None,
            Ok(_) => {
                buf.push(byte[0]);
                let n = buf.len();
                if n >= 4 && &buf[n - 4..] == b"\r\n\r\n" {
                    return Some(buf);
                }
                if n > 64 * 1024 {
                    return None;
                }
            }
            Err(_) => return None,
        }
    }
}

fn read_exact_body(stream: &mut TcpStream, n: usize) -> Option<Vec<u8>> {
    let mut buf = vec![0u8; n];
    stream.read_exact(&mut buf).ok()?;
    Some(buf)
}

fn parse_content_length(header_text: &str) -> i64 {
    for line in header_text.split("\r\n") {
        let lower = line.to_ascii_lowercase();
        if let Some(rest) = lower.strip_prefix("content-length:") {
            if let Ok(v) = rest.trim().parse::<i64>() {
                return v;
            }
        }
    }
    0
}

fn handle_connection(mut stream: TcpStream, routes: &'static [Route], max_body: usize) {
    let header_buf = match read_until_headers_end(&mut stream) {
        Some(b) => b,
        None => return,
    };
    let header_text = String::from_utf8_lossy(&header_buf);
    let mut parts = header_text.splitn(3, ' ');
    let method = parts.next().unwrap_or("").trim().to_string();
    let path_full = parts.next().unwrap_or("").trim().to_string();
    let path = path_full.split('?').next().unwrap_or("").to_string();
    if method.is_empty() || path.is_empty() {
        return;
    }

    let mut content_length = parse_content_length(&header_text);
    if content_length < 0 {
        content_length = 0;
    }
    if content_length as usize > max_body {
        send_response(&mut stream, 413, "{\"success\":false,\"error\":\"요청 본문이 너무 큼\"}");
        return;
    }

    let body = if content_length > 0 {
        match read_exact_body(&mut stream, content_length as usize) {
            Some(b) => b,
            None => return,
        }
    } else {
        Vec::new()
    };

    let req = HttpRequest { method: method.clone(), path: path.clone(), body };

    let mut matched_handler: Option<Handler> = None;
    let mut path_exists = false;
    for r in routes {
        if r.path == path {
            path_exists = true;
            if r.method.eq_ignore_ascii_case(&method) {
                matched_handler = Some(r.handler);
                break;
            }
        }
    }

    let (status, resp_body) = if let Some(h) = matched_handler {
        h(&req)
    } else if path_exists {
        (405, "{\"success\":false,\"error\":\"허용되지 않은 메서드\"}".to_string())
    } else {
        (404, "{\"success\":false,\"error\":\"찾을 수 없음\"}".to_string())
    };

    send_response(&mut stream, status, &resp_body);
}

pub fn run(bind_addr: &str, port: u16, routes: &'static [Route], max_request_body_bytes: usize, max_concurrency: i32) {
    let listener = match TcpListener::bind((bind_addr, port)) {
        Ok(l) => l,
        Err(e) => {
            log_line(&format!("bind() 실패(포트 {} 사용 중이거나 권한 없음): {}", port, e));
            std::process::exit(1);
        }
    };

    log_line(&format!("pastellive Rust 이미지 처리 서비스가 {}:{} 에서 시작되었습니다.", bind_addr, port));

    let sem = Arc::new(Semaphore::new(max_concurrency));

    for incoming in listener.incoming() {
        let stream = match incoming {
            Ok(s) => s,
            Err(_) => continue,
        };
        let sem = Arc::clone(&sem);
        sem.acquire();
        thread::spawn(move || {
            handle_connection(stream, routes, max_request_body_bytes);
            sem.release();
        });
    }
}

pub fn log(msg: &str) {
    log_line(msg);
}
