#!/usr/bin/env python3
"""Prepare a pinned upstream update on a temporary branch; never publish here."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

MIHOMO = 'MetaCubeX/mihomo'
OPENCONNECT = 'SagerNet/sing-openconnect'
MODULE = 'github.com/sagernet/sing-openconnect'


def run(*args, cwd=None, data=None):
    return subprocess.check_output(args, cwd=cwd, input=data, text=True).strip()


def api(endpoint, optional=False):
    result = subprocess.run(['gh', 'api', endpoint], text=True, capture_output=True)
    if optional and result.returncode and 'HTTP 404' in result.stderr:
        return None
    if result.returncode:
        raise RuntimeError(f"GitHub API {endpoint}: {result.stderr.strip()}")
    return json.loads(result.stdout)


def pages(endpoint):
    result = json.loads(run('gh', 'api', '--paginate', '--slurp', endpoint))
    return [item for page in result for item in page]


def find_release(repo, tag):
    release = api(f'repos/{repo}/releases/tags/{tag}', optional=True)
    if release is not None:
        return release
    # GitHub's tag endpoint can omit drafts; the authenticated list includes them.
    return next((item for item in pages(f'repos/{repo}/releases?per_page=100')
                 if item['tag_name'] == tag), None)


def stable_version(tag):
    match = re.fullmatch(r'v(\d+)\.(\d+)\.(\d+)', tag)
    return tuple(map(int, match.groups())) if match else None


def ancestor(repo, older, newer):
    result = subprocess.run(['git', '-C', str(repo), 'merge-base', '--is-ancestor', older, newer])
    if result.returncode not in (0, 1):
        raise RuntimeError('cannot check upstream ancestry')
    return result.returncode == 0


def fingerprint(base, lock):
    patch = run('git', 'diff', '--binary', base, '--', '.', ':(exclude)upstream.lock.json')
    inputs = {'patch': patch, 'mihomo': lock['mihomo'], 'openconnect': lock['openconnect'], 'go': lock['go'], 'channel': lock['channel']}
    return hashlib.sha256(json.dumps(inputs, sort_keys=True).encode()).hexdigest()


def select_library(repo, locked, explicit='', current_channel='stable'):
    current = locked['commit']
    if explicit:
        if not re.fullmatch(r'(?:[0-9a-f]{40}|v\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?)', explicit):
            raise ValueError('openconnect_ref must be a full SHA or exact version tag')
        sha = run('git', '-C', str(repo), 'rev-parse', explicit+'^{commit}')
        return sha, 'stable' if stable_version(explicit) else 'candidate'
    tags = run('git', '-C', str(repo), 'tag').splitlines()
    for tag in sorted((t for t in tags if stable_version(t)), key=stable_version, reverse=True):
        sha = run('git', '-C', str(repo), 'rev-parse', tag+'^{commit}')
        # An old release tag must not downgrade a newer pinned commit.
        if ancestor(repo, current, sha) and (sha != current or current_channel != 'stable'):
            return sha, 'stable'
    branch = locked['branch']
    if not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9/._-]*', branch):
        raise ValueError('invalid tracked branch')
    head = run('git', '-C', str(repo), 'rev-parse', 'origin/'+branch+'^{commit}')
    if head != current and ancestor(repo, current, head):
        return head, 'candidate'
    if head != current and not ancestor(repo, current, head):
        raise RuntimeError('sing-openconnect branch history changed; refusing automatic rollback')
    return current, None


def emit(values):
    for key, value in values.items():
        print(f'{key}={value}')
    if os.environ.get('GITHUB_OUTPUT'):
        with open(os.environ['GITHUB_OUTPUT'], 'a') as output:
            for key, value in values.items():
                output.write(f'{key}={value}\n')


def prepare(args):
    if not args.check and run('git', 'status', '--porcelain'):
        raise RuntimeError('upstream preparation requires a clean checkout')
    original = json.loads(Path('upstream.lock.json').read_text())
    lock = json.loads(json.dumps(original))
    base = run('git', 'rev-parse', 'HEAD')
    tag = args.mihomo or api(f'repos/{MIHOMO}/releases/latest')['tag_name']
    if not stable_version(tag):
        raise ValueError('Mihomo version must be an exact stable vMAJOR.MINOR.PATCH tag')
    if stable_version(tag) < stable_version(original['mihomo']['tag']):
        raise ValueError('use Validate with a published source tag to reproduce older Mihomo; tracking does not roll back history')
    mihomo_sha = api(f'repos/{MIHOMO}/commits/{tag}')['sha']
    if tag == original['mihomo']['tag'] and mihomo_sha != original['mihomo']['commit']:
        raise RuntimeError('pinned Mihomo tag moved; refusing update')
    with tempfile.TemporaryDirectory(prefix='mihomo-oc-upstream-') as directory:
        run('git', 'clone', '--quiet', f'https://github.com/{OPENCONNECT}.git', directory)
        oc_sha, channel = select_library(directory, lock['openconnect'], args.openconnect, lock['channel'])
    lock['mihomo'] = {'tag': tag, 'commit': mihomo_sha}
    if channel:
        lock['channel'] = channel
    if args.check:
        emit({'mihomo': tag, 'mihomo_sha': mihomo_sha, 'openconnect_sha': oc_sha, 'channel': lock['channel']})
        return
    if run('go', 'env', 'GOVERSION') != 'go'+lock['go']:
        raise RuntimeError('use the Go version pinned in upstream.lock.json')
    branch = f"automation/upstream-{os.environ.get('GITHUB_RUN_ID', 'local')}-{os.environ.get('GITHUB_RUN_ATTEMPT', '1')}"
    run('git', 'switch', '-c', branch)
    if mihomo_sha != original['mihomo']['commit']:
        run('git', 'fetch', '--no-tags', f'https://github.com/{MIHOMO}.git', mihomo_sha)
        if not ancestor('.', original['mihomo']['commit'], mihomo_sha):
            raise RuntimeError('Mihomo history diverged; refusing automatic merge')
        # The three-way merge retains upstream history and applies the fork changes.
        # Conflicts are deliberately left in the candidate checkout for the job log.
        run('git', 'merge', '--no-ff', '--no-edit', mihomo_sha)
    if oc_sha != original['openconnect']['commit']:
        run('go', 'get', MODULE+'@'+oc_sha)
        run('go', 'mod', 'tidy')
        version = json.loads(run('go', 'list', '-m', '-json', MODULE))['Version']
        lock['openconnect']['version'] = version
        lock['openconnect']['commit'] = oc_sha
    digest = fingerprint(mihomo_sha, lock)
    repo = args.repo or os.environ['GITHUB_REPOSITORY']
    if digest == original.get('inputs_digest'):
        version = original['release']
        release = find_release(repo, version)
        if release and not release['draft']:
            emit({'changed': 'false'})
            return
        # Retry an interrupted draft/publication using exactly the same source tag.
        emit({'changed': 'true', 'sha': base, 'base': base, 'version': version, 'channel': lock['channel']})
        return
    tags = pages(f'repos/{repo}/tags?per_page=100')
    pattern = re.compile(re.escape(tag)+r'-oc\.(\d+)$')
    serials = [int(m.group(1)) for entry in tags if (m := pattern.fullmatch(entry['name']))]
    previous = original.get('release', '')
    if m := pattern.fullmatch(previous):
        serials.append(int(m.group(1)))
    version = f'{tag}-oc.{max(serials, default=0)+1}'
    lock.update(inputs_digest=digest, adapter_commit=base, release=version)
    Path('upstream.lock.json').write_text(json.dumps(lock, indent=2)+'\n')
    run('git', 'add', '-A')
    run('git', 'commit', '-m', f'chore: pin upstream inputs for {version}')
    sha = run('git', 'rev-parse', 'HEAD')
    if args.push:
        run('git', 'push', 'origin', 'HEAD:refs/heads/'+branch)
    emit({'changed': 'true', 'sha': sha, 'base': base, 'version': version, 'channel': lock['channel']})


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--mihomo', default='')
    parser.add_argument('--openconnect', default='')
    parser.add_argument('--repo', default='')
    parser.add_argument('--push', action='store_true')
    parser.add_argument('--check', action='store_true', help='inspect upstream refs without editing this checkout')
    prepare(parser.parse_args())
