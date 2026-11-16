import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from upstream import select_library, fingerprint, find_release, run
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


class DraftReleaseTest(unittest.TestCase):
    @patch('upstream.pages')
    @patch('upstream.api')
    def test_draft_not_visible_through_tag_endpoint(self, api_mock, pages_mock):
        draft = {'tag_name': 'v1.19.34-oc.1', 'draft': True, 'id': 1}
        api_mock.return_value = None
        pages_mock.return_value = [draft]
        self.assertEqual(find_release('owner/repo', draft['tag_name']), draft)

    @patch('upstream.pages')
    @patch('upstream.api')
    def test_public_release_does_not_need_draft_scan(self, api_mock, pages_mock):
        release = {'tag_name': 'v1.19.34-oc.1', 'draft': False, 'id': 1}
        api_mock.return_value = release
        self.assertEqual(find_release('owner/repo', release['tag_name']), release)
        pages_mock.assert_not_called()
