"""Regression checks for links in the release documentation bundle."""
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("verify_release", Path(__file__).with_name("verify-release.py"))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


class DocumentationLinksTest(unittest.TestCase):
    def test_relative_links_and_anchors(self):
        release.verify_doc_links({
            "README.md": b"# Portbook\n[Guide](docs/GUIDE.md#port-pool)\n[Online](https://example.com)\n",
            "docs/GUIDE.md": b"# Guide\n## Port pool\n[Back](../README.md)\n[Local](#port-pool)\n",
        })

    def test_missing_packaged_document(self):
        with self.assertRaisesRegex(RuntimeError, "broken packaged link"):
            release.verify_doc_links({"README.md": b"[Guide](docs/GUIDE.md)"})

    def test_missing_heading(self):
        with self.assertRaisesRegex(RuntimeError, "broken packaged anchor"):
            release.verify_doc_links({"README.md": b"# Portbook\n[Old](#old-heading)"})

    def test_link_outside_archive(self):
        with self.assertRaisesRegex(RuntimeError, "broken packaged link"):
            release.verify_doc_links({"README.md": b"[Outside](../README.md)"})


if __name__ == "__main__":
    unittest.main()
