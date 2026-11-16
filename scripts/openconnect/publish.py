#!/usr/bin/env python3
"""Publish validated artifacts, without overwriting tags or existing release assets."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
from upstream import api, run

TARGETS = {(system, arch) for system in ('linux','darwin','windows') for arch in ('amd64','arm64')}


def publish(repo, base, directory):
    lock = json.loads(Path('upstream.lock.json').read_text())
    source = run('git','rev-parse','HEAD')
    version = lock.get('release')
    manifests = [json.loads(p.read_text()) for p in directory.glob('mihomo-openconnect-*.json')]
    if {(m['target']['os'],m['target']['arch']) for m in manifests} != TARGETS or len(manifests) != len(TARGETS):
        raise RuntimeError('release requires all six platform manifests')
    for manifest in manifests:
        if manifest['source_commit'] != source or manifest['inputs'] != lock:
            raise RuntimeError('mixed source commits or dependency inputs in artifacts')
        if not version:
            raise RuntimeError('release inputs have not been prepared')
        target = manifest['target']
        name = f"mihomo-openconnect-{version}-{target['os']}-{target['arch']}"
        extension = '.zip' if target['os']=='windows' else '.tar.gz'
        if not (directory/(name+extension)).is_file():
            raise RuntimeError('missing platform archive')
    (directory/'version-manifest.json').write_text(json.dumps({'source_commit':source,'inputs':lock,'builds':manifests},indent=2)+'\n')
    files = sorted(p for p in directory.iterdir() if p.is_file() and p.name != 'SHA256SUMS')
    (directory/'SHA256SUMS').write_text(''.join(f'{hashlib.sha256(p.read_bytes()).hexdigest()}  {p.name}\n' for p in files))
    release = api(f'repos/{repo}/releases/tags/{version}',optional=True)
    if release and not release['draft']:
        raise RuntimeError('release is already published; it is immutable')
    remote_main = run('git','ls-remote','origin','refs/heads/main').split()[0]
    if remote_main not in (base, source):
        raise RuntimeError('main advanced while validation ran; rerun tracking on the new head')
    remote_tag = run('git','ls-remote','origin','refs/tags/'+version,'refs/tags/'+version+'^{}')
    if remote_tag:
        run('git','fetch','origin','refs/tags/'+version)
        if run('git','rev-parse','FETCH_HEAD^{commit}') != source:
            raise RuntimeError('release tag already points to another commit')
        run('git','push','origin',source+':refs/heads/main')
    else:
        run('git','tag','-a',version,source,'-m',version)
        run('git','push','--atomic','origin',source+':refs/heads/main','refs/tags/'+version)
    with tempfile.TemporaryDirectory() as temporary:
        body = Path(temporary)/'notes.md'
        body.write_text(f"Native OpenConnect outbound for Mihomo. All release gates passed, including the ocserv interoperability and lifecycle suite.\n\n"
                        f"- Mihomo: `{lock['mihomo']['tag']}` (`{lock['mihomo']['commit']}`)\n"
                        f"- sing-openconnect: `{lock['openconnect']['version']}` (`{lock['openconnect']['commit']}`)\n"
                        f"- Adaptation input: `{lock['adapter_commit']}`\n- Complete source: `{source}`\n"
                        f"- Go: `{lock['go']}`; CGO disabled; `with_gvisor`\n\n"
                        "See BUILD.json in each archive and version-manifest.json for all build inputs. "
                        "Corporate authentication, system TUN and iOS host integration require their own environment validation.\n")
        if not release:
            command = ['gh','release','create',version,'--repo',repo,'--verify-tag','--draft','--title',version,'--notes-file',str(body)]
            if lock['channel']=='candidate': command.append('--prerelease')
            run(*command)
        release = api(f'repos/{repo}/releases/tags/{version}')
        existing = {asset['name'] for asset in release['assets']}
        for path in sorted(directory.iterdir()):
            if not path.is_file(): continue
            if path.name in existing:
                run('gh','release','download',version,'--repo',repo,'--pattern',path.name,'--dir',temporary)
                if (Path(temporary)/path.name).read_bytes() != path.read_bytes():
                    raise RuntimeError('refusing to overwrite a different release asset: '+path.name)
            else:
                run('gh','release','upload',version,str(path),'--repo',repo)
        run('gh','release','edit',version,'--repo',repo,'--draft=false',
            '--latest='+str(lock['channel']=='stable').lower(), '--prerelease='+str(lock['channel']=='candidate').lower())
    print(f'https://github.com/{repo}/releases/tag/{version}')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--repo', required=True)
    parser.add_argument('--base', required=True)
    parser.add_argument('--artifacts',type=Path,default=Path('dist'))
    args = parser.parse_args()
    publish(args.repo,args.base,args.artifacts)
