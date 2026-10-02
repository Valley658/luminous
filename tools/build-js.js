#!/usr/bin/env node
// 사이트 JS 빌드 (압축/minify)
//   원본(사람이 고치는 코드): frontend/js/*.js   ← GitHub에 올라가지 않음(.gitignore)
//   결과(사이트·GitHub 공개):  static/js/*.js     ← 압축된 코드
// 원본을 고친 뒤에는 scripts\JS빌드.bat (또는 빌드배포.bat) 을 실행하면 다시 만들어진다.
//
// [2026-10-02] 예전에는 javascript-obfuscator로 난독화했는데, 파일이 원본의 약 6배로 커지고
// (script.js 90KB → 520KB) 문자열 복호화·흐름 뒤섞기 때문에 브라우저 실행도 느려져서
// 사이트가 느려지는 원인이 됐다. 지금은 terser로 공백·주석 제거, 지역 변수 이름 줄이기만 한다
// → 원본보다 작아지고 실행 속도는 원본과 같다.
const fs = require("fs");
const path = require("path");
const { minify } = require("terser");

const ROOT = path.join(__dirname, "..");
const SRC_DIR = path.join(ROOT, "frontend", "js");
const OUT_DIR = path.join(ROOT, "static", "js");
const FILES = ["script.js", "watch.js", "m.js", "imagepreview.js"];

// 템플릿(HTML)의 onclick 등 인라인 스크립트가 이 파일들의 전역 함수(switchTab 등)를 직접 부르므로
// 최상위(전역) 이름은 절대 바꾸지 않는다(toplevel: false). 함수 안의 지역 변수 이름만 줄인다.
const OPTIONS = {
  ecma: 2020,
  compress: { passes: 2, toplevel: false, keep_fargs: true, drop_debugger: true },
  mangle: { toplevel: false },
  format: { comments: false, ascii_only: false },
  sourceMap: false,
};

function sleep(ms) {
  Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ms);
}

// Windows에서는 nginx(open_file_cache)나 백신이 static/js 파일을 열어 둔 동안
// rename으로 덮어쓰기가 EPERM/EBUSY로 막힐 수 있다. 잠깐씩 기다리며 다시 시도하고,
// 끝까지 안 되면 파일 내용을 직접 덮어쓴다.
function replaceFile(tmpPath, outPath, content) {
  for (let i = 0; i < 10; i++) {
    try {
      fs.renameSync(tmpPath, outPath);
      return;
    } catch (e) {
      if (!["EPERM", "EBUSY", "EACCES"].includes(e.code)) throw e;
      sleep(300);
    }
  }
  fs.writeFileSync(outPath, content, "utf8");
  try { fs.unlinkSync(tmpPath); } catch (_) {}
}

async function main() {
  let failed = 0;
  for (const name of FILES) {
    const srcPath = path.join(SRC_DIR, name);
    if (!fs.existsSync(srcPath)) {
      console.warn(`[skip] ${srcPath} 없음`);
      continue;
    }
    const code = fs.readFileSync(srcPath, "utf8");
    try {
      const result = await minify({ [name]: code }, OPTIONS);
      const out = result.code + "\n";
      const outPath = path.join(OUT_DIR, name);
      fs.writeFileSync(outPath + ".tmp", out, "utf8");
      replaceFile(outPath + ".tmp", outPath, out);
      console.log(`[ok] ${name}  ${Buffer.byteLength(code)} -> ${Buffer.byteLength(out)} bytes`);
    } catch (e) {
      failed++;
      console.error(`[실패] ${name}: ${e.message}`);
    }
  }
  if (failed) process.exit(1);
}

main();
