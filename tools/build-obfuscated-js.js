#!/usr/bin/env node
const fs = require("fs");
const path = require("path");
const JavaScriptObfuscator = require("javascript-obfuscator");

const ROOT = path.join(__dirname, "..");
const SRC_DIR = path.join(ROOT, "static", "js");
const DIST_DIR = path.join(SRC_DIR, "dist");

const FILES = ["script.js", "watch.js", "m.js"];

const OPTIONS = {
  compact: true,
  controlFlowFlattening: true,
  controlFlowFlatteningThreshold: 0.5,
  deadCodeInjection: true,
  deadCodeInjectionThreshold: 0.2,
  stringArray: true,
  stringArrayEncoding: ["base64"],
  stringArrayThreshold: 0.75,
  renameGlobals: false,
  renameProperties: false,
  selfDefending: false,
  identifierNamesGenerator: "hexadecimal",
  numbersToExpressions: true,
  simplify: true,
  splitStrings: true,
  splitStringsChunkLength: 10,
  target: "browser",
};

function main() {
  if (!fs.existsSync(DIST_DIR)) fs.mkdirSync(DIST_DIR, { recursive: true });

  for (const name of FILES) {
    const srcPath = path.join(SRC_DIR, name);
    if (!fs.existsSync(srcPath)) {
      console.warn(`[skip] ${srcPath} not found`);
      continue;
    }
    const code = fs.readFileSync(srcPath, "utf8");
    const result = JavaScriptObfuscator.obfuscate(code, OPTIONS).getObfuscatedCode();
    const outPath = path.join(DIST_DIR, name);
    fs.writeFileSync(outPath, result, "utf8");
    console.log(`[ok] ${srcPath} -> ${outPath} (${code.length} -> ${result.length} bytes)`);
  }
}

main();
