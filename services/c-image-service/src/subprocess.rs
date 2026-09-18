use std::io::Read;
use std::process::{Child, Command, Stdio};
use std::sync::{Condvar, Mutex, OnceLock};
use std::thread;
use std::time::{Duration, Instant};

pub struct SubprocessResult {
    pub spawn_failed: bool,
    pub timed_out: bool,
    pub exit_code: i32,
    pub output: String,
}

struct Limiter {
    running: Mutex<i32>,
    cond: Condvar,
    limit: Mutex<i32>,
}

static LIMITER: OnceLock<Limiter> = OnceLock::new();

fn limiter() -> &'static Limiter {
    LIMITER.get_or_init(|| Limiter {
        running: Mutex::new(0),
        cond: Condvar::new(),
        limit: Mutex::new(0),
    })
}

pub fn set_max_concurrent(n: i32) {
    let l = limiter();
    let mut lim = l.limit.lock().unwrap();
    *lim = if n > 0 { n } else { 0 };
}

fn slot_acquire() {
    let l = limiter();
    let mut running = l.running.lock().unwrap();
    loop {
        let lim = *l.limit.lock().unwrap();
        if lim <= 0 || *running < lim {
            break;
        }
        running = l.cond.wait(running).unwrap();
    }
    *running += 1;
}

fn slot_release() {
    let l = limiter();
    let mut running = l.running.lock().unwrap();
    if *running > 0 {
        *running -= 1;
    }
    l.cond.notify_one();
}

fn read_all_to_string(mut r: impl Read) -> String {
    let mut buf = Vec::new();
    let _ = r.read_to_end(&mut buf);
    String::from_utf8_lossy(&buf).into_owned()
}

fn run_impl(argv: &[String], timeout_seconds: u64) -> SubprocessResult {
    let timeout_seconds = if timeout_seconds == 0 { 60 } else { timeout_seconds };
    if argv.is_empty() {
        return SubprocessResult { spawn_failed: true, timed_out: false, exit_code: -1, output: String::new() };
    }

    let mut cmd = Command::new(&argv[0]);
    cmd.args(&argv[1..]);
    cmd.stdin(Stdio::null());
    cmd.stdout(Stdio::piped());
    cmd.stderr(Stdio::piped());

    let mut child: Child = match cmd.spawn() {
        Ok(c) => c,
        Err(_) => {
            return SubprocessResult {
                spawn_failed: true,
                timed_out: false,
                exit_code: -1,
                output: format!("실행 파일을 찾을 수 없음: {}", argv[0]),
            };
        }
    };

    let stdout = child.stdout.take();
    let stderr = child.stderr.take();
    let out_handle = thread::spawn(move || stdout.map(read_all_to_string).unwrap_or_default());
    let err_handle = thread::spawn(move || stderr.map(read_all_to_string).unwrap_or_default());

    let deadline = Instant::now() + Duration::from_secs(timeout_seconds);
    let mut timed_out = false;
    let exit_status;
    loop {
        match child.try_wait() {
            Ok(Some(status)) => {
                exit_status = Some(status);
                break;
            }
            Ok(None) => {
                if Instant::now() >= deadline {
                    let _ = child.kill();
                    let _ = child.wait();
                    timed_out = true;
                    exit_status = None;
                    break;
                }
                thread::sleep(Duration::from_millis(50));
            }
            Err(_) => {
                exit_status = None;
                break;
            }
        }
    }

    let out_s = out_handle.join().unwrap_or_default();
    let err_s = err_handle.join().unwrap_or_default();
    let mut combined = out_s;
    combined.push_str(&err_s);

    let exit_code = exit_status.and_then(|s| s.code()).unwrap_or(-1);

    SubprocessResult { spawn_failed: false, timed_out, exit_code, output: combined }
}

pub fn run_subprocess(argv: &[String], timeout_seconds: u64) -> SubprocessResult {
    slot_acquire();
    let r = run_impl(argv, timeout_seconds);
    slot_release();
    r
}
