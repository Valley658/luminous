#!/usr/bin/env node
// 사이트 JS 난독화 빌드
//   원본(사람이 고치는 코드): frontend/js/*.js   ← GitHub에 올라가지 않음(.gitignore)
//   결과(사이트·GitHub 공개):  static/js/*.js     ← 난독화된 코드만 공개
// 원본을 고친 뒤에는 scripts\JS빌드.bat (또는 빌드배포.bat) 을 실행하면 다시 만들어진다.
const fs = require("fs");
const path = require("path");
const JavaScriptObfuscator = require("javascript-obfuscator");

const ROOT = path.join(__dirname, "..");
const SRC_DIR = path.join(ROOT, "frontend", "js");
const OUT_DIR = path.join(ROOT, "static", "js");
const FILES = ["script.js", "watch.js", "m.js", "imagepreview.js"];

// 템플릿(HTML) 안의 인라인 스크립트가 script.js 의 전역 함수(switchTab 등)를 직접 부르므로
// 전역 이름과 객체 속성 이름은 바꾸지 않는다(renameGlobals/renameProperties=false).
// 그 외에는 javascript-obfuscator 에서 동작을 바꾸지 않는 범위의 강한 옵션을 쓴다.
const OPTIONS = {
  target: "browser",
  compact: true,
  simplify: true,
  identifierNamesGenerator: "mangled-shuffled",
  renameGlobals: false,
  renameProperties: false,
  controlFlowFlattening: true,
  controlFlowFlatteningThreshold: 0.4,
  deadCodeInjection: true,
  deadCodeInjectionThreshold: 0.15,
  numbersToExpressions: true,
  splitStrings: true,
  splitStringsChunkLength: 12,
  stringArray: true,
  stringArrayThreshold: 0.9,
  stringArrayEncoding: ["rc4"],
  stringArrayRotate: true,
  stringArrayShuffle: true,
  stringArrayIndexShift: true,
  stringArrayCallsTransform: true,
  stringArrayCallsTransformThreshold: 0.5,
  stringArrayWrappersCount: 1,
  stringArrayWrappersChainedCalls: true,
  stringArrayWrappersParametersMaxCount: 4,
  stringArrayWrappersType: "function",
  transformObjectKeys: true,
  selfDefending: true,
  debugProtection: false,
  disableConsoleOutput: false,
  unicodeEscapeSequence: false,
  sourceMap: false,
};

function sleep(ms) {
  Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ms);
}

// Windows에서는 nginx(open_file_cache)나 백신이 static/js 파일을 열어 둔 동안
// rename으로 덮어쓰기가 EPERM/EBUSY로 막힐 수 있다(방문자가 많이 받는 script.js가 특히 그렇다).
// 잠깐씩 기다리며 다시 시도하고, 끝까지 안 되면 파일 내용을 직접 덮어쓴다.
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

function main() {
  let failed = 0;
  for (const name of FILES) {
    const srcPath = path.join(SRC_DIR, name);
    if (!fs.existsSync(srcPath)) {
      console.warn(`[skip] ${srcPath} 없음`);
      continue;
    }
    const code = fs.readFileSync(srcPath, "utf8");
    try {
      const seed = Math.floor(Math.random() * 2 ** 31); // 빌드할 때마다 결과가 달라짐
      // 난독화가 새로 만드는 전역 이름(문자열 표 함수 등)이 페이지의 다른 스크립트와
      // 겹치지 않게 파일마다 고유 접두사를 붙인다.
      const identifiersPrefix = "_l" + name.replace(/\W/g, "").slice(0, 3) + "_";
      const out = JavaScriptObfuscator.obfuscate(code, { ...OPTIONS, seed, identifiersPrefix }).getObfuscatedCode();
      const outPath = path.join(OUT_DIR, name);
      fs.writeFileSync(outPath + ".tmp", out, "utf8");
      replaceFile(outPath + ".tmp", outPath, out);
      console.log(`[ok] ${name}  ${code.length} -> ${out.length} bytes`);
    } catch (e) {
      failed++;
      console.error(`[실패] ${name}: ${e.message}`);
    }
  }
  if (failed) process.exit(1);
}

main();
