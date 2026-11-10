"""Build the Darwin Process boundary with recorded SDK declarations."""
from __future__ import annotations

from dataclasses import dataclass
from collections.abc import Callable
import hashlib
from pathlib import Path
import shlex
import struct
import subprocess


TARGET = 'aarch64-apple-darwin'
DEPLOYMENT = '27.0'
SOURCES = {
    'process_es': 'runtime/native/src/process_es.c',
    'process_bootstrap': 'runtime/native/src/process_bootstrap.c',
    'execution_token': 'runtime/native/host/process/execution_token.c',
}


def compiler_flags(sdk: str) -> list[str]:
    return ['-target', TARGET, '-mmacosx-version-min=' + DEPLOYMENT,
            '-isysroot', sdk, '-std=c11', '-O2', '-fPIC', '-fblocks',
            '-fno-modules', '-Wall', '-Wextra', '-Werror',
            '-Wno-error=deprecated-declarations', '-iquote', 'runtime/native/include']


def sha256(path: Path) -> str:
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def owned(root: Path, path: Path) -> Path:
    resolved = path.resolve()
    if not resolved.is_relative_to(root):
        raise ValueError(f'Process build output leaves Lo: {path}')
    return resolved


def invoke(root: Path, work: Path, arguments: list[str]) -> str:
    result = subprocess.run(arguments, cwd=root, stdin=subprocess.DEVNULL,
                            env={'CI': '1', 'PATH': '/usr/bin:/bin', 'TMPDIR': str(work),
                                 'CLANG_MODULE_CACHE_PATH': str(work / 'module-cache'),
                                 'LC_ALL': 'C', 'ZERO_AR_DATE': '1'},
                            capture_output=True, text=True, check=False)
    if result.returncode:
        raise ValueError(f'Process SDK command failed ({arguments[0]}): {result.stderr}')
    return result.stdout.strip()


@dataclass(frozen=True)
class SDK:
    compiler: Path
    root: Path
    version: str
    compiler_version: str

    @classmethod
    def discover(cls, root: Path, work: Path) -> SDK:
        prefix = ['/usr/bin/xcrun', '--no-cache', '--sdk', 'macosx']
        compiler = Path(invoke(root, work, prefix + ['--find', 'clang']))
        sdk = Path(invoke(root, work, prefix + ['--show-sdk-path']))
        if not compiler.is_absolute() or not sdk.is_absolute():
            raise ValueError('Process SDK tools require absolute paths')
        compiler, sdk = compiler.resolve(strict=True), sdk.resolve(strict=True)
        version = invoke(root, work, prefix + ['--show-sdk-version'])
        compiler_version = invoke(root, work, [str(compiler), '--version'])
        return cls(compiler, sdk, version, compiler_version)

    def identity(self) -> dict[str, object]:
        settings = self.root / 'SDKSettings.json'
        return {
            'compiler': {'path': str(self.compiler), 'sha256': sha256(self.compiler),
                         'version': self.compiler_version},
            'sdk': {'path': str(self.root), 'version': self.version,
                    'settings_sha256': sha256(settings)},
        }


def deployment(path: Path) -> str:
    data = path.read_bytes()
    if len(data) < 32 or struct.unpack_from('<I', data)[0] != 0xfeedfacf:
        raise ValueError(f'Process SDK object is not Mach-O: {path}')
    commands = struct.unpack_from('<I', data, 16)[0]
    offset, selected = 32, None
    for _ in range(commands):
        if offset + 8 > len(data):
            raise ValueError('truncated Process SDK object commands')
        kind, size = struct.unpack_from('<II', data, offset)
        if size < 8 or offset + size > len(data):
            raise ValueError('invalid Process SDK object command size')
        if kind == 0x32:
            if size < 24 or selected is not None:
                raise ValueError('invalid Process SDK deployment command')
            platform, minimum = struct.unpack_from('<II', data, offset + 8)
            if platform != 1 or minimum != 27 << 16:
                raise ValueError('Process SDK object changes the Darwin deployment baseline')
            selected = DEPLOYMENT
        offset += size
    if selected is None:
        raise ValueError('Process SDK object has no deployment command')
    return selected


def build_objects(root: Path, work: Path, names: tuple[str, ...] = tuple(SOURCES)) -> tuple[list[Path], dict[str, object]]:
    root = root.resolve(strict=True)
    work = owned(root, work)
    work.mkdir(parents=True, exist_ok=True)
    sdk = SDK.discover(root, work)
    identity = sdk.identity()
    flags = compiler_flags(str(sdk.root))
    objects, records = [], {}
    for name in names:
        if name not in SOURCES:
            raise ValueError(f'unknown Process SDK source: {name}')
        source = owned(root, root / SOURCES[name])
        output = owned(root, work / (name + '.o'))
        dependencies = owned(root, work / (name + '.d'))
        source_hash = sha256(source)
        invoke(root, work, [str(sdk.compiler), *flags, '-MD', '-MF', str(dependencies),
                            '-c', str(source), '-o', str(output)])
        dependency_text = dependencies.read_text().replace('\\\n', '')
        target, separator, inputs = dependency_text.partition(':')
        if not separator or not target:
            raise ValueError('Process SDK compiler omitted its dependency inventory')
        headers = {}
        for name_text in shlex.split(inputs):
            path = Path(name_text).resolve(strict=True)
            if path == source:
                continue
            label = path.relative_to(root).as_posix() if path.is_relative_to(root) else str(path)
            headers[label] = sha256(path)
        if sha256(source) != source_hash:
            raise ValueError('Process SDK source changed during compilation')
        records[name] = {'source': {'path': SOURCES[name], 'sha256': source_hash},
                         'headers': dict(sorted(headers.items())), 'flags': flags,
                         'object_sha256': sha256(output), 'deployment': deployment(output)}
        objects.append(output)
    if sdk.identity() != identity:
        raise ValueError('Process SDK tool identity changed during compilation')
    return objects, {**identity, 'objects': records}


def validate_receipt(record: object, local_digest: Callable[[str], str]) -> None:
    """Validate producer evidence without opening its external SDK paths."""
    if not isinstance(record, dict) or set(record) != {'compiler', 'sdk', 'objects'}:
        raise ValueError('Process SDK receipt field set differs')
    compiler, sdk, objects = record['compiler'], record['sdk'], record['objects']
    if not isinstance(compiler, dict) or set(compiler) != {'path', 'sha256', 'version'}:
        raise ValueError('Process SDK compiler identity differs')
    if not isinstance(sdk, dict) or set(sdk) != {'path', 'version', 'settings_sha256'}:
        raise ValueError('Process SDK identity differs')
    if not isinstance(objects, dict) or set(objects) != set(SOURCES):
        raise ValueError('Process SDK object inventory differs')
    hashes = [compiler['sha256'], sdk['settings_sha256']]
    for identity in (compiler, sdk):
        path = identity['path']
        if not isinstance(path, str) or '\0' in path or not Path(path).is_absolute() or str(Path(path)) != path or '..' in Path(path).parts:
            raise ValueError('Process SDK producer path is invalid')
        if not isinstance(identity['version'], str) or not identity['version']:
            raise ValueError('Process SDK producer identity is invalid')
    for name, source in SOURCES.items():
        item = objects[name]
        if not isinstance(item, dict) or set(item) != {'source', 'headers', 'flags', 'object_sha256', 'deployment'}:
            raise ValueError('Process SDK object receipt differs')
        if item['source'] != {'path': source, 'sha256': local_digest(source)}:
            raise ValueError('Process SDK source identity differs')
        if item['deployment'] != DEPLOYMENT or item['flags'] != compiler_flags(sdk['path']):
            raise ValueError('Process SDK deployment identity differs')
        if not isinstance(item['headers'], dict) or not item['headers']:
            raise ValueError('Process SDK header inventory is empty')
        for name, expected_hash in item['headers'].items():
            if not isinstance(name, str) or not name or '\0' in name or str(Path(name)) != name or '..' in Path(name).parts:
                raise ValueError('Process SDK header path is invalid')
            if not Path(name).is_absolute():
                if not name.startswith('runtime/native/') or local_digest(name) != expected_hash:
                    raise ValueError('Process SDK local header identity differs')
        hashes.extend([item['object_sha256'], *item['headers'].values()])
    for value in hashes:
        if not isinstance(value, str) or len(value) != 64 or any(character not in '0123456789abcdef' for character in value):
            raise ValueError('Process SDK receipt contains an invalid SHA-256')
