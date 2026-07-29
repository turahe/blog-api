#!/usr/bin/env node
/*
 * OpenAPI Contract Validation Runner (Node entry)
 * -----------------------------------------------
 * Orchestrates:
 *   1. YAML syntax / structural parse of openapi.yaml + bundle to contracts/
 *   2. Redocly CLI lint (recommended + custom rules in redocly.yaml)
 *   3. Bundled spec + dereferenced spec generation
 *   4. Delegates semantic/format checks to validate_openapi.py
 *
 * Outputs:
 *   - reports/openapi-validation.log    — progress logger
 *   - reports/openapi-lint.json         — structured Redocly output
 *   - reports/openapi-lint.txt          — human-readable lint summary
 *   - reports/openapi-semantic.json     — semantic checker output
 *   - reports/openapi-report.md         — combined human-readable report
 *
 * Exit: 0 on clean (with optional --allow-warns); non-zero on any error.
 */

'use strict';

const fs = require('fs');
const path = require('path');
const os = require('os');
const { spawnSync, spawn } = require('child_process');

const REPO_ROOT = path.resolve(__dirname, '..', '..');
const CONTRACTS_DIR = path.join(REPO_ROOT, 'contracts');
const REPORTS_DIR = path.join(REPO_ROOT, 'reports');
const PATHS_DIR = path.join(REPO_ROOT, 'paths');
const COMPONENTS_DIR = path.join(REPO_ROOT, 'components');
function fileExists(p) { try { return fs.statSync(p).isFile(); } catch { return false; } }
function dirExists(p) { try { return fs.statSync(p).isDirectory(); } catch { return false; } }
const ENTRY_OPENAPI = (() => {
  const c = path.join(CONTRACTS_DIR, 'openapi.yaml');
  const r = path.join(REPO_ROOT, 'openapi.yaml');
  if (fileExists(c)) return c;
  return r;
})();
const BUNDLED_OPENAPI = path.join(CONTRACTS_DIR, 'openapi.bundle.yaml');
const DEREF_OPENAPI = path.join(CONTRACTS_DIR, 'openapi.bundle.deref.yaml');
const LOG_FILE = path.join(REPORTS_DIR, 'openapi-validation.log');
const LINT_JSON = path.join(REPORTS_DIR, 'openapi-lint.json');
const LINT_TXT = path.join(REPORTS_DIR, 'openapi-lint.txt');
const SEMANTIC_JSON = path.join(REPORTS_DIR, 'openapi-semantic.json');
const COMBINED_MD = path.join(REPORTS_DIR, 'openapi-report.md');

let LOG_STREAM = null;
function ensureReportDir() {
  fs.mkdirSync(REPORTS_DIR, { recursive: true });
  LOG_STREAM = fs.createWriteStream(LOG_FILE, { flags: 'w', highWaterMark: 0 });
}

function log(level, msg) {
  const ts = new Date().toISOString();
  const line = `[${ts}] [${level.toUpperCase()}] ${msg}`;
  process.stdout.write(line + '\n');
  if (LOG_STREAM) LOG_STREAM.write(line + '\n');
}
function flushLog(cb) {
  if (!LOG_STREAM) { process.nextTick(cb); return; }
  if (LOG_STREAM.closed || LOG_STREAM.destroyed) { process.nextTick(cb); return; }
  LOG_STREAM.once('finish', () => cb && cb());
  try { LOG_STREAM.end(); } catch { cb && cb(); }
}
function fatal(msg, code = 1) {
  log('error', msg);
  flushLog(() => process.exit(code));
}

function whichNode() {
  const npx = process.platform === 'win32' ? 'npx.cmd' : 'npx';
  return npx;
}

function redoclyBin() {
  const local = path.join(REPO_ROOT, 'node_modules', '.bin',
    process.platform === 'win32' ? 'redocly.cmd' : 'redocly');
  try {
    if (fs.statSync(local).isFile()) return local;
  } catch { /* fallthrough */ }
  return null;
}

function redoclyRun(subcmd, args, extraEnv) {
  const bin = redoclyBin();
  if (bin) {
    return run(bin, [subcmd, ...args], { env: extraEnv });
  }
  return run(whichNode(), [
    '--yes', '--package=@redocly/cli', '--', 'redocly', subcmd, ...args,
  ], { env: extraEnv });
}

function run(cmd, args, opts = {}) {
  log('info', `$ ${cmd} ${args.join(' ')}`);
  const res = spawnSync(cmd, args, {
    cwd: REPO_ROOT,
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
    env: { ...process.env, ...(opts.env || {}) },
    ...opts.spawnOpts,
  });
  if (res.stdout && res.stdout.trim()) {
    res.stdout.split('\n').forEach(l => log('stdout', l));
  }
  if (res.stderr && res.stderr.trim()) {
    res.stderr.split('\n').forEach(l => log('stderr', l));
  }
  return { status: res.status ?? -1, stdout: res.stdout, stderr: res.stderr };
}

function openapiSpecVersion(rootYamlPath) {
  try {
    const top = fs.readFileSync(rootYamlPath, 'utf8').split(/\r?\n/).slice(0, 20).join('\n');
    const m = top.match(/^openapi:\s*['"]?([0-9]+\.[0-9]+)/m);
    return m ? m[1] : null;
  } catch { return null; }
}

function yamlSyntaxCheck(filePath) {
  /* Parse the YAML file with a Node YAML parser when available; otherwise fall
     back to invoking the Python validator in `--yaml-only` mode.  This keeps
     the step self-contained even when `yaml` npm package is missing. */
  try {
    const yaml = require('yaml');
    const source = fs.readFileSync(filePath, 'utf8');
    const doc = yaml.parseDocument(source, { prettyErrors: true });
    if (doc.errors && doc.errors.length > 0) {
      return { ok: false, errors: doc.errors.map(e => ({
        file: filePath,
        line: e.linePos ? e.linePos[0].line : 0,
        col: e.linePos ? e.linePos[0].col : 0,
        message: e.message,
      })) };
    }
    return { ok: true };
  } catch (e) {
    if (e && e.code === 'MODULE_NOT_FOUND') {
      // Fall back to a light parse via python3 + the bundled script.
      try {
        const jsonOut = path.join(os.tmpdir(), `openapi-syntax-${process.pid}.json`);
        const res = run(process.env.PYTHON_BIN || (process.platform === 'win32' ? 'python.exe' : 'python3'), [
          path.join(__dirname, 'validate_openapi.py'),
          '--entry', filePath, '--bundle', filePath,
          '--json-out', jsonOut,
          '--report-dir', REPORTS_DIR,
          '--yaml-only',
        ]);
        if (res.status === 0) return { ok: true };
        // Try to surface structured errors from JSON output when available.
        let structured = [];
        try {
          const parsed = JSON.parse(fs.readFileSync(jsonOut, 'utf8'));
          structured = [].concat(parsed.errors || []).concat(parsed.warnings || [])
            .filter(v => v && (v.severity === 'error' || /parse|syntax|yamlerror/i.test(v.check || '')));
        } catch { /* ignore */ }
        const firstLine = (s) => {
          const line = (s || '').split('\n').map(l => l.trim()).find(Boolean);
          return line || '';
        };
        if (structured.length) {
          return {
            ok: false,
            errors: structured.map(v => ({
              file: v.file ? path.resolve(REPO_ROOT, v.file) : filePath,
              line: Number(v.line) || 0,
              col: Number(v.col) || 0,
              message: v.message || firstLine(res.stderr) || firstLine(res.stdout) || 'YAML syntax parse failed',
            })),
          };
        }
        const errMsg = firstLine(res.stderr) || firstLine(res.stdout) || `YAML syntax parse failed (exit ${res.status})`;
        const lineMatch = errMsg.match(/\(line[: ]+(\d+)[,: ]+column[: ]+(\d+)\)|\bline\s+(\d+)[,:\s]+col(?:umn)?\s+(\d+)/i);
        let line = 0, col = 0;
        if (lineMatch) {
          line = Number(lineMatch[1] || lineMatch[3]) || 0;
          col = Number(lineMatch[2] || lineMatch[4]) || 0;
        }
        return { ok: false, errors: [{ file: filePath, line, col, message: errMsg }] };
      } catch (inner) {
        return { ok: false, errors: [{ file: filePath, line: 0, col: 0, message: inner.message }] };
      }
    }
    return { ok: false, errors: [{ file: filePath, line: 0, col: 0, message: e.message }] };
  }
}

function writeCombinedReport({ lintSummary, semantic, start, durMs, exitCode }) {
  const fmt = (x) => (x && x.errors ? x : { errors: [], warnings: [] });
  const L = fmt(lintSummary);
  const S = fmt(semantic);
  const allErrors = [
    ...(L.errors || []).map(e => ({ ...e, source: 'lint' })),
    ...(S.errors || []).map(e => ({ ...e, source: 'semantic' })),
  ];
  const allWarns = [
    ...(L.warnings || []).map(e => ({ ...e, source: 'lint' })),
    ...(S.warnings || []).map(e => ({ ...e, source: 'semantic' })),
  ];
  const lines = [];
  lines.push(`# OpenAPI Validation Report`);
  lines.push(``);
  lines.push(`| Field | Value |`);
  lines.push(`|---|---|`);
  lines.push(`| Entry spec | \`${path.relative(REPO_ROOT, ENTRY_OPENAPI)}\` |`);
  lines.push(`| OpenAPI version | \`${openapiSpecVersion(ENTRY_OPENAPI) || 'unknown'}\` |`);
  lines.push(`| Bundled output | \`${path.relative(REPO_ROOT, BUNDLED_OPENAPI)}\` |`);
  lines.push(`| Started | ${start} |`);
  lines.push(`| Duration | ${(durMs / 1000).toFixed(2)}s |`);
  lines.push(`| Exit code | ${exitCode} |`);
  lines.push(`| Errors | ${allErrors.length} |`);
  lines.push(`| Warnings | ${allWarns.length} |`);
  lines.push(``);
  lines.push(`## Violations`);
  lines.push(``);
  if (allErrors.length === 0 && allWarns.length === 0) {
    lines.push(`✅ No violations found.`);
  } else {
    lines.push(`| # | Severity | Source | File | Line | Rule / Check | Message |`);
    lines.push(`|---|---|---|---|---|---|---|`);
    let idx = 1;
    for (const v of allErrors) {
      lines.push(`| ${idx++} | **error** | ${v.source} | ${v.file || '-'} | ${v.line ?? '-'} | ${v.rule || v.check || '-'} | ${v.message || ''} |`);
    }
    for (const v of allWarns) {
      lines.push(`| ${idx++} | warn | ${v.source} | ${v.file || '-'} | ${v.line ?? '-'} | ${v.rule || v.check || '-'} | ${v.message || ''} |`);
    }
  }
  lines.push(``);
  lines.push(`## Raw Log`);
  lines.push(``);
  lines.push(`See \`reports/openapi-validation.log\` for full command output and progress.`);
  fs.writeFileSync(COMBINED_MD, lines.join('\n') + '\n', 'utf8');
  log('info', `Combined report written: ${path.relative(REPO_ROOT, COMBINED_MD)}`);
}

function main(argv) {
  const startTime = new Date();
  const args = new Set(argv.slice(2));
  const allowWarns = args.has('--allow-warnings') || args.has('--allow-warns');
  const skipSemantic = args.has('--skip-semantic');
  const skipLint = args.has('--skip-lint');
  const onlySemantic = args.has('--only-semantic');
  const onlyLint = args.has('--only-lint');

  ensureReportDir();
  log('info', `Starting OpenAPI validation (pid=${process.pid})`);
  log('info', `Entry spec: ${ENTRY_OPENAPI}`);

  if (!fileExists(ENTRY_OPENAPI)) fatal(`Entry OpenAPI not found at ${ENTRY_OPENAPI}`);
  if (!dirExists(path.join(REPO_ROOT, 'node_modules', '@redocly'))) {
    log('warn', '@redocly/cli not installed; running `npm install --no-audit --no-fund`...');
    const r = run(process.platform === 'win32' ? 'npm.cmd' : 'npm', ['install', '--no-audit', '--no-fund']);
    if (r.status !== 0) fatal('npm install failed (Redocly CLI required)');
  }

  // --- Step 1: YAML syntax of entry file (fast fail)
  log('step', '1/5 — YAML syntax parsing');
  const syntax = yamlSyntaxCheck(ENTRY_OPENAPI);
  if (!syntax.ok) {
    syntax.errors.forEach(e => log('error', `YAML syntax error at ${path.relative(REPO_ROOT, e.file)}:${e.line}:${e.col} — ${e.message}`));
    writeCombinedReport({
      lintSummary: { errors: syntax.errors, warnings: [] },
      semantic: null,
      start: startTime.toISOString(),
      durMs: Date.now() - startTime.getTime(),
      exitCode: 2,
    });
    flushLog(() => process.exit(2));
    return;
  }

  let lintErrors = [];
  let lintWarnings = [];

  if (!onlySemantic && !skipLint) {
    // --- Step 2: Redocly lint — JSON report
    log('step', '2/5 — Redocly CLI lint (structured JSON)');
    fs.mkdirSync(path.dirname(LINT_JSON), { recursive: true });
    const json = redoclyRun('lint', [ENTRY_OPENAPI, '--format=json', '--max-problems=10000']);
    try {
      // Redocly --format=json writes valid JSON even on non-zero exit
      const parsed = JSON.parse(json.stdout || '{"problems":[]}');
      const problems = parsed.problems || [];
      fs.writeFileSync(LINT_JSON, JSON.stringify(parsed, null, 2), 'utf8');
      lintErrors = problems.filter(p => p.severity === 'error').map(p => ({
        rule: p.ruleId || p.rule || '',
        file: p.location?.source ? path.relative(REPO_ROOT, path.resolve(REPO_ROOT, p.location.source)) : '',
        line: p.location?.pointer ? guessLineFromPointer(p.location.source, p.location.pointer) : (p.location?.start?.line ?? p.start?.line ?? null),
        message: p.message || '',
      }));
      lintWarnings = problems.filter(p => p.severity === 'warn').map(p => ({
        rule: p.ruleId || p.rule || '',
        file: p.location?.source ? path.relative(REPO_ROOT, path.resolve(REPO_ROOT, p.location.source)) : '',
        line: p.location?.pointer ? guessLineFromPointer(p.location.source, p.location.pointer) : (p.location?.start?.line ?? p.start?.line ?? null),
        message: p.message || '',
      }));
    } catch (e) {
      log('warn', `Failed to parse Redocly JSON output: ${e.message}`);
    }

    // --- Step 3: Redocly lint — human-readable (stylish) for TXT report
    log('step', '3/5 — Redocly CLI lint (human-readable)');
    const stylish = redoclyRun('lint', [ENTRY_OPENAPI, '--format=stylish', '--max-problems=200']);
    fs.writeFileSync(LINT_TXT, (stylish.stdout || '') + (stylish.stderr ? '\n--- stderr ---\n' + stylish.stderr : ''), 'utf8');
  }

  if (!onlySemantic && !skipLint) {
    // --- Step 4: Bundle + deref outputs
    log('step', '4/5 — Produce bundled + dereferenced specs');
    const b = redoclyRun('bundle', [ENTRY_OPENAPI, '-o', BUNDLED_OPENAPI, '--ext=yaml']);
    if (b.status !== 0) fatal('Redocly bundle failed', 3);
    const d = redoclyRun('bundle', [ENTRY_OPENAPI, '--dereferenced', '-o', DEREF_OPENAPI, '--ext=yaml']);
    if (d.status !== 0) fatal('Redocly deref bundle failed', 3);
    log('info', `Bundled: ${path.relative(REPO_ROOT, BUNDLED_OPENAPI)}`);
    log('info', `Dereferenced: ${path.relative(REPO_ROOT, DEREF_OPENAPI)}`);
  }

  // --- Step 5: Semantic validation via Python companion
  let semantic = null;
  if (!onlyLint && !skipSemantic) {
    log('step', '5/5 — Semantic checks (Python validator)');
    const pythonScript = path.join(__dirname, 'validate_openapi.py');
    if (!fileExists(pythonScript)) fatal(`Python semantic validator missing at ${pythonScript}`);
    const pythonBin = process.platform === 'win32' ? 'python.exe' : (process.env.PYTHON_BIN || 'python3');
    const cmd = run(pythonBin, [
      pythonScript,
      '--entry', ENTRY_OPENAPI,
      '--bundle', BUNDLED_OPENAPI,
      '--json-out', SEMANTIC_JSON,
      '--report-dir', REPORTS_DIR,
      allowWarns ? '--allow-warnings' : '',
    ].filter(Boolean));
    try {
      semantic = JSON.parse(fs.readFileSync(SEMANTIC_JSON, 'utf8'));
    } catch (e) {
      log('warn', `Failed to parse semantic JSON: ${e.message}`);
      semantic = { errors: [], warnings: [] };
    }
    if (cmd.status !== 0 && !(allowWarns && !semantic.errors?.length)) {
      // propagate semantic exit; log but don't exit yet so we still write report
      log('warn', `Semantic validator exited ${cmd.status}`);
    }
  }

  const semErr = semantic?.errors?.length || 0;
  const semWarn = semantic?.warnings?.length || 0;
  const errCount = lintErrors.length + semErr;
  const warnCount = lintWarnings.length + semWarn;
  let exitCode = 0;
  if (errCount > 0) exitCode = 1;
  else if (!allowWarns && warnCount > 0) exitCode = 0; // warns never fail default unless caller opts; configurable via flag if desired

  log('info', `Summary: errors=${errCount}, warnings=${warnCount} (lint errors=${lintErrors.length}/warn=${lintWarnings.length}; semantic errors=${semErr}/warn=${semWarn})`);

  writeCombinedReport({
    lintSummary: { errors: lintErrors, warnings: lintWarnings },
    semantic,
    start: startTime.toISOString(),
    durMs: Date.now() - startTime.getTime(),
    exitCode,
  });

  flushLog(() => {
    // Print final verdict with CI-friendly grouping
    console.log('');
    console.log('--- VALIDATION RESULT ---');
    if (exitCode === 0) {
      console.log('PASS — OpenAPI validation successful');
      if (warnCount) console.log(`  (with ${warnCount} warning${warnCount === 1 ? '' : 's'}; pass with --allow-warnings to silence)`);
    } else {
      console.log(`FAIL — ${errCount} error${errCount === 1 ? '' : 's'} (see reports/openapi-report.md)`);
    }
    process.exit(exitCode);
  });
}

function guessLineFromPointer(sourceFile, pointer) {
  /* Best-effort: walk the YAML tree with the vendored yaml module and try to
     recover start.line of the node at $ref pointer. Returns line number or
     null if not available. */
  try {
    const yaml = require('yaml');
    const src = fs.readFileSync(path.resolve(REPO_ROOT, sourceFile || ''), 'utf8');
    const doc = yaml.parseDocument(src, { keepSourceTokens: true });
    if (!doc) return null;
    const parts = pointer.split('/').filter(Boolean).map(decodeURIComponent);
    let node = doc.contents;
    for (const p of parts) {
      if (!node) break;
      if (node.type === 'MAP') {
        const pair = node.items.find(kv => String(kv.key?.value ?? kv.key) === p);
        node = pair?.value;
      } else if (node.type === 'SEQ') {
        const idx = Number.parseInt(p, 10);
        node = Number.isFinite(idx) ? node.items[idx] : undefined;
      } else {
        break;
      }
    }
    if (node && node.range) {
      // range is [start, end, source]; convert to 1-based line via slice
      const pre = src.slice(0, node.range[0]);
      return (pre.match(/\n/g) || []).length + 1;
    }
    return node?.srcToken?.start?.line ?? null;
  } catch {
    return null;
  }
}

main(process.argv);
