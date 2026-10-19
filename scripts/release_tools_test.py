"""Regression checks for the tag policy protecting the publication workflow."""
from pathlib import Path
import subprocess
import sys
import unittest


class TagPolicyTest(unittest.TestCase):
    def test_accepted(self):
        for tag in ("v0.1.0", "v1.0.0", "v0.2.0-rc.1", "v1.2.3-alpha-beta.0"):
            with self.subTest(tag=tag):
                self.assertEqual(self.check(tag).returncode, 0)

    def test_rejected(self):
        for tag in ("v2.0.0", "v01.2.3", "v1.02.3", "v1.2.03", "1.2.3", "v1.2.3.4",
                    "v1.2.3-01", "v1.2.3-", "v1.2.3-alpha..1", "v1.2.3;echo bad", "v1.2.3+meta"):
            with self.subTest(tag=tag):
                self.assertNotEqual(self.check(tag).returncode, 0)

    @staticmethod
    def check(tag):
        return subprocess.run([sys.executable, str(Path(__file__).with_name("check-release-tag.py")), tag], capture_output=True)


if __name__ == "__main__":
    unittest.main()
