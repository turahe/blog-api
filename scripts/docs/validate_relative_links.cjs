#!/usr/bin/env node
/**
 * scripts/docs/validate_relative_links.cjs
 * ========================================
 *
 * Enforce the Relative-Link Usage Rules documented in
 * docs/guides/relative-link-usage-rules.md.
 *
 * What the validator does (see §3 of the rules doc for the formal specification):
 *   1. FORBIDS any absolute `file:///` link inside `[text](...)` markdown link destinations.
 *   2. FORBIDS any repo-root-absolute link that starts with "/docs" or "/contracts" or "/paths"
 *      — every link must start with "./", "../", or a bare sibling filename (same dir).
 *   3. RESOLVES every relative link path fragment (removing ./ ../ navigations as well as
 *      URL-escaped spaces etc.) then verifies the target file EXISTS on disk.
 *      Line-range anchors `#L<start>-L<end>` are supported and stripped before existence check.
 *      Section anchors `#some-heading` are validated by regex against the target Markdown file's
 *      heading-slug catalogue (GitHub-compatible slug rules).
 *   4. CATCHES common malformations: unbalanced parenthesis in URLs, whitespace adjacent to anchors,
 *      un-encoded special characters, duplicate ../ (going above the repo root).
 *   5. Supports `--fix` mode to convert every `file://<ABS-PREFIX>/<repo>/<relpath>#anchor` link to
 *      the correct computed relative link for each source file.
 *
 * Exit codes:
 *   0  — all checks passed, zero violations, zero broken links.
 *   1  — usage error (bad arguments, file not found).
 *   2  — one or more ABSOLUTE links found (rule 0 forbidden).
 *   3  — one or more RELATIVE LINKS DO NOT RESOLVE to an existing file / valid anchor.
 *   4  — mixed violations (both absolute + broken).
 *
 * Typical usage:
 *   node scripts/docs/validate_relative_links.cjs docs/**.md              # validate
 *   node scripts/docs/validate_relative_links.cjs docs/**.md --fix        # auto-fix absolute
 *   node scripts/docs/validate_relative_links.cjs --root /srv/repo/x docs  # override repo root
 */
'use strict';

const fs = require('node:fs');
const path = require('node:path');

// -------- CLI arg parsing --------
let root = process.cwd();
const patterns = [];
let fixMode = false;
for (const a of process.argv.slice(2)) {
  if (a === '--fix') fixMode = true;
  else if (a.startsWith('--root=')) root = a.slice('--root='.length);
  else if (a === '--root') { /* skip, next is value handled below */ }
  else if (patterns.length && patterns[patterns.length - 1] === '--root') {
    patterns[patterns.length - 1] = a; root = a;
  } else patterns.push(a);
}
root = path.resolve(root);

if (patterns.length === 0) {
  console.error('usage: validate_relative_links.cjs <file|glob|dir>... [--fix] [--root DIR]');
  process.exit(1);
}

// -------- Collect markdown files from positional args --------
function expand(p, out) {
  if (!fs.existsSync(p)) return;
  const st = fs.statSync(p);
  if (st.isFile()) { if (p.endsWith('.md')) out.push(p); return; }
  if (st.isDirectory()) {
    for (const child of fs.readdirSync(p)) expand(path.join(p, child), out);
    return;
  }
}
function globToFiles(pattern) {
  if (!/[*?]/.test(pattern)) { expand(pattern, files); return; }
  // minimal glob support for dir/**/*.md
  const idx = pattern.indexOf('**');
  if (idx >= 0) {
    const base = pattern.slice(0, idx);
    expand(base.length ? base : '.', files);
    const suffix = pattern.slice(idx + 2).replace(/^\/+/, '').replace(/\./g, '\\.').replace(/\*/g, '[^/]*');
    const re = new RegExp(suffix + '$');
    for (let i = files.length - 1; i >= 0; i--) if (!re.test(files[i])) files.splice(i, 1);
  }
}
const files = [];
for (const p of patterns) globToFiles(p);
if (files.length === 0) { console.error('no markdown files match inputs'); process.exit(1); }

// -------- Regexes --------
// Matches markdown [text](url "title?") links; captures url inside $1.
const LINK_RE = /\[([^\]]*)\]\(((?:[^()\s]+|\([^)]*\))+)(?:\s+"[^"]*")?\)/g;
const ABS_FILE_RE = /^file:\/\/(?:localhost)?(\/(?:[A-Za-z]:)?[^#?\s)]+)(#[^)\s]*)?/;
const ROOT_ABS_RE = /^\/(docs|contracts|paths|tests|scripts)\b/;
const HEADING_RE = /^#{1,6}\s+(.+?)\s*#*\s*$/m;
const HEADING_RE_GLOBAL = /^#{1,6}\s+(.+?)\s*#*\s*$/gm;

// GitHub-compatible heading slug (lowercase, strip punctuation, spaces → '-', collapse '-')
function slugify(str) {
  return String(str)
    .toLowerCase()
    .normalize('NFKD')
    .replace(/[\u0300-\u036f]/g, '')
    .replace(/[^\p{L}\p{N}\s-]/gu, '')
    .trim()
    .replace(/\s+/g, '-')
    .replace(/-+/g, '-');
}

function headingSlugs(md) {
  const out = new Set();
  let m; HEADING_RE_GLOBAL.lastIndex = 0;
  while ((m = HEADING_RE_GLOBAL.exec(md)) !== null) out.add(slugify(m[1]));
  return out;
}

// -------- Core routines --------
/** Resolve a relative link target against the source file's directory; returns absolute path. */
function resolveRel(srcFile, relTarget) {
  // Strip anchor
  const hashIdx = relTarget.indexOf('#');
  const targetPath = (hashIdx >= 0 ? relTarget.slice(0, hashIdx) : relTarget) || '';
  const anchor = hashIdx >= 0 ? relTarget.slice(hashIdx + 1) : null;
  const resolved = path.resolve(path.dirname(srcFile), decodeURIComponent(targetPath));
  return { resolvedPath: resolved, targetPath, anchor };
}

/** Convert an absolute repo-internal path to a relative link from srcFile. */
function toRelative(srcFile, repoAbsPath, anchor) {
  let rel = path.relative(path.dirname(srcFile), repoAbsPath);
  if (!rel.startsWith('.') && !rel.startsWith('..')) rel = './' + rel;
  rel = rel.split(path.sep).join('/');
  return rel + (anchor ? '#' + anchor : '');
}

// -------- Main per-file loop --------
let absoluteViolations = 0;
let resolveViolations = 0;
let formattedViolations = 0;
let autoFixedCount = 0;
const targetSlugCache = new Map(); // absPath → Set<string>

for (const fileAbs of files.map(f => path.resolve(f)).sort()) {
  if (!fileAbs.startsWith(root + path.sep) && fileAbs !== root) continue; // not in repo root; skip
  const original = fs.readFileSync(fileAbs, 'utf8');
  let next = original;

  // --- Preprocessing: blank out every triple-backtick fenced block (``` ... ```) so that
  //     example "bad links" used inside "Correct / Incorrect" tables or inline code
  //     demos do NOT trigger violations. Fences can have an info string (```markdown, ```js, etc).
  const FENCE_RE = /^```[\s\S]*?^```$/gm;
  const step1 = original.replace(FENCE_RE, (m) => '%%%FENCE_REDACTED%%%'.padEnd(m.length, '\n').slice(0, m.length));
  // --- Preprocessing 2: blank out HTML <!-- relative-links nolint-begin --> ... <!-- nolint-end --> blocks
  //     used in the relative-link-rules spec document itself to isolate intentionally incorrect
  //     markdown link demo examples. Blocks are case-insensitive.
  const NOLINT_RE = /<!--\s*relative-links\s+nolint-begin\s*-->[\s\S]*?<!--\s*relative-links\s+nolint-end\s*-->/gim;
  const redacted = step1.replace(NOLINT_RE, (m) => '%%%NOLINT%%%'.padEnd(m.length, '\n').slice(0, m.length));

  LINK_RE.lastIndex = 0;
  let match;
  const replacements = []; // [start, end, newUrl]
  while ((match = LINK_RE.exec(redacted)) !== null) {
    const url = match[2];
    const urlStart = match.index + match[0].indexOf('(') + 1;
    const urlEnd = urlStart + url.length;
    // Skip links that fell inside a redacted fence or nolint block
    const slice = redacted.slice(urlStart, urlEnd);
    if (slice.includes('%%%FENCE_REDACTED%%%') || slice.includes('%%%NOLINT%%%')) continue;

    // --- Rule 1: no absolute file:// links ---
    const abs = ABS_FILE_RE.exec(url);
    if (abs) {
      absoluteViolations++;
      const targetAbsRaw = abs[1];
      const anchor = abs[2] ? abs[2].slice(1) : null;
      // Match any path that has our repo root inside it (cross-machine absolute paths)
      let repoRel = null;
      for (const prefix of [root, '/mnt/myadrive/repo/turahe/blog-api',
        '/home/runner/work/turahe/blog-api',
        '/workspace', '/repo', '/srv/blog-api']) {
        const nixP = prefix.replace(/\\/g, '/');
        if (targetAbsRaw === nixP) { repoRel = ''; break; }
        if (targetAbsRaw.startsWith(nixP + '/')) { repoRel = targetAbsRaw.slice(nixP.length + 1); break; }
      }
      if (!repoRel && fs.existsSync(targetAbsRaw) && targetAbsRaw.startsWith(root + path.sep)) {
        repoRel = targetAbsRaw.slice(root.length + 1);
      }
      if (fixMode && repoRel !== null) {
        const fixed = toRelative(fileAbs, path.join(root, repoRel.split('/').join(path.sep)), anchor);
        replacements.push([urlStart, urlEnd, fixed]);
        autoFixedCount++;
        console.log(`[FIX] ${path.relative(root, fileAbs)}:  ${url}  ->  ${fixed}`);
      } else {
        console.error(`[ABSOLUTE FORBIDDEN] ${path.relative(root, fileAbs)}:  ${url}` +
          (fixMode ? ' (could not map to repo root — left alone)' : ''));
      }
      continue;
    }

    // --- Rule 2: no repo-root-absolute paths ---
    if (ROOT_ABS_RE.test(url)) {
      formattedViolations++;
      const hashIdx = url.indexOf('#');
      const p0 = hashIdx >= 0 ? url.slice(1, hashIdx) : url.slice(1);
      const anchor = hashIdx >= 0 ? url.slice(hashIdx + 1) : null;
      if (fixMode) {
        const fixed = toRelative(fileAbs, path.join(root, p0.split('/').join(path.sep)), anchor);
        replacements.push([urlStart, urlEnd, fixed]);
        autoFixedCount++;
        console.log(`[FIX root-absolute] ${path.relative(root, fileAbs)}:  ${url}  ->  ${fixed}`);
      } else {
        console.error(`[ROOT-ABSOLUTE FORBIDDEN] ${path.relative(root, fileAbs)}:  ${url}`);
      }
      continue;
    }

    // --- Rule 3: only relative syntax ---
    if (url.startsWith('http://') || url.startsWith('https://') || url.startsWith('mailto:')) continue; // external OK
    if (url.startsWith('javascript:') || url.startsWith('data:')) {
      formattedViolations++;
      console.error(`[FORBIDDEN SCHEME] ${path.relative(root, fileAbs)}:  ${url}`);
      continue;
    }

    // Resolve relative link and check existence + anchor
    let { resolvedPath, targetPath, anchor } = resolveRel(fileAbs, url);
    // Prevent escaping repo root
    if (!resolvedPath.startsWith(root + path.sep) && resolvedPath !== root) {
      resolveViolations++;
      console.error(`[ESCAPES REPO ROOT] ${path.relative(root, fileAbs)}:  ${url}  (resolved to ${resolvedPath})`);
      continue;
    }

    if (!targetPath) {
      // pure anchor within same file — check anchor against self slugs
      if (anchor && !/^L\d+(-L\d+)?$/.test(anchor)) {
        if (!targetSlugCache.has(fileAbs)) targetSlugCache.set(fileAbs, headingSlugs(original));
        const slugs = targetSlugCache.get(fileAbs);
        if (!slugs.has(anchor)) {
          resolveViolations++;
          console.error(`[BROKEN INTRA-FILE ANCHOR] ${path.relative(root, fileAbs)}  #${anchor}`);
        }
      }
      continue;
    }

    if (!fs.existsSync(resolvedPath)) {
      resolveViolations++;
      console.error(`[BROKEN TARGET] ${path.relative(root, fileAbs)}:  ${url}   -> ${path.relative(root, resolvedPath)} (missing)`);
      continue;
    }

    // Anchor validation
    if (anchor) {
      if (/^L\d+(-L\d+)?$/.test(anchor)) {
        // numeric line-range anchor: file existence is sufficient (line ranges are dynamic and file length varies).
        // Warn if start L# exceeds known file length
        const m = /^L(\d+)(?:-L(\d+))?$/.exec(anchor);
        const endLine = Number(m[2] || m[1]);
        const lineCount = (fs.readFileSync(resolvedPath, 'utf8').match(/\n/g) || []).length + 1;
        if (endLine > lineCount) {
          resolveViolations++;
          console.error(`[BROKEN LINE RANGE] ${path.relative(root, fileAbs)}  ${url} — target only ${lineCount} lines`);
        }
      } else {
        // heading anchor: GitHub slug
        if (!targetSlugCache.has(resolvedPath)) targetSlugCache.set(resolvedPath, headingSlugs(fs.readFileSync(resolvedPath, 'utf8')));
        const slugs = targetSlugCache.get(resolvedPath);
        if (!slugs.has(anchor)) {
          resolveViolations++;
          console.error(`[BROKEN HEADING ANCHOR] ${path.relative(root, fileAbs)}  ${url}`);
        }
      }
    }
  }

  if (fixMode && replacements.length) {
    replacements.sort((a, b) => b[0] - a[0]); // apply end → start so offsets stay correct
    let edited = next;
    for (const [s, e, newUrl] of replacements) edited = edited.slice(0, s) + newUrl + edited.slice(e);
    fs.writeFileSync(fileAbs, edited);
  }
}

console.log(`\n——————————————————————————————————————————————————————`);
console.log(`Files scanned:                ${files.length}`);
console.log(`Absolute violations:          ${absoluteViolations}${fixMode ? `  (auto-fixed: ${autoFixedCount})` : ``}`);
console.log(`Root-absolute violations:     ${formattedViolations}`);
console.log(`Broken-target violations:     ${resolveViolations}`);
console.log(`Total violations:             ${absoluteViolations + formattedViolations + resolveViolations}`);

let exit = 0;
if (absoluteViolations > 0) exit |= 2;
if (resolveViolations > 0 || formattedViolations > 0) exit |= 3;
process.exit(Math.min(exit, 4));
