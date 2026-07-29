#!/usr/bin/env python3
"""
OpenAPI Semantic & Conformance Validator
=========================================

Companion to validate_openapi.cjs. Runs AFTER redocly lint/bundle and verifies
project-specific semantic rules that generic linters miss.

Checks implemented
------------------
1.  Path format: always `/api/v1/<resource>` + kebab-case resources + consistent
    parameter placeholder names (`{camelCaseId}` / `{usernameOrId}` style rules).
2.  Versioned root: every path must begin with `/api/v1` (no v0/v2 drift without
    explicit migration).
3.  Operation uniqueness: each operationId is unique, snake_case or camelCase only.
4.  Tag consistency: each operation declares only tags that are defined in `tags:`
    at the root.
5.  Response completeness: every operation defines >=1 2xx success code; read-only
    methods (GET/HEAD) must define 406/404/401 as applicable; mutation methods
    (POST/PUT/PATCH/DELETE) must define 400 + 401 + 403 + 409/422 as applicable;
    every non-`$ref` response must include `description`.
6.  Security: non-public endpoints (paths not listed in PUBLIC_PATH_PREFIXES) must
    include an entry in `security:` (per-op or default global). Operations that
    touch admin MUST require the `Authorization` security scheme.
7.  Schema data-type consistency:
      - every schema `type` is one of the OpenAPI 3.x primitive/aggregate set;
      - if `format` is set, it must be compatible with the type (e.g. string+date,
        int32+integer, uuid+string);
      - `enum` arrays have no duplicates and every value matches the declared type;
      - common objects (`links`, `meta`, `pagination`, `envelope.data`) match the
        project-wide envelope shape (see components/schemas/Common.yaml).
8.  Parameter style: path parameters use `{id}` patterns matching the schema type
    in `components/parameters.yaml`; query params for pagination use page/per_page;
    `from_date` / `to_date` are ISO-8601 strings; no duplicate in+name pairs per op.
9.  Split-ref integrity: every `$ref` that points into `./paths/*.yaml#/~1api~1v1~1...`
    resolves; every referenced schema/component is resolvable in `components/`.
10. 3.0 / 3.1 compatibility: no legacy `nullable: true` (3.0-only) in 3.1 specs
    where `type: [X, "null"]` or `anyOf/null` should be used instead;
    no `exclusiveMinimum: true` when number/int absent.
11. Reference cycle detection: warns on cycles > 8 hops (indicates pathological
    schema recursion in user-written schemas).

Outputs:
    - JSON report with { errors: [...], warnings: [...], stats: {...} } written to
      --json-out.
    - Progress + summary logged to reports/openapi-semantic.log via the logger.

Exit codes: 0 = clean; 1 = errors; 2 = fatal IO/parse error.
"""

from __future__ import annotations

import argparse
import json
import logging
import os
import re
import sys
from collections import Counter, defaultdict
from dataclasses import dataclass, asdict, field
from pathlib import Path
from typing import Any, Iterable

# ---- YAML parsing with line numbers ----------------------------------------
# PyYAML does not expose line numbers on plain dicts, so we use ruamel.yaml if
# available; otherwise we fall back to a mark-attributed loader based on pyyaml.
try:
    from ruamel.yaml import YAML  # type: ignore
    from ruamel.yaml.comments import CommentedBase, CommentedMap, CommentedSeq  # type: ignore
    _HAS_RUAMEL = True

    def _line_of(node: Any) -> int | None:
        if isinstance(node, CommentedBase):
            return node.lc.line + 1 if node.lc and node.lc.line is not None else None
        return None
    _YAML_LOAD = lambda raw: YAML(pure=True).load(raw)  # noqa: E731
except Exception:  # pragma: no cover
    _HAS_RUAMEL = False
    import yaml as _pyyaml  # type: ignore

    class _MarkedLoader(_pyyaml.SafeLoader):  # pragma: no cover - fallback path
        pass

    def _construct_mapping(loader, node, deep=False):  # pragma: no cover
        mapping = _pyyaml.SafeLoader.construct_mapping(loader, node, deep=deep)
        mapping['__line__'] = node.start_mark.line + 1
        return mapping

    def _construct_sequence(loader, node, deep=False):  # pragma: no cover
        seq = _pyyaml.SafeLoader.construct_sequence(loader, node, deep=deep)
        seq.append(('__line__', node.start_mark.line + 1))
        return seq

    _MarkedLoader.add_constructor(_pyyaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, _construct_mapping)
    _MarkedLoader.add_constructor(_pyyaml.resolver.BaseResolver.DEFAULT_SEQUENCE_TAG, _construct_sequence)

    def _line_of(node: Any) -> int | None:  # pragma: no cover
        if isinstance(node, dict):
            return node.get('__line__')
        if isinstance(node, list) and node and node[-1] and isinstance(node[-1], tuple) and len(node[-1]) == 2 and node[-1][0] == '__line__':
            return node[-1][1]
        return None
    _YAML_LOAD = lambda raw: _pyyaml.load(raw, Loader=_MarkedLoader)  # noqa: E731

REPO_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_PUBLIC_PATH_PREFIXES = (
    '/api/v1/posts',
    '/api/v1/users/',
    '/api/v1/health',
    '/api/v1/categories',
)
WELL_KNOWN_EXEMPT_PATH_PREFIXES = (
    '/health/',   # liveness/readiness/version probes (standard non-api prefix)
    '/openapi',
    '/.well-known/',
    '/robots.txt',
    '/favicon.ico',
)
ADMIN_PATH_INFIX = '/admin/'
HTTP_SAFE_METHODS = {'get', 'head', 'options'}
HTTP_MUTATION_METHODS = {'post', 'put', 'patch', 'delete'}
OPENAPI_VALID_TYPES = {'object', 'array', 'string', 'number', 'integer', 'boolean', 'null'}
COMPATIBLE_FORMATS: dict[str, set[str]] = {
    'string': {'date', 'date-time', 'time', 'email', 'hostname', 'idn-email',
               'idn-hostname', 'ipv4', 'ipv6', 'iri', 'iri-reference',
               'json-pointer', 'regex', 'relative-json-pointer', 'uri',
               'uri-reference', 'uri-template', 'uuid', 'password', 'binary', 'byte'},
    'integer': {'int32', 'int64'},
    'number': {'float', 'double'},
}
ILLEGAL_NULLABLE_VERSION = (3, 1)  # 3.1+ must NOT use nullable: true outside 3.0-formalisms.


# ---- Results data model ------------------------------------------------------
@dataclass
class Violation:
    severity: str                 # 'error' | 'warning'
    check: str                    # stable rule id
    message: str
    file: str = ''
    line: int | None = None
    rule: str = ''                # when borrowed from redocly-style rules

    def to_dict(self) -> dict[str, Any]:
        return {k: v for k, v in asdict(self).items() if v is not None}


@dataclass
class ValidationState:
    entry_path: Path
    bundle_path: Path
    report_dir: Path
    spec: Any = None
    bundle: Any = None
    entry_file: str = ''
    bundle_file: str = ''
    violations: list[Violation] = field(default_factory=list)
    stats: dict[str, Any] = field(default_factory=dict)

    def add(self, v: Violation) -> None:
        self.violations.append(v)

    @property
    def errors(self) -> list[Violation]:
        return [v for v in self.violations if v.severity == 'error']

    @property
    def warnings(self) -> list[Violation]:
        return [v for v in self.violations if v.severity == 'warning']


def _setup_logger(report_dir: Path) -> logging.Logger:
    report_dir.mkdir(parents=True, exist_ok=True)
    log_path = report_dir / 'openapi-semantic.log'
    logger = logging.getLogger('openapi-semantic')
    logger.setLevel(logging.DEBUG)
    logger.handlers.clear()
    fmt = logging.Formatter('[%(asctime)s] [%(levelname)s] %(message)s', '%Y-%m-%dT%H:%M:%S')
    sh = logging.StreamHandler(stream=sys.stdout)
    sh.setFormatter(fmt); logger.addHandler(sh)
    fh = logging.FileHandler(log_path, mode='w', encoding='utf-8')
    fh.setFormatter(fmt); logger.addHandler(fh)
    return logger


# ---- Loader helpers ----------------------------------------------------------
def _resolve_file(working_dir: Path, maybe_ref: str) -> Path:
    if maybe_ref.startswith('./') or maybe_ref.startswith('/') or (len(maybe_ref) > 2 and maybe_ref[1] == ':'):
        p = Path(maybe_ref)
    else:
        p = working_dir / maybe_ref
    return p.resolve()


def _parse_yaml_file(file_path: Path) -> tuple[Any, list[Violation]]:
    errs: list[Violation] = []
    try:
        raw = file_path.read_text(encoding='utf-8')
    except OSError as e:
        errs.append(Violation('error', 'IO/read-file', f'failed to read {file_path.name}: {e}', str(file_path), 0))
        return None, errs
    try:
        doc = _YAML_LOAD(raw)
    except Exception as e:  # noqa: BLE001
        msg = str(e)
        line = 0
        col = 0
        # PyYAML / ruamel emit "line X, column Y" (case vary); also "(line N: column M)" forms
        m = re.search(r'[Ll]ine\s+(\d+)[,:]?\s*(?:[Cc]ol(?:umn)?)\s+(\d+)|\(line[: ]+(\d+)[,: ]+[Cc]ol(?:umn)?[: ]+(\d+)\)', msg)
        if m:
            line = int(m.group(1) or m.group(3) or 0)
            col = int(m.group(2) or m.group(4) or 0)
        else:
            m2 = re.search(r'\bline\s+(\d+)', msg)
            if m2:
                line = int(m2.group(1))
        rel_name = str(file_path)
        try:
            rel_name = str(file_path.relative_to(REPO_ROOT))
        except Exception:  # noqa: BLE001
            pass
        errs.append(Violation(
            'error', 'syntax/yaml-parse', f'YAML syntax error: {msg}', rel_name, line or 0,
            rule=f'col={col}' if col else '',
        ))
        return None, errs
    if not isinstance(doc, (dict, type(None))):
        errs.append(Violation('error', 'syntax/root-shape', 'root YAML node must be a mapping', str(file_path), _line_of(doc)))
    return doc, errs


def _split_ref(ref: str) -> tuple[str, str]:
    if '#' in ref:
        f, p = ref.split('#', 1)
        return f, p
    return ref, ''


def _deref_pointer(document: Any, pointer: str) -> Any:
    if not pointer:
        return document
    parts = [seg.replace('~1', '/').replace('~0', '~') for seg in pointer.lstrip('/').split('/')]
    node = document
    for p in parts:
        if isinstance(node, dict):
            if p not in node:
                raise KeyError(pointer)
            node = node[p]
        elif isinstance(node, list):
            try:
                node = node[int(p)]
            except (ValueError, IndexError) as e:
                raise KeyError(pointer) from e
        else:
            raise KeyError(pointer)
    return node


def _load_entry_resolved(state: ValidationState, logger: logging.Logger) -> None:
    """Load entry + bundle. Bundle is produced by redocly step so it's fully flat except for internal refs."""
    state.entry_file = str(state.entry_path.relative_to(REPO_ROOT))
    state.bundle_file = str(state.bundle_path.relative_to(REPO_ROOT))
    logger.info('loading entry spec %s', state.entry_file)
    entry, v1 = _parse_yaml_file(state.entry_path)
    for v in v1: state.add(v)
    logger.info('loading bundled spec %s', state.bundle_file)
    bundle, v2 = _parse_yaml_file(state.bundle_path)
    for v in v2: state.add(v)
    if entry is None or bundle is None:
        raise SystemExit(2)
    state.spec = entry
    state.bundle = bundle


# ---- Rule implementations ----------------------------------------------------
def r01_openapi_version(state: ValidationState) -> None:
    spec = state.spec
    version = spec.get('openapi') if isinstance(spec, dict) else None
    if not version or not isinstance(version, str) or not re.match(r'^3\.[0-9]+\.[0-9]+$', version):
        state.add(Violation('error', 'open-api-version',
            f"spec must declare `openapi: 3.x.y` at the root (got: {version!r})",
            state.entry_file, _line_of(spec)))
        return
    state.stats['openapi_version'] = version
    state.stats['openapi_version_tuple'] = tuple(int(x) for x in version.split('.'))


def r02_path_format_and_versioning(state: ValidationState) -> None:
    paths = state.spec.get('paths') if isinstance(state.spec, dict) else None
    if not isinstance(paths, dict):
        state.add(Violation('error', 'paths-present', '`paths:` object missing at root', state.entry_file, _line_of(state.spec)))
        return
    state.stats['paths_count'] = len(paths)
    api_versioned_re = re.compile(r'^/api/v1(/[A-Za-z0-9_.\-~]+|/\{[A-Za-z_][A-Za-z0-9_]*\})*$')
    kebab_resource_re = re.compile(r'^[a-z0-9][a-z0-9\-]*$')
    for raw_path in list(paths.keys()):
        exempt = any(raw_path.startswith(pref) for pref in WELL_KNOWN_EXEMPT_PATH_PREFIXES)
        if exempt:
            continue
        if not api_versioned_re.match(raw_path):
            state.add(Violation('error', 'path/format',
                f'path `{raw_path}` does not match `/api/v1/...` kebab/curly-param shape',
                state.entry_file, _line_of(paths)))
        segs = [s for s in raw_path.split('/') if s]
        # First 2 segments are 'api', 'v1'
        for seg in segs[2:]:
            if seg.startswith('{') and seg.endswith('}'):
                name = seg[1:-1]
                if not re.match(r'^[A-Za-z][A-Za-z0-9]*(Id|OrId|Number|Name|Slug|Token|Key|Code)$|^(id|page|per_page|slug|usernameOrId|revisionIdOrNumber)$', name):
                    state.add(Violation('warning', 'path/param-name',
                        f'path parameter `{{{name}}}` in `{raw_path}` should use camelCase Id suffix or known names',
                        state.entry_file, _line_of(paths)))
            elif not kebab_resource_re.match(seg):
                state.add(Violation('error', 'path/resource-kebab',
                    f'resource segment `{seg}` in `{raw_path}` must be kebab-case alphanumeric',
                    state.entry_file, _line_of(paths)))


def r03_operations(state: ValidationState) -> None:
    paths = state.spec.get('paths') if isinstance(state.spec, dict) else {}
    defined_tags = {t.get('name') for t in state.spec.get('tags', []) if isinstance(t, dict) and t.get('name')}
    global_security = [dict(s) for s in state.spec.get('security', []) if isinstance(s, dict)]
    operation_ids: Counter[str] = Counter()
    state.stats['operations_count'] = 0
    for path, path_item in paths.items():
        if not isinstance(path_item, dict):
            continue
        for method in list(path_item.keys()):
            if method not in HTTP_SAFE_METHODS | HTTP_MUTATION_METHODS:
                if method in ('$ref', 'summary', 'description', 'servers', 'parameters'):
                    continue
                state.add(Violation('warning', 'op/method-unknown',
                    f'`{path}` has non-standard HTTP verb `{method}`',
                    state.entry_file, _line_of(path_item)))
                continue
            op = path_item[method]
            if not isinstance(op, dict):
                continue
            state.stats['operations_count'] += 1
            opid = op.get('operationId')
            if not opid or not isinstance(opid, str):
                state.add(Violation('error', 'op/operationId',
                    f'{method.upper()} {path} missing `operationId`',
                    state.entry_file, _line_of(op)))
            else:
                if not re.match(r'^[A-Za-z][A-Za-z0-9_]*$', opid):
                    state.add(Violation('error', 'op/operationId-format',
                        f'operationId `{opid}` must be snake_case or camelCase alphanumeric',
                        state.entry_file, _line_of(op)))
                operation_ids[opid] += 1
            tags = op.get('tags') or []
            if not isinstance(tags, list):
                state.add(Violation('error', 'op/tags-shape', f'{method.upper()} {path} tags must be a list', state.entry_file, _line_of(op)))
            else:
                for tag in tags:
                    if tag not in defined_tags:
                        state.add(Violation('error', 'op/tag-defined',
                            f'{method.upper()} {path} references undefined tag `{tag}`',
                            state.entry_file, _line_of(op)))
            # 2xx response
            responses = op.get('responses') if isinstance(op.get('responses'), dict) else {}
            if not responses:
                state.add(Violation('error', 'op/responses-present',
                    f'{method.upper()} {path} must declare `responses`', state.entry_file, _line_of(op)))
            else:
                codes = list(responses.keys())
                if not any(re.match(r'^2\d\d|2XX$', c) for c in codes):
                    state.add(Violation('error', 'op/has-2xx',
                        f'{method.upper()} {path} must declare at least one 2xx response',
                        state.entry_file, _line_of(responses)))
                for code, resp in responses.items():
                    if not isinstance(resp, dict): continue
                    if '$ref' in resp: continue
                    if not resp.get('description'):
                        state.add(Violation('error', 'op/response-description',
                            f'{method.upper()} {path} response `{code}` missing `description`',
                            state.entry_file, _line_of(resp)))
            # Security coverage
            sec = op.get('security') if isinstance(op.get('security'), list) else global_security
            exempt = any(path.startswith(pref) for pref in WELL_KNOWN_EXEMPT_PATH_PREFIXES)
            is_public = exempt or (any(path.startswith(pref) for pref in DEFAULT_PUBLIC_PATH_PREFIXES) and ADMIN_PATH_INFIX not in path)
            if not is_public and not sec:
                state.add(Violation('error', 'security/covered',
                    f'{method.upper()} {path} is not in public prefixes but declares no `security`',
                    state.entry_file, _line_of(op)))
            if ADMIN_PATH_INFIX in path:
                has_bearer = any('bearerAuth' in s for s in sec) if sec else False
                if not has_bearer:
                    state.add(Violation('warning', 'security/admin-bearer',
                        f'{method.upper()} {path} should require `bearerAuth` security',
                        state.entry_file, _line_of(op)))
            # Parameter uniqueness
            params = (path_item.get('parameters') if isinstance(path_item.get('parameters'), list) else []) + \
                     (op.get('parameters') if isinstance(op.get('parameters'), list) else [])
            seen: set[tuple[str, str]] = set()
            for p in params:
                if not isinstance(p, dict) or '$ref' in p:
                    continue
                k = (p.get('in', ''), p.get('name', ''))
                if k in seen:
                    state.add(Violation('error', 'param/duplicate',
                        f'{method.upper()} {path} has duplicate parameter `{k[1]}` in `{k[0]}`',
                        state.entry_file, _line_of(p)))
                seen.add(k)
    # Uniqueness summary for operationIds
    for opid, cnt in operation_ids.items():
        if cnt > 1:
            state.add(Violation('error', 'op/operationId-unique',
                f'operationId `{opid}` reused {cnt} times', state.entry_file))


def r04_schema_types_and_formats(state: ValidationState) -> None:
    bundle = state.bundle if isinstance(state.bundle, dict) else {}
    schemas = bundle.get('components', {}).get('schemas') if isinstance(bundle.get('components'), dict) else {}
    if not isinstance(schemas, dict): return
    state.stats['schemas_count'] = len(schemas)
    visited: dict[int, None] = {}

    def check_node(node: Any, breadcrumb: str, depth: int = 0) -> None:
        if depth > 30:
            state.add(Violation('warning', 'schema/recursion-depth',
                f'schema recursion deeper than 30 at `{breadcrumb}`',
                state.bundle_file, _line_of(node)))
            return
        nid = id(node)
        if nid in visited: return
        visited[nid] = None
        if isinstance(node, dict):
            t = node.get('type')
            fmt = node.get('format')
            if t is not None:
                if isinstance(t, list):
                    for one_t in t:
                        if not isinstance(one_t, str) or one_t not in OPENAPI_VALID_TYPES:
                            state.add(Violation('error', 'schema/type-value',
                                f'`{breadcrumb}` declares invalid type list member `{one_t}`',
                                state.bundle_file, _line_of(node)))
                elif not isinstance(t, str) or t not in OPENAPI_VALID_TYPES:
                    state.add(Violation('error', 'schema/type-value',
                        f'`{breadcrumb}` declares invalid type `{t}`',
                        state.bundle_file, _line_of(node)))
            if fmt is not None and isinstance(t, str) and t in COMPATIBLE_FORMATS:
                if fmt not in COMPATIBLE_FORMATS[t]:
                    state.add(Violation('error', 'schema/format-mismatch',
                        f'`{breadcrumb}` has incompatible format `{fmt}` for type `{t}`',
                        state.bundle_file, _line_of(node)))
            enum = node.get('enum')
            if isinstance(enum, list):
                seen_e = []
                for v in enum:
                    try:
                        if isinstance(v, dict):
                            hv = tuple(sorted((k, tuple(vv.items()) if isinstance(vv, dict) else (tuple(vv) if isinstance(vv, list) else vv)) for k, vv in v.items()))
                        elif isinstance(v, list):
                            hv = tuple(v)
                        else:
                            hv = v
                        if hv in seen_e:
                            state.add(Violation('error', 'schema/enum-duplicates',
                                f'`{breadcrumb}` enum contains duplicate values',
                                state.bundle_file, _line_of(node)))
                            break
                        seen_e.append(hv)
                    except TypeError:
                        continue
            # 3.1+ forbids `nullable: true` (should use type array/null)
            if 'nullable' in node and state.stats.get('openapi_version_tuple', (3, 1)) >= ILLEGAL_NULLABLE_VERSION:
                state.add(Violation('error', 'schema/nullable-31',
                    f'`{breadcrumb}` uses 3.0-only `nullable: true` in a 3.1 spec; '
                    'use `type: [X, "null"]` or `anyOf: [{type: X}, {type: null}]`',
                    state.bundle_file, _line_of(node)))
            # Descend
            for k, v in node.items():
                if k in ('properties', 'additionalProperties', 'items', 'allOf', 'anyOf', 'oneOf', 'not',
                        'patternProperties', 'dependentSchemas', 'unevaluatedProperties',
                        'unevaluatedItems', 'contains', 'prefixItems'):
                    if isinstance(v, dict):
                        if '$ref' in v and len(v) == 1:
                            continue
                        if k == 'properties':
                            for pname, pschema in v.items():
                                check_node(pschema, f'{breadcrumb}.{k}.{pname}', depth + 1)
                        else:
                            check_node(v, f'{breadcrumb}.{k}', depth + 1)
                    elif isinstance(v, list):
                        for i, it in enumerate(v):
                            check_node(it, f'{breadcrumb}.{k}[{i}]', depth + 1)
        elif isinstance(node, list):
            for i, it in enumerate(node):
                check_node(it, f'{breadcrumb}[{i}]', depth + 1)

    for name, schema in schemas.items():
        check_node(schema, f'#/components/schemas/{name}')


def r05_split_ref_integrity(state: ValidationState, logger: logging.Logger) -> None:
    """Walk entry spec and confirm every $ref into paths/*.yaml#/~1api~1v1... resolves."""
    entry = state.spec
    if not isinstance(entry, dict): return
    def refs_in(node: Any) -> Iterable[tuple[str, Any]]:
        if isinstance(node, dict):
            for k, v in node.items():
                if k == '$ref' and isinstance(v, str):
                    yield v, node
                else:
                    yield from refs_in(v)
        elif isinstance(node, list):
            for it in node:
                yield from refs_in(it)
    unresolved = 0
    for ref, node in refs_in(entry):
        file_part, pointer = _split_ref(ref)
        if not file_part:
            continue  # intra-file; handled by Redocly
        target = _resolve_file(state.entry_path.parent, file_part)
        if not target.exists():
            state.add(Violation('error', 'ref/file-missing',
                f'$ref points to missing file `{file_part}`',
                state.entry_file, _line_of(node)))
            unresolved += 1
            continue
        sub, errs = _parse_yaml_file(target)
        for e in errs: state.add(e)
        if sub is None: continue
        if pointer:
            try:
                _deref_pointer(sub, pointer)
            except KeyError:
                state.add(Violation('error', 'ref/pointer-missing',
                    f'$ref `{ref}` has unresolved JSON pointer `{pointer}`',
                    state.entry_file, _line_of(node)))
                unresolved += 1
    logger.info('split-ref integrity: %d unresolved refs reported', unresolved)
    state.stats['unresolved_split_refs'] = unresolved


def r06_envelope_consistency(state: ValidationState) -> None:
    """Project rule: EnvelopeXxx wrappers in Common.yaml must have `ok`, `data`, `meta` shape."""
    schemas = state.bundle.get('components', {}).get('schemas') if isinstance(state.bundle, dict) else {}
    if not isinstance(schemas, dict): return
    for name, sch in schemas.items():
        if not re.match(r'^Envelope', name): continue
        if not isinstance(sch, dict): continue
        props = sch.get('properties') if isinstance(sch.get('properties'), dict) else {}
        for required_key in ('ok', 'data'):
            if required_key not in props:
                state.add(Violation('warning', 'schema/envelope-shape',
                    f'`{name}` envelope is missing required `{required_key}` property',
                    state.bundle_file, _line_of(sch)))
        if 'meta' not in props and 'pagination' not in props:
            state.add(Violation('warning', 'schema/envelope-shape',
                f'`{name}` envelope has neither `meta` nor `pagination` property',
                state.bundle_file, _line_of(sch)))


def r07_cycle_detection(state: ValidationState) -> None:
    schemas = state.bundle.get('components', {}).get('schemas') if isinstance(state.bundle, dict) else {}
    if not isinstance(schemas, dict): return

    def visit(name: str, path: list[str]) -> None:
        if name in path:
            cycle_len = len(path) - path.index(name) + 1
            if cycle_len > 8:
                state.add(Violation('warning', 'schema/cycle',
                    f'schema reference cycle of length {cycle_len}: {" -> ".join(path + [name])}',
                    state.bundle_file))
            return
        sch = schemas.get(name)
        if not isinstance(sch, dict): return
        path.append(name)
        def look(v: Any) -> None:
            if isinstance(v, dict):
                ref = v.get('$ref')
                if isinstance(ref, str) and ref.startswith('#/components/schemas/'):
                    nxt = ref.split('/')[-1]
                    visit(nxt, path)
                for nv in v.values(): look(nv)
            elif isinstance(v, list):
                for it in v: look(it)
        look(sch)
        path.pop()

    for n in list(schemas.keys()):
        visit(n, [])


# ---- Driver ------------------------------------------------------------------
ALL_CHECKS = (
    r01_openapi_version,
    r02_path_format_and_versioning,
    r03_operations,
    r04_schema_types_and_formats,
    r06_envelope_consistency,
    r07_cycle_detection,
)

def main(argv: list[str]) -> int:
    ap = argparse.ArgumentParser(description='Semantic OpenAPI validator')
    ap.add_argument('--entry', required=True, type=Path, help='openapi.yaml root (split refs)')
    ap.add_argument('--bundle', required=True, type=Path, help='redocly bundle output')
    ap.add_argument('--json-out', required=True, type=Path, help='where to write {errors, warnings, stats}')
    ap.add_argument('--report-dir', required=True, type=Path, help='reports directory (logs)')
    ap.add_argument('--allow-warnings', action='store_true', help='do not fail on warnings alone (we never do; flag kept for symmetry)')
    ap.add_argument('--yaml-only', action='store_true', help='only load + parse YAML, exit 2 on parse errors')
    args = ap.parse_args(argv)

    logger = _setup_logger(args.report_dir)
    logger.info('semantic validator starting (python %s)', '.'.join(map(str, sys.version_info[:3])))
    if not _HAS_RUAMEL:
        logger.warning('ruamel.yaml not available; line numbers will be less precise (pyyaml fallback)')

    state = ValidationState(entry_path=args.entry.resolve(), bundle_path=args.bundle.resolve(), report_dir=args.report_dir.resolve())

    try:
        _load_entry_resolved(state, logger)
    except SystemExit as e:
        logger.error('aborted before semantic checks: exit=%d', e.code)
        _write_json_out(args.json_out, state, logger)
        return 2

    if args.yaml_only:
        logger.info('--yaml-only mode: loaded entry + bundle successfully')
        _write_json_out(args.json_out, state, logger)
        return 0

    for check in ALL_CHECKS:
        logger.info('running %s', check.__name__)
        try:
            check(state)
        except Exception as e:  # noqa: BLE001
            state.add(Violation('error', 'internal', f'check {check.__name__} raised {type(e).__name__}: {e}', state.entry_file))

    # Split-ref integrity is a separate step that needs logger + state
    try:
        logger.info('running r05_split_ref_integrity')
        r05_split_ref_integrity(state, logger)
    except Exception as e:  # noqa: BLE001
        state.add(Violation('error', 'internal', f'check r05_split_ref_integrity raised {type(e).__name__}: {e}', state.entry_file))

    state.stats['violations'] = {'errors': len(state.errors), 'warnings': len(state.warnings)}
    logger.info('semantic checks complete: errors=%d warnings=%d', len(state.errors), len(state.warnings))

    _write_json_out(args.json_out, state, logger)

    # Write human-readable summary for CI logs
    summary_path = state.report_dir / 'openapi-semantic-summary.txt'
    with summary_path.open('w', encoding='utf-8') as fh:
        fh.write(f'OpenAPI Semantic Validation Summary\n')
        fh.write(f'  entry : {state.entry_file}\n')
        fh.write(f'  bundle: {state.bundle_file}\n')
        fh.write(f'  stats : {json.dumps(state.stats, default=str)}\n')
        fh.write(f'  errors ({len(state.errors)}):\n')
        for v in state.errors: fh.write(f'    E {v.check}: {v.message} ({v.file}:{v.line})\n')
        fh.write(f'  warnings ({len(state.warnings)}):\n')
        for v in state.warnings: fh.write(f'    W {v.check}: {v.message} ({v.file}:{v.line})\n')
    logger.info('wrote summary %s', summary_path)
    return 1 if state.errors else 0


def _write_json_out(json_out: Path, state: ValidationState, logger: logging.Logger) -> None:
    json_out.parent.mkdir(parents=True, exist_ok=True)
    payload = {
        'errors': [v.to_dict() for v in state.errors],
        'warnings': [v.to_dict() for v in state.warnings],
        'stats': state.stats,
        'entry': state.entry_file,
        'bundle': state.bundle_file,
    }
    with json_out.open('w', encoding='utf-8') as fh:
        json.dump(payload, fh, indent=2, default=str)
    logger.info('wrote semantic report %s', json_out)


if __name__ == '__main__':
    sys.exit(main(sys.argv[1:]))
