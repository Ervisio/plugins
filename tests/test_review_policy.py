import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('policy', Path(__file__).parents[1] / 'scripts/review-policy.py')
policy = importlib.util.module_from_spec(spec)
spec.loader.exec_module(policy)


class PolicyTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.git('init', '-q')
        self.git('config', 'user.name', 'Test')
        self.git('config', 'user.email', 'test@localhost')
        (self.root / 'plugins').mkdir()
        self.old = dict(id='hello', repo='Ervisio/plugin-hello', version='1.0.0', manifest=dict(
            capabilities={'commands': []}, visibleTo={}, platforms=['linux']))
        self.source = dict(id='hello', repo=self.old['repo'], trust='team')
        self.write('registry.json', {'plugins': [self.source]})
        self.write('plugins/hello.json', self.old)
        self.commit()
        self.base = self.git('rev-parse', 'HEAD')
        self.original_git = policy.git
        policy.git = self.git

    def tearDown(self):
        policy.git = self.original_git
        self.tmp.cleanup()

    def git(self, *args):
        return subprocess.check_output(['git', '-C', str(self.root), *args], text=True).strip()

    def write(self, path, value):
        (self.root / path).parent.mkdir(parents=True, exist_ok=True)
        (self.root / path).write_text(json.dumps(value))

    def commit(self):
        self.git('add', '.')
        self.git('commit', '-qm', 'fixture')

    def allowed(self, change=None, extra=False):
        new = json.loads(json.dumps(self.old)); new['version'] = '1.0.1'
        if change: change(new)
        self.write('plugins/hello.json', new)
        if extra: self.write('scripts.json', {})
        self.commit()
        return policy.eligible(self.base, self.git('rev-parse', 'HEAD'))

    def test_same_permissions(self):
        self.assertTrue(self.allowed())

    def test_new_permissions(self):
        self.assertFalse(self.allowed(lambda p: p['manifest']['capabilities'].update(network=['example.com'])))

    def test_unknown_permission_field(self):
        self.assertFalse(self.allowed(lambda p: p['manifest']['capabilities'].update(futurePower=True)))

    def test_visibility(self):
        self.assertFalse(self.allowed(lambda p: p['manifest'].update(visibleTo={'groups': ['sudo']})))

    def test_platforms(self):
        self.assertFalse(self.allowed(lambda p: p['manifest'].update(platforms=['linux', 'windows'])))

    def test_rollback(self):
        self.assertFalse(self.allowed(lambda p: p.update(version='0.9.0')))

    def test_source_swap(self):
        self.assertFalse(self.allowed(lambda p: p.update(repo='someone/plugin-hello')))

    def test_extra_files(self):
        self.assertFalse(self.allowed(extra=True))

    def test_community(self):
        self.source['trust'] = 'community'
        self.write('registry.json', {'plugins': [self.source]}); self.commit()
        self.base = self.git('rev-parse', 'HEAD')
        self.assertFalse(self.allowed())

    def test_first_release(self):
        self.git('rm', '-q', 'plugins/hello.json'); self.commit()
        self.base = self.git('rev-parse', 'HEAD')
        with self.assertRaises(subprocess.CalledProcessError):
            self.allowed()


if __name__ == '__main__': unittest.main()
