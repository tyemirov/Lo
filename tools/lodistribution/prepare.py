"""Prepare pinned package inputs. Network access requires --download."""
from __future__ import annotations
import argparse
import hashlib
import io
import json
from pathlib import Path
import shutil
import struct
import sys
import tarfile
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
PLATFORM = ROOT / 'experiments/native-runtime-abi/platform-inputs.json'
DEPENDENCIES = ROOT / 'docs/evidence/native-runtime-abi/linux-host-dependencies.json'
TOOLS = {'clang': 'clang-21', 'ld.lld': 'lld', 'ld64.lld': 'lld'}
BASE_LIBRARIES = {'libc.so.6', 'libm.so.6', 'ld-linux-aarch64.so.1'}


def local(path: Path) -> Path:
    if not path.is_absolute():
        path = ROOT / path
    resolved = path.resolve()
    if resolved == ROOT or not resolved.is_relative_to(ROOT):
        raise ValueError(f'path must be strictly inside Lo: {path}')
    for part in (path, *path.parents):
        if part == ROOT:
            break
        if part.is_symlink():
            raise ValueError(f'symlink path is forbidden: {part}')
    return resolved


def digest(path: Path) -> str:
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def pin(path: Path) -> dict[str, str]:
    return {'path': str(path.relative_to(ROOT)), 'sha256': digest(path)}


def verified(record: dict[str, str]) -> Path:
    path = local(Path(record['path']))
    if digest(path) != record['sha256']:
        raise ValueError(f'input digest differs: {path}')
    return path


def tree_digest(root: Path) -> str:
    hasher = hashlib.sha256()
    for path in sorted(root.rglob('*')):
        if path.is_file():
            path = local(path)
            hasher.update(path.relative_to(root).as_posix().encode() + b'\0')
            hasher.update(bytes.fromhex(digest(path)))
    return hasher.hexdigest()


def validate_prepared(record: dict) -> None:
    """Bind every supplied member and inventory to the selected immutable inputs."""
    canonical = json.loads(Path(__file__).with_name('inputs.lock.json').read_text())
    platform = json.loads(PLATFORM.read_text())
    dependencies = json.loads(DEPENDENCIES.read_text())
    expected_sources = {(name, item['url']): item for name, source in dependencies['sources'].items() for item in source['files']}
    actual_sources = {(item['source'], item['url']): item for item in record['sources']}
    if len(actual_sources) != len(record['sources']) or set(actual_sources) != set(expected_sources):
        raise ValueError('prepared source inventory differs from canonical inputs')
    for key, expected in expected_sources.items():
        actual = actual_sources[key]
        if any(actual[field] != expected[field] for field in ('url', 'sha256', 'bytes')):
            raise ValueError(f'prepared source inventory pin differs: {key}')
        verified(actual)
    if record['llvm'] != '21.1.8' or set(record['tools']) != set(canonical['tools']):
        raise ValueError('prepared tool inventory differs from canonical inputs')
    for host, tools in canonical['tools'].items():
        if set(record['tools'][host]) != set(tools):
            raise ValueError(f'prepared tool inventory differs: {host}')
        for name, sha256 in tools.items():
            actual = record['tools'][host][name]
            if actual['sha256'] != sha256:
                raise ValueError(f'prepared tool digest differs from canonical input: {host} {name}')
            verified(actual)
    if set(record['host_libraries']) != set(canonical['host_libraries']):
        raise ValueError('prepared host library inventory differs from canonical inputs')
    for name, sha256 in canonical['host_libraries'].items():
        actual = record['host_libraries'][name]
        if actual['sha256'] != sha256:
            raise ValueError(f'prepared host library digest differs from canonical input: {name}')
        verified(actual)
    if record['elf_dependencies'] != canonical['elf_dependencies']:
        raise ValueError('prepared ELF dependency graph differs from canonical inputs')
    expected_archives = {'llvm-linux': platform['host_tools']['aarch64-unknown-linux-gnu'],
                         'llvm-darwin': platform['host_tools']['aarch64-apple-darwin']}
    if set(record['archives']) != set(expected_archives):
        raise ValueError('prepared LLVM archive inventory differs from canonical inputs')
    for name, expected in expected_archives.items():
        if any(record['archives'][name][field] != expected[field] for field in ('url', 'sha256', 'bytes')):
            raise ValueError(f'prepared LLVM archive pin differs: {name}')
    expected_packages = {item['Package']: item for item in dependencies['packages']}
    actual_packages = {item['Package']: item for item in record['packages']}
    if len(actual_packages) != len(record['packages']) or set(actual_packages) != set(expected_packages):
        raise ValueError('prepared Debian package inventory differs from canonical inputs')
    for name, expected in expected_packages.items():
        actual = actual_packages[name]
        if {key: value for key, value in actual.items() if key != 'archive'} != expected:
            raise ValueError(f'prepared Debian package pin differs: {name}')
        if actual['archive']['sha256'] != expected['SHA256']:
            raise ValueError(f'prepared Debian archive digest differs: {name}')
        verified(actual['archive'])


def fetch(state: Path, name: str, record: dict, download: bool) -> Path:
    path = local(state / 'downloads' / name)
    path.parent.mkdir(parents=True, exist_ok=True)
    if not path.exists():
        if not download:
            raise ValueError(f'missing {name}; explicit preparation requires --download')
        partial = local(path.with_name(path.name + '.part'))
        if not partial.exists():
            request = urllib.request.Request(record['url'], headers={'User-Agent': 'Lo-distribution'})
            with urllib.request.urlopen(request, timeout=120) as response, partial.open('wb') as output:
                shutil.copyfileobj(response, output)
        if digest(partial) != record['sha256'] or partial.stat().st_size != int(record.get('bytes', record.get('Size'))):
            raise ValueError(f'download digest/size differs: {name}')
        partial.replace(path)
    if digest(path) != record['sha256'] or path.stat().st_size != int(record.get('bytes', record.get('Size'))):
        raise ValueError(f'archive digest/size differs: {name}')
    return path


def needed(path: Path) -> list[str]:
    """Read ELF64 DT_NEEDED directly, without a host ELF tool dependency."""
    with path.open('rb') as source:
        header = source.read(64)
        if header[:6] != b'\x7fELF\x02\x01':
            raise ValueError(f'expected little-endian ELF64: {path}')
        offset = struct.unpack_from('<Q', header, 32)[0]
        size, count = struct.unpack_from('<HH', header, 54)
        segments = []
        for index in range(count):
            source.seek(offset + index * size)
            segments.append(struct.unpack('<IIQQQQQQ', source.read(56)))
        dynamic = next(segment for segment in segments if segment[0] == 2)
        source.seek(dynamic[2])
        entries = [struct.unpack('<qQ', source.read(16)) for _ in range(dynamic[5] // 16)]
        string_address = next(value for tag, value in entries if tag == 5)
        load = next(segment for segment in segments if segment[0] == 1 and segment[3] <= string_address < segment[3] + segment[5])
        strings = load[2] + string_address - load[3]
        result = []
        for tag, value in entries:
            if tag == 1:
                source.seek(strings + value)
                chars = bytearray()
                while (byte := source.read(1)) != b'\0':
                    if not byte:
                        raise ValueError(f'unterminated ELF library name: {path}')
                    chars.extend(byte)
                result.append(chars.decode('ascii'))
        return result


def extract_deb(source: Path, output: Path) -> None:
    with source.open('rb') as archive:
        if archive.read(8) != b'!<arch>\n':
            raise ValueError(f'invalid Debian archive: {source}')
        while header := archive.read(60):
            name = header[:16].decode().strip().rstrip('/')
            size = int(header[48:58])
            content = archive.read(size)
            if size % 2:
                archive.read(1)
            if name.startswith('data.tar'):
                with tarfile.open(fileobj=io.BytesIO(content)) as members:
                    for member in members:
                        if member.isfile():
                            destination = local(output / member.name)
                            destination.parent.mkdir(parents=True, exist_ok=True)
                            with members.extractfile(member) as original, destination.open('wb') as target:
                                shutil.copyfileobj(original, target)
                return
    raise ValueError(f'Debian archive lacks data: {source}')


def prepare(state: Path, download: bool, darwin_tools: Path) -> dict:
    if not state.is_relative_to(ROOT / '_scratch'):
        raise ValueError('preparation state must be under Lo/_scratch')
    state.mkdir(parents=True, exist_ok=True)
    platform = json.loads(PLATFORM.read_text())
    dependencies = json.loads(DEPENDENCIES.read_text())
    linux = platform['host_tools']['aarch64-unknown-linux-gnu']
    archive = fetch(state, 'llvm-linux.tar.xz', linux, download)
    tools_dir = local(state / 'linux-tools')
    tools_dir.mkdir(exist_ok=True)
    with tarfile.open(archive, 'r|xz') as members:
        selected = set(TOOLS.values())
        found = set()
        for member in members:
            path = Path(member.name)
            if path.parent.name == 'bin' and path.name in selected and member.isfile():
                member.name = path.name
                members.extract(member, path=tools_dir, filter='data')
                found.add(path.name)
                if found == selected:
                    break
        if found != selected:
            raise ValueError(f'LLVM archive lacks tools: {sorted(selected - found)}')
    host_root = local(state / 'linux-libraries')
    host_root.mkdir(exist_ok=True)
    package_pins = []
    for package in dependencies['packages']:
        record = dict(package, sha256=package['SHA256'])
        deb = fetch(state, package['Filename'].split('/')[-1], record, download)
        extract_deb(deb, host_root)
        package_pins.append(dict(package, archive=pin(deb)))
    candidates = {}
    for path in host_root.rglob('*'):
        if path.is_file() and '.so.' in path.name:
            with path.open('rb') as source:
                if source.read(4) == b'\x7fELF':
                    candidates[path.name] = path
    libraries: dict[str, dict] = {}
    edges: dict[str, list[str]] = {}
    queue = [tools_dir / name for name in set(TOOLS.values())]
    while queue:
        path = queue.pop()
        dependencies_needed = needed(path)
        edges[path.name] = dependencies_needed
        for name in dependencies_needed:
            if name in BASE_LIBRARIES or name in libraries:
                continue
            matches = [value for key, value in candidates.items() if key == name or key.startswith(name + '.')]
            if len(matches) != 1:
                raise ValueError(f'undeclared ELF dependency {name} required by {path.name}: {matches}')
            libraries[name] = pin(matches[0])
            queue.append(matches[0])
    sources = []
    for name, source in dependencies['sources'].items():
        for record in source['files']:
            path = fetch(state, record['url'].split('/')[-1], record, download)
            sources.append(dict(record, source=name, **pin(path)))
    darwin = json.loads(darwin_tools.read_text())
    for record in darwin['tools'].values():
        verified(record)
    result = {'llvm': '21.1.8', 'tools': {'darwin/arm64': {name: darwin['tools'][name] for name in TOOLS},
              'linux/arm64': {name: pin(tools_dir / original) for name, original in TOOLS.items()}},
              'host_libraries': libraries, 'elf_dependencies': edges, 'archives': {'llvm-linux': dict(linux, **pin(archive)),
              'llvm-darwin': darwin['archive']}, 'packages': package_pins, 'sources': sources}
    validate_prepared(result)
    (state / 'prepared.json').write_text(json.dumps(result, indent=2) + '\n')
    return {'prepared': str((state / 'prepared.json').relative_to(ROOT)), 'host_libraries': sorted(libraries)}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument('--state', type=Path, required=True)
    parser.add_argument('--darwin-tools', type=Path, default=Path('_scratch/native-runtime/tools.json'))
    parser.add_argument('--download', action='store_true')
    args = parser.parse_args()
    try:
        print(json.dumps(prepare(local(args.state), args.download, local(args.darwin_tools))))
        return 0
    except (ValueError, OSError, KeyError, tarfile.TarError) as error:
        print(f'distribution preparation: {error}', file=sys.stderr)
        return 1

if __name__ == '__main__':
    sys.exit(main())
