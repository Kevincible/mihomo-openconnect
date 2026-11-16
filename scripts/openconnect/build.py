#!/usr/bin/env python3
"""Build one CLI archive from fixed module versions, recording its complete inputs."""
import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import time
import zipfile


def output(*args):
    return subprocess.check_output(args, text=True).strip()


def build(target_os, arch, destination):
    lock = json.loads(Path('upstream.lock.json').read_text())
    source = output('git', 'rev-parse', 'HEAD')
    version = lock.get('release', 'development-'+source[:12])
    toolchain = output('go', 'env', 'GOVERSION')
    if toolchain != 'go'+lock['go']:
        raise RuntimeError('toolchain differs from upstream.lock.json')
    module = json.loads(output('go', 'list', '-mod=readonly', '-m', '-json', 'github.com/sagernet/sing-openconnect'))
    if module['Version'] != lock['openconnect']['version'] or module.get('Replace'):
        raise RuntimeError('OpenConnect dependency differs from the lock file')
    timestamp = output('git', 'show', '-s', '--format=%cI', 'HEAD')
    epoch = int(output('git', 'show', '-s', '--format=%ct', 'HEAD'))
    flags = ['-mod=readonly', '-trimpath', '-tags=with_gvisor', '-buildvcs=true',
             '-ldflags=-s -w -X github.com/metacubex/mihomo/constant.Version='+version+
             ' -X github.com/metacubex/mihomo/constant.MihomoName=mihomo-openconnect'+
             ' -X github.com/metacubex/mihomo/constant.BuildTime='+timestamp]
    env = dict(os.environ, GOOS=target_os, GOARCH=arch, CGO_ENABLED='0', GOAMD64='v1', GOTOOLCHAIN='local')
    destination.mkdir(parents=True, exist_ok=True)
    name = f'mihomo-openconnect-{version}-{target_os}-{arch}'
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        binary = root / ('mihomo-openconnect.exe' if target_os == 'windows' else 'mihomo-openconnect')
        subprocess.run(['go', 'build', *flags, '-o', str(binary), '.'], env=env, check=True)
        manifest = {'source_commit': source, 'inputs': lock, 'go': toolchain,
                    'target': {'os': target_os, 'arch': arch, 'CGO_ENABLED': '0', 'GOAMD64': 'v1'},
                    'flags': flags, 'binary_sha256': hashlib.sha256(binary.read_bytes()).hexdigest()}
        (root/'BUILD.json').write_text(json.dumps(manifest, indent=2)+'\n')
        shutil.copy('LICENSE', root/'LICENSE')
        shutil.copy('docs/openconnect/config.example.yaml', root/'config.example.yaml')
        if target_os == 'windows':
            archive = destination/(name+'.zip')
            with zipfile.ZipFile(archive, 'w', zipfile.ZIP_DEFLATED) as zip_file:
                for path in sorted(root.iterdir()):
                    info = zipfile.ZipInfo(path.name, time.gmtime(epoch)[:6])
                    info.compress_type = zipfile.ZIP_DEFLATED
                    info.external_attr = (0o755 if path == binary else 0o644) << 16
                    zip_file.writestr(info, path.read_bytes())
        else:
            archive = destination/(name+'.tar.gz')
            with archive.open('wb') as raw, gzip.GzipFile(filename='', mode='wb', fileobj=raw, mtime=0) as compressed:
                with tarfile.open(fileobj=compressed, mode='w') as tar:
                    for path in sorted(root.iterdir()):
                        info = tar.gettarinfo(str(path), arcname=path.name)
                        info.mtime, info.uid, info.gid = epoch, 0, 0
                        info.uname, info.gname = '', ''
                        info.mode = 0o755 if path == binary else 0o644
                        with path.open('rb') as contents:
                            tar.addfile(info, contents)
        (destination/(name+'.json')).write_text(json.dumps(manifest, indent=2)+'\n')
    print(archive)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--os', required=True, choices=['linux','darwin','windows'])
    parser.add_argument('--arch', required=True, choices=['amd64','arm64'])
    parser.add_argument('--output', type=Path, default=Path('dist'))
    args = parser.parse_args()
    build(args.os, args.arch, args.output)
