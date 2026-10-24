"""Read-only I011 durable evidence audit. Only the fresh report is written."""
from __future__ import annotations

import argparse
from dataclasses import dataclass
import gzip
import hashlib
import json
import os
from pathlib import Path
import re
from typing import Any, Iterator
from urllib.parse import unquote

WORK = Path(__file__).resolve().parent
ROOT = WORK.parents[3]
EVIDENCE = ROOT / 'docs/evidence/i011-qualification-20261022'
HEX = re.compile(r'[0-9a-f]{64}')
LINK = re.compile(r'\[[^\]]*\]\(([^)]+)\)')
STE_PATH = re.compile(r'(?:^|/)ste-reference/.*\.(?:pdf|txt|text|csv|tsv)$|ASD-STE100_ISSUE9\.(?:pdf|txt|text)|(?:^|/)ste[-_]dictionary', re.I)
ARCHIVE_FIELDS = {
    'raw_sha256': ('raw_sha256', 'source_sha256', 'original_sha256', 'sha256'),
    'raw_bytes': ('raw_bytes', 'source_bytes', 'bytes'),
    'packed_sha256': ('compressed_sha256', 'archive_sha256'),
    'packed_bytes': ('compressed_bytes', 'archive_bytes'),
}


def ste_text_signature(prefix: bytes) -> bool:
    text = prefix.lower()
    return all(marker in text for marker in (
        b'asd-ste100', b'approved meaning/alternatives', b'approved example', b'not ' + b'approved'))


@dataclass(frozen=True)
class OriginReview:
    path: str
    sha256: str
    pairs: tuple[tuple[str, str], ...]


def load_origin_review(path: Path, expected_sha256: str) -> OriginReview:
    if not HEX.fullmatch(expected_sha256):
        raise ValueError('origin review requires exact SHA-256')
    selected = path.resolve(strict=True)
    if not selected.is_relative_to(ROOT):
        raise ValueError('origin review leaves Lo')
    raw = selected.read_bytes()
    if hashlib.sha256(raw).hexdigest() != expected_sha256:
        raise ValueError('origin review hash differs')
    def unique(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
        result: dict[str, Any] = {}
        for key, value in pairs:
            if key in result:
                raise ValueError('duplicate origin review key')
            result[key] = value
        return result
    value = json.loads(raw, object_pairs_hook=unique)
    if not isinstance(value, dict) or set(value) != {'issue', 'classifications'} or value['issue'] != 'I011':
        raise ValueError('origin review schema differs')
    entries = value['classifications']
    if not isinstance(entries, list) or not entries:
        raise ValueError('origin review classifications required')
    pairs = []
    seen = set()
    for entry in entries:
        if not isinstance(entry, dict) or set(entry) != {'source', 'sha256', 'disposition'}:
            raise ValueError('origin classification schema differs')
        source, digest = entry['source'], entry['sha256']
        if not isinstance(source, str) or not isinstance(digest, str) or not HEX.fullmatch(digest):
            raise ValueError('origin classification identity invalid')
        candidate = Path(source)
        if candidate.is_absolute() or '..' in candidate.parts or candidate.as_posix() != source:
            raise ValueError('origin classification path invalid')
        if not (ROOT/candidate).resolve().is_relative_to(ROOT) or source in seen:
            raise ValueError('origin classification path outside Lo or repeated')
        if entry['disposition'] != 'reviewed_detector_source':
            raise ValueError('origin classification disposition unknown')
        seen.add(source)
        pairs.append((source, digest))
    return OriginReview(str(selected.relative_to(ROOT)), expected_sha256, tuple(pairs))


def reviewed_detector_origins(raw_sha256: str, sources: list[str], review: OriginReview) -> list[str]:
    approved = set(review.pairs)
    return sorted({source for source in sources if (source, raw_sha256) in approved})


def local(path: Path, boundary: Path = ROOT) -> Path:
    resolved = path.resolve()
    if not resolved.is_relative_to(boundary):
        raise ValueError(f'path escapes {boundary.relative_to(ROOT) if boundary != ROOT else "Lo"}: {path}')
    return resolved


def relative(path: Path) -> str:
    return str(local(path).relative_to(ROOT))


def digest(path: Path) -> str:
    value = hashlib.sha256()
    with local(path).open('rb') as stream:
        for block in iter(lambda: stream.read(1 << 20), b''):
            value.update(block)
    return value.hexdigest()


def read_json(path: Path) -> Any:
    return json.loads(local(path).read_text())


def objects(value: Any, pointer: str = '') -> Iterator[tuple[str, dict[str, Any]]]:
    if isinstance(value, dict):
        yield pointer, value
        for key, child in sorted(value.items()):
            escaped = key.replace('~', '~0').replace('/', '~1')
            yield from objects(child, pointer + '/' + escaped)
    elif isinstance(value, list):
        for index, child in enumerate(value):
            yield from objects(child, pointer + '/' + str(index))


@dataclass(frozen=True)
class Declaration:
    origin: str
    archive: Path
    source: str
    fields: dict[str, Any]
    zero_timestamp: bool


def archive_path(value: str, document: Path) -> Path:
    path = ROOT / value if value.startswith('docs/evidence/') else document.parent / value
    return local(path, EVIDENCE)


def payload(path: Path) -> tuple[dict[str, Any], bytes]:
    raw_hash = hashlib.sha256()
    raw_size = 0
    prefix = bytearray()
    with gzip.open(local(path, EVIDENCE), 'rb') as stream:
        for block in iter(lambda: stream.read(1 << 20), b''):
            raw_hash.update(block)
            raw_size += len(block)
            if len(prefix) < 262144:
                prefix.extend(block[:262144 - len(prefix)])
    with path.open('rb') as stream:
        header = stream.read(10)
    return {'raw_sha256': raw_hash.hexdigest(), 'raw_bytes': raw_size,
            'packed_sha256': digest(path), 'packed_bytes': path.stat().st_size,
            'gzip_mtime': int.from_bytes(header[4:8], 'little')}, bytes(prefix)


def markdown_files() -> Iterator[Path]:
    for current, directories, files in os.walk(ROOT):
        directories[:] = sorted(name for name in directories if not
                                 (Path(current) == ROOT and name in {'_scratch', '.git', 'bin'}))
        for name in sorted(files):
            if name.endswith('.md') and not name.endswith('-PLAN.md'):
                yield local(Path(current) / name)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--latest-manifest', type=Path, required=True)
    parser.add_argument('--source-sha256', required=True)
    parser.add_argument('--completion-receipt', type=Path)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--origin-review', type=Path, required=True)
    parser.add_argument('--origin-review-sha256', required=True)
    args = parser.parse_args()
    origin_review = load_origin_review(args.origin_review, args.origin_review_sha256)
    output = local(args.output, WORK)
    if output.exists():
        raise ValueError('refusing report replacement or automatic retry')
    if not output.parent.is_dir() or not HEX.fullmatch(args.source_sha256):
        raise ValueError('existing output parent and exact source SHA-256 are required')
    latest_path = local(args.latest_manifest, EVIDENCE)
    latest = read_json(latest_path)
    if not isinstance(latest, dict):
        raise ValueError('latest manifest must be an object')
    failures: list[dict[str, Any]] = []
    warnings: list[dict[str, Any]] = []
    inputs: dict[str, str] = {relative(Path(__file__)): digest(Path(__file__)),
                              origin_review.path: origin_review.sha256}
    declarations: dict[Path, list[Declaration]] = {}
    documents: dict[Path, Any] = {}
    cross_links: list[dict[str, Any]] = []
    claims: list[dict[str, Any]] = []
    resolved_origin_candidates: list[dict[str, Any]] = []
    exclusions: list[dict[str, Any]] = []
    reviews: list[str] = []
    json_paths = sorted(EVIDENCE.rglob('*.json'))
    for document in json_paths:
        try:
            data = read_json(document)
        except (ValueError, OSError) as error:
            failures.append({'kind': 'invalid_json', 'path': relative(document), 'error': str(error)})
            continue
        documents[document] = data
        inputs[relative(document)] = digest(document)
        if 'review' in document.name or 'verification' in document.name:
            reviews.append(relative(document))
        zero_timestamp = isinstance(data, dict) and 'mtime' in str(data.get('archive_format', '')).lower()
        for pointer, row in objects(data):
            origin = relative(document) + '#' + pointer
            archive = row.get('archive')
            if isinstance(archive, str) and archive.endswith('.gz') and '://' not in archive:
                try:
                    target = archive_path(archive, document)
                    source = str(row.get('source', row.get('original_path', '')))
                    declaration = Declaration(origin, target, source, row, zero_timestamp)
                    declarations.setdefault(target, []).append(declaration)
                    if STE_PATH.search(source):
                        failures.append({'kind': 'official_ste_source_candidate', 'origin': origin, 'source': source})
                except ValueError as error:
                    failures.append({'kind': 'archive_boundary', 'origin': origin, 'error': str(error)})
            for key in ['hash_only', 'omitted_binaries', 'omitted_files', 'excluded_groups', 'package_file_identities', 'generated_executables_hash_only']:
                if key in row:
                    exclusions.append({'origin': origin + '/' + key, 'entries': len(row[key]) if isinstance(row[key], (list, dict)) else 1})
            for key in ['qualification_claim', 'qualification_complete', 'i011_complete']:
                if key in row:
                    claims.append({'origin': origin + '/' + key, 'value': row[key],
                                   'latest': document == latest_path and pointer == ''})
            paths = [(key, value) for key, value in row.items() if isinstance(value, str)
                     and value.startswith('docs/evidence/i011-qualification-20261022/') and '\n' not in value]
            for key, value in paths:
                try:
                    target = local(ROOT / value.split('#')[0], EVIDENCE)
                    entry: dict[str, Any] = {'origin': origin, 'key': key, 'target': relative(target)}
                    if not target.exists():
                        failures.append(dict(entry, kind='missing_durable_reference'))
                    candidates = [key + '_sha256']
                    if key.endswith('_path'):
                        candidates.append(key[:-5] + '_sha256')
                    if key == 'path' or len(paths) == 1:
                        candidates.append('sha256')
                    expected = next((row[name] for name in candidates if isinstance(row.get(name), str)
                                     and HEX.fullmatch(row[name])), None)
                    if key != 'archive' and expected and target.is_file():
                        entry.update(expected_sha256=expected, actual_sha256=digest(target))
                        if entry['actual_sha256'] != expected:
                            failures.append(dict(entry, kind='durable_link_hash_mismatch'))
                    cross_links.append(entry)
                except ValueError as error:
                    failures.append({'kind': 'durable_reference_boundary', 'origin': origin, 'error': str(error)})
    actual_payloads: dict[Path, dict[str, Any]] = {}
    for archive in sorted(EVIDENCE.rglob('*.gz')):
        try:
            info, prefix = payload(archive)
            actual_payloads[archive] = info
            inputs[relative(archive)] = info['packed_sha256']
            if prefix.startswith(b'%PDF'):
                failures.append({'kind': 'pdf_origin_review_required', 'archive': relative(archive)})
            if ste_text_signature(prefix):
                origins = reviewed_detector_origins(info['raw_sha256'],
                    [declaration.source for declaration in declarations.get(archive, [])], origin_review)
                if origins:
                    resolved_origin_candidates.append({'kind': 'reviewed_detector_source',
                        'archive': relative(archive), 'raw_sha256': info['raw_sha256'], 'sources': origins})
                else:
                    failures.append({'kind': 'official_ste_text_candidate', 'archive': relative(archive)})
            for declaration in declarations.get(archive, []):
                for field, keys in ARCHIVE_FIELDS.items():
                    expected = next((declaration.fields[key] for key in keys if key in declaration.fields), None)
                    if expected is not None and expected != info[field]:
                        failures.append({'kind': 'archive_declaration_mismatch', 'origin': declaration.origin,
                                         'archive': relative(archive), 'field': field, 'expected': expected, 'actual': info[field]})
                if declaration.zero_timestamp and info['gzip_mtime'] != 0:
                    failures.append({'kind': 'gzip_timestamp_mismatch', 'origin': declaration.origin, 'archive': relative(archive)})
        except (ValueError, OSError, EOFError) as error:
            failures.append({'kind': 'gzip_integrity', 'archive': relative(archive), 'error': str(error)})
    for candidate in sorted(EVIDENCE.rglob('*')):
        if candidate.is_file() and (candidate.suffix.lower() == '.pdf' or STE_PATH.search(relative(candidate))):
            failures.append({'kind': 'uncompressed_ste_or_pdf_origin_review_required', 'path': relative(candidate)})
    for archive in declarations:
        if archive not in actual_payloads:
            failures.append({'kind': 'missing_or_invalid_declared_archive', 'archive': relative(archive)})
    unindexed = [relative(path) for path in actual_payloads if path not in declarations]
    if unindexed:
        warnings.append({'kind': 'valid_unindexed_payloads', 'paths': unindexed})
    markdown: list[dict[str, Any]] = []
    completion_sentences: list[dict[str, Any]] = []
    for document in markdown_files():
        local(document)
        content = document.read_text()
        selected = []
        for match in LINK.finditer(content):
            destination = match.group(1).strip('<>').split('#')[0]
            if not destination or '://' in destination or destination.startswith('#'):
                continue
            target = (document.parent / unquote(destination)).resolve()
            if target.is_relative_to(EVIDENCE):
                selected.append({'source': relative(document), 'target': relative(target), 'exists': target.exists()})
        if selected:
            inputs[relative(document)] = digest(document)
            markdown.extend(selected)
        for entry in selected:
            if not entry['exists']:
                failures.append(dict(entry, kind='missing_markdown_target'))
        for line_number, line in enumerate(content.splitlines(), 1):
            if re.search(r'\bI011 (?:is |has been )?(?:complete|closed|fully qualified)\b', line, re.I):
                completion_sentences.append({'path': relative(document), 'line': line_number, 'text': line})
                inputs[relative(document)] = digest(document)
    expected_pending = ['documentation_disposition', 'independent_completion_review', 'owned_scratch_cleanup']
    source_base = '_scratch/i011-20261020/completion-20261022/'
    def archived_latest(source: str) -> dict[str, Any]:
        rows = [row for row in latest['records'] if row['source'] == source]
        if len(rows) != 1:
            raise ValueError('latest archived source must have exactly one record: ' + source)
        path = archive_path(rows[0]['archive'], latest_path)
        data = json.loads(gzip.decompress(path.read_bytes()))
        if not isinstance(data, dict):
            raise ValueError('latest archived JSON must be an object: ' + source)
        return data
    latest_checks: dict[str, Any] = {}
    try:
        driver = archived_latest(source_base + 'specialized-final/status.json')
        frozen = archived_latest(source_base + 'specialized-final/frozen.json')
        audit = archived_latest(source_base + 'specialized-final-audit.json')
        latest_checks = {
            'manifest_issue': latest['issue'] == 'I011',
            'manifest_source': latest['source_sha256'] == args.source_sha256,
            'code_and_performance_passed': latest['code_and_performance_status'] == 'passed',
            'driver_performance_pass': driver['state'] == 'performance-pass',
            'driver_source': driver['source_sha256'] == args.source_sha256,
            'frozen_semantic_source': frozen['semantic_source_sha256'] == args.source_sha256,
            'raw_audit_passed': audit['audit_status'] == 'all_code_and_performance_checks_passed',
            'raw_audit_source': audit['source_sha256'] == args.source_sha256,
            'immutable_manifest_keeps_completion_pending': latest['i011_complete'] is False,
            'immutable_pending_gates': latest['pending'] == expected_pending,
        }
        for name, passed in latest_checks.items():
            if not passed:
                failures.append({'kind': 'latest_qualification_contract', 'check': name})
    except (KeyError, ValueError, TypeError, OSError) as error:
        failures.append({'kind': 'latest_qualification_contract', 'error': str(error)})
    completion: dict[str, Any] = {'status': 'pending', 'pending': expected_pending,
                                  'reason': 'Immutable final manifest deliberately retains documentation, independent review, and cleanup gates.'}
    if args.completion_receipt:
        receipt_path = local(args.completion_receipt, EVIDENCE)
        receipt = read_json(receipt_path)
        inputs[relative(receipt_path)] = digest(receipt_path)
        checks = {'issue': receipt.get('issue') == 'I011',
                  'manifest_binding': receipt.get('manifest') == {'path': relative(latest_path), 'sha256': digest(latest_path)},
                  'source_binding': receipt.get('source_sha256') == args.source_sha256,
                  'gates': receipt.get('resolved_gates') == expected_pending,
                  'complete': receipt.get('i011_complete') is True,
                  'evidence_nonempty': isinstance(receipt.get('evidence'), list) and bool(receipt['evidence'])}
        for entry in receipt.get('evidence', []):
            try:
                path = local(ROOT / entry['path'], EVIDENCE)
                if digest(path) != entry['sha256']:
                    failures.append({'kind': 'completion_receipt_evidence_hash', 'path': relative(path)})
                inputs[relative(path)] = digest(path)
            except (KeyError, OSError, ValueError) as error:
                failures.append({'kind': 'completion_receipt_evidence', 'error': str(error)})
        for name, passed in checks.items():
            if not passed:
                failures.append({'kind': 'completion_receipt_contract', 'check': name})
        if all(checks.values()):
            completion = {'status': 'declared_complete', 'receipt': relative(receipt_path),
                          'limits': 'Receipt identities and declared gate resolution verified; final review judgment remains primary authority.'}
    if completion_sentences and completion['status'] == 'pending':
        failures.append({'kind': 'unsupported_maintained_completion_claim', 'claims': completion_sentences})
    # Detect mutation during this read-only audit; historical scratch/source bytes are never consulted.
    for name, expected in sorted(inputs.items()):
        if digest(ROOT / name) != expected:
            failures.append({'kind': 'audit_input_changed', 'path': name})
    report = {'issue': 'I011', 'state': 'passed' if not failures else 'failed', 'audit_script_sha256': digest(Path(__file__)),
              'latest_manifest': relative(latest_path), 'latest_source_sha256': args.source_sha256,
              'json_files': len(json_paths), 'gzip_files': len(actual_payloads), 'declared_archive_paths': len(declarations),
              'declared_archive_references': sum(len(rows) for rows in declarations.values()),
              'cross_set_links': cross_links, 'markdown_evidence_links': markdown, 'identity_only_and_exclusions': exclusions,
              'existing_review_receipts': reviews, 'historical_and_latest_claims': claims,
              'resolved_origin_candidates': resolved_origin_candidates,
              'origin_review': {'path': origin_review.path, 'sha256': origin_review.sha256},
              'maintained_completion_sentences': completion_sentences, 'latest_qualification_checks': latest_checks, 'i011_completion': completion,
              'input_sha256': inputs, 'warnings': warnings, 'findings': failures,
              'limits': ['Declared hashes and gzip CRCs are verified; no historical source path is compared with current source bytes.',
                         'Hash-only binaries and excluded regenerable guests are not missing raw evidence.',
                         'STE detection uses source paths, PDF magic, and text signatures; any candidate requires origin review.',
                         'The concrete final manifest, archived driver/freeze/raw audit, and optional additive receipt are checked; final review judgment remains independent.',
                         'Historical pending/nonpass records remain historical; they are not failures of latest qualification.']}
    with output.open('x') as stream:
        stream.write(json.dumps(report, indent=2, sort_keys=True) + '\n')
    return 0 if not failures else 1


if __name__ == '__main__':
    raise SystemExit(main())
