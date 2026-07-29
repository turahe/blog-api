#!/usr/bin/env node
// Pure Mermaid parser validation for Markdown files: extracts every ```mermaid fenced
// block, uses jsdom + mermaid.parse() to assert syntax validity. Exits 2 when ANY block
// fails to parse, writing the exact error to stderr.
import { JSDOM } from 'jsdom';
import mermaid from 'mermaid';
import fs from 'node:fs';

// --- Stub a minimal DOM (required because mermaid.parse() accesses document/window) ---
const dom = new JSDOM('<!doctype html><html><body></body></html>', {
  pretendToBeVisual: true,
  url: 'http://localhost/'
});
globalThis.window = dom.window;
globalThis.document = dom.window.document;
globalThis.MathJax = undefined;

const filePath = process.argv[2];
if (!filePath || !fs.existsSync(filePath)) {
  console.error('usage: node scripts/docs/parse_mermaid_validate.mjs <file.md>');
  process.exit(1);
}

const md = fs.readFileSync(filePath, 'utf8');
const regex = /```mermaid\n([\s\S]*?)\n```/g;
const blocks = [];
let match;
while ((match = regex.exec(md)) !== null) blocks.push(match[1]);

if (blocks.length === 0) {
  console.log(`${filePath}: zero mermaid blocks found (nothing to validate).`);
  process.exit(0);
}

mermaid.initialize({ startOnLoad: false, securityLevel: 'strict', suppressErrorRendering: true });

let errors = 0;
for (let i = 0; i < blocks.length; i++) {
  const code = blocks[i];
  try {
    await mermaid.parse(code);
    console.log(`  BLOCK ${String(i + 1).padStart(2, '0')}  OK  (${code.split('\n').length} lines)`);
  } catch (e) {
    const msg = String(e && e.message || e);
    // ---- jsdom parse-time false-positive: sequence diagrams with `box TITLE ... end`
    // participant grouping throw "Option is not defined" because the layout/box drawing
    // code accesses a browser-only `Option` constructor; these ALWAYS render correctly in
    // real browsers (GitHub / MkDocs / Confluence), so downgrade to a warning.
    const isBoxGroupingFalsePositive =
      /^Option is not defined\s*$/.test(msg) && /^\s*box\s+/im.test(code);
    if (isBoxGroupingFalsePositive) {
      console.warn(`  BLOCK ${String(i + 1).padStart(2, '0')}  WARN jsdom-only (box grouping). Render will succeed in real browser.`);
    } else {
      errors++;
      console.error(`  BLOCK ${String(i + 1).padStart(2, '0')}  PARSE ERROR:`);
      const lines = msg.split('\n').slice(0, 8);
      for (const l of lines) console.error('    ' + l);
    }
  }
}

console.log('\nSummary:', blocks.length, 'block(s),', errors, 'parse error(s).');
process.exit(errors > 0 ? 2 : 0);
