import json
import os
from pathlib import Path
import tempfile
import unittest

from upstream import select_library, fingerprint, run
from publish import publish, TARGETS


class UpstreamHistoryTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.repo = Path(self.temp.name)
        run('git', 'init', '-q', str(self.repo))
        self.git('config','user.name','Test')
        self.git('config','user.email','test@example.invalid')
        self.old = self.commit('first')
        self.git('tag','v0.1.0')
        self.current = self.commit('pinned')
        self.git('update-ref','refs/remotes/origin/main',self.current)
        self.locked = {'commit':self.current,'branch':'main'}

    def git(self,*args):
        return run('git','-C',str(self.repo),*args)

    def commit(self, text):
        (self.repo/'source').write_text(text)
        self.git('add','source')
        self.git('commit','-qm',text)
        return self.git('rev-parse','HEAD')

    def test_old_tag_does_not_downgrade_pseudo_version(self):
        self.assertEqual(select_library(self.repo,self.locked), (self.current,None))

    def test_branch_commit_is_candidate_and_tag_promotes_it(self):
        head = self.commit('next')
        self.git('update-ref','refs/remotes/origin/main',head)
        self.assertEqual(select_library(self.repo,self.locked),(head,'candidate'))
        self.git('tag','v0.1.1')
        self.assertEqual(select_library(self.repo,self.locked),(head,'stable'))
        self.locked['commit'] = head
        self.assertEqual(select_library(self.repo,self.locked,current_channel='candidate'),(head,'stable'))
        next_head = self.commit('after-tag')
        self.git('update-ref','refs/remotes/origin/main',next_head)
        self.assertEqual(select_library(self.repo,self.locked),(next_head,'candidate'))

    def test_rewritten_branch_fails_closed(self):
        self.git('update-ref','refs/remotes/origin/main',self.old)
        with self.assertRaisesRegex(RuntimeError,'history changed'):
            select_library(self.repo,self.locked)

    def test_input_digest_ignores_release_state_but_detects_code(self):
        previous = os.getcwd()
        os.chdir(self.repo)
        self.addCleanup(os.chdir, previous)
        lock = {'mihomo':{'commit':self.old},'openconnect':self.locked,'go':'1.27.3','channel':'stable'}
        first = fingerprint(self.old,lock)
        Path('upstream.lock.json').write_text(json.dumps(lock))
        self.git('add','upstream.lock.json')
        self.git('commit','-qm','release bookkeeping')
        self.assertEqual(first,fingerprint(self.old,lock))
        self.commit('feature change')
        self.assertNotEqual(first,fingerprint(self.old,lock))


class PublicationGuardTest(unittest.TestCase):
    def test_incomplete_platform_set_cannot_publish(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaisesRegex(RuntimeError,'six platform'):
                publish('unused/repo','unused',Path(directory))

    def test_mixed_source_cannot_publish(self):
        lock=json.loads(Path('upstream.lock.json').read_text())
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            for system,arch in TARGETS:
                (root/f'mihomo-openconnect-{system}-{arch}.json').write_text(json.dumps({
                    'target':{'os':system,'arch':arch},'source_commit':'wrong','inputs':lock}))
            with self.assertRaisesRegex(RuntimeError,'mixed source'):
                publish('unused/repo','unused',root)
