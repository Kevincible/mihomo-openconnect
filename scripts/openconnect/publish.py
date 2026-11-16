#!/usr/bin/env python3
"""Publish validated artifacts, without overwriting tags or existing release assets."""
import argparse
import hashlib
import json
from pathlib import Path
import tempfile
import subprocess
from urllib.parse import urlencode
from upstream import api, find_release, run

TARGETS = {(system, arch) for system in ('linux','darwin','windows') for arch in ('amd64','arm64')}


def publish(repo, base, directory):
    lock = json.loads(Path('upstream.lock.json').read_text())
    source = run('git','rev-parse','HEAD')
    version = lock.get('release')
    manifests = [json.loads(p.read_text()) for p in sorted(directory.glob('mihomo-openconnect-*.json'))]
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
    release = find_release(repo, version)
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
            # Use the mutation response: a just-created draft may not be indexed by
            # either the tag endpoint or the releases list yet.
            release = api(f'repos/{repo}/releases', method='POST', body={
                'tag_name': version, 'target_commitish': source, 'name': version,
                'body': body.read_text(), 'draft': True, 'prerelease': lock['channel']=='candidate',
            })
        release_id = release['id']
        print(f'Uploading release {version} by ID {release_id}', flush=True)
        existing = {asset['name']: asset for asset in release['assets']}
        for path in sorted(directory.iterdir()):
            if not path.is_file(): continue
            if path.name in existing:
                asset = existing[path.name]
                digest = 'sha256:'+hashlib.sha256(path.read_bytes()).hexdigest()
                if asset.get('digest') == digest and asset.get('state') == 'uploaded':
                    continue
                downloaded = Path(temporary)/path.name
                with downloaded.open('wb') as output:
                    subprocess.run(['gh','api',f"repos/{repo}/releases/assets/{asset['id']}",
                                    '-H','Accept: application/octet-stream'], stdout=output, check=True)
                if downloaded.read_bytes() != path.read_bytes():
                    raise RuntimeError('refusing to overwrite a different release asset: '+path.name)
            else:
                upload_url = f'https://uploads.github.com/repos/{repo}/releases/{release_id}/assets?'+urlencode({'name':path.name})
                run('gh','api',upload_url,'--method','POST','--input',str(path),
                    '-H','Content-Type: application/octet-stream','-H',f'Content-Length: {path.stat().st_size}')
        published = api(f'repos/{repo}/releases/{release_id}', method='PATCH', body={
            'draft': False, 'prerelease': lock['channel']=='candidate',
            'make_latest': str(lock['channel']=='stable').lower(),
        })
        if published['draft']:
            raise RuntimeError('GitHub did not publish the draft')
    print(f'https://github.com/{repo}/releases/tag/{version}')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--repo', required=True)
    parser.add_argument('--base', required=True)
    parser.add_argument('--artifacts',type=Path,default=Path('dist'))
    args = parser.parse_args()
    publish(args.repo,args.base,args.artifacts)
