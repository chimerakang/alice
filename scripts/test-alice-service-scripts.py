#!/usr/bin/env python3
"""Exercise service scripts with fake commands; never restart the real bot."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent

class ServiceScriptsTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.base = Path(self.tmp.name)
        self.bin = self.base / 'bin'
        self.bin.mkdir()
        self.log = self.base / 'calls'
        self.env = dict(os.environ, PATH=str(self.bin)+os.pathsep+os.environ['PATH'], TEST_CALLS=str(self.log), TEST_LOADED='1', TEST_NOW='1000', ALICE_WATCHDOG_STATE_DIR=str(self.base/'state'))
        self.command('go', '''echo go >> "$TEST_CALLS"
if [ "${TEST_BUILD_FAIL:-0}" = 1 ]; then exit 42; fi
printf new > "$3"
''')
        self.command('launchctl', '''echo "launchctl $*" >> "$TEST_CALLS"
if [ "$1" = print ] && [ "$TEST_LOADED" = 0 ]; then exit 1; fi
''')
        self.command('curl', '''echo "curl $*" >> "$TEST_CALLS"
exit "${TEST_HTTP_FAIL:-0}"
''')
        self.command('date', '''echo "$TEST_NOW"
''')
        self.script = self.base / 'start-bot.sh'
        shutil.copy2(ROOT/'start-bot.sh', self.script)
        (self.base/'alice').write_text('old')

    def command(self, name, body):
        p = self.bin/name
        p.write_text('#!/bin/bash\nset -eu\n'+body)
        p.chmod(0o755)

    def run_script(self, path):
        return subprocess.run(['bash', str(path)], env=self.env, capture_output=True, text=True)

    def calls(self):
        return self.log.read_text().splitlines() if self.log.exists() else []

    def test_build_before_restart(self):
        result=self.run_script(self.script)
        self.assertEqual(result.returncode,0,result.stderr)
        calls=self.calls()
        self.assertEqual(calls[0],'go')
        self.assertTrue(any('kickstart -k' in s for s in calls))
        self.assertEqual((self.base/'alice').read_text(),'new')
        self.assertTrue(any('--max-time 2' in s for s in calls))

    def test_failed_build_preserves_old_service(self):
        self.env['TEST_BUILD_FAIL']='1'
        result=self.run_script(self.script)
        self.assertNotEqual(result.returncode,0)
        self.assertEqual((self.base/'alice').read_text(),'old')
        self.assertEqual(self.calls(),['go'])
        self.assertFalse(list(self.base.glob('.alice-build.*')))

    def watchdog(self):
        result=self.run_script(ROOT/'scripts/alice-watchdog.sh')
        self.assertEqual(result.returncode,0,result.stderr)

    def restart_count(self):
        return sum('kickstart -k' in s for s in self.calls())

    def test_watchdog_threshold_cooldown_and_recovery(self):
        self.env['TEST_HTTP_FAIL']='22'
        self.watchdog()  # First observed process has a 120-second startup grace.
        self.assertEqual(self.restart_count(),0)
        self.env['TEST_NOW']='1121'
        self.watchdog();self.watchdog()
        self.assertEqual(self.restart_count(),0)
        self.watchdog()
        self.assertEqual(self.restart_count(),1)
        for _ in range(3):self.watchdog()
        self.assertEqual(self.restart_count(),1)
        self.env['TEST_NOW']='1422'
        self.watchdog()
        self.assertEqual(self.restart_count(),2)
        self.env['TEST_HTTP_FAIL']='0'
        self.watchdog()
        self.assertEqual((self.base/'state/failures').read_text().strip(),'0')
        self.assertTrue(any('--max-time 5' in s for s in self.calls()))

    def test_watchdog_respects_unloaded_service(self):
        self.env.update(TEST_HTTP_FAIL='22',TEST_LOADED='0')
        for _ in range(4):self.watchdog()
        self.assertEqual(self.restart_count(),0)
        self.assertFalse(any(s.startswith('curl') for s in self.calls()))

if __name__ == '__main__':
    unittest.main()
