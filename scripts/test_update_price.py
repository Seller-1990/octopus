import contextlib
import io
import json
import tempfile
import unittest
import urllib.error
from pathlib import Path
from unittest import mock

import updatePrice


class UpdatePriceTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="octopus-price-test-")
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        self.output = root / "internal" / "globalprice" / "presets.go"
        self.output.parent.mkdir(parents=True)
        self.original = "existing price presets\n"
        self.output.write_text(self.original, encoding="utf-8")
        location = mock.patch.object(updatePrice, "__file__", str(root / "scripts" / "updatePrice.py"))
        location.start()
        self.addCleanup(location.stop)
        output = contextlib.redirect_stdout(io.StringIO())
        output.__enter__()
        self.addCleanup(output.__exit__, None, None, None)

    def assert_presets_unchanged(self):
        self.assertEqual(self.output.read_text(encoding="utf-8"), self.original)

    def test_network_failure_preserves_existing_presets(self):
        with mock.patch.object(updatePrice.urllib.request, "urlopen", side_effect=urllib.error.URLError("offline")):
            with self.assertRaises(urllib.error.URLError):
                updatePrice.main()
        self.assert_presets_unchanged()

    def test_invalid_json_preserves_existing_presets(self):
        with mock.patch.object(updatePrice.urllib.request, "urlopen", return_value=io.BytesIO(b"not json")):
            with self.assertRaises(json.JSONDecodeError):
                updatePrice.main()
        self.assert_presets_unchanged()

    def test_catalog_without_supported_models_preserves_existing_presets(self):
        catalogs = [{}, {"openai": {"models": {}}}, {"other-provider": {"models": {}}}]
        for catalog in catalogs:
            with self.subTest(catalog=catalog):
                response = io.BytesIO(json.dumps(catalog).encode())
                with mock.patch.object(updatePrice.urllib.request, "urlopen", return_value=response):
                    with self.assertRaises(ValueError):
                        updatePrice.main()
                self.assert_presets_unchanged()

    def test_valid_catalog_preserves_prices_and_deduplicates_models(self):
        catalog = {
            "openai": {"models": {
                "paid": {"id": "audit-model", "cost": {"input": 2, "output": 3}},
                "free": {"id": "audit-free", "cost": {"input": 0, "output": 0}},
            }},
            "google": {"models": {
                "duplicate": {"id": "audit-model", "cost": {"input": 99}},
            }},
        }
        response = io.BytesIO(json.dumps(catalog).encode())
        with mock.patch.object(updatePrice.urllib.request, "urlopen", return_value=response):
            updatePrice.main()
        content = self.output.read_text(encoding="utf-8")
        self.assertEqual(content.count('"audit-model":'), 1)
        self.assertIn('"audit-model": {Input: 2, Output: 3,', content)
        self.assertIn('"audit-free": {Input: 0, Output: 0,', content)


if __name__ == "__main__":
    unittest.main()
