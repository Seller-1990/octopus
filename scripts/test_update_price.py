import contextlib
import io
import json
import os
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
        env = mock.patch.dict(os.environ)
        env.start()
        self.addCleanup(env.stop)
        # 隔离外部环境:默认语义必须是无逃生舱的 fail-loud。
        os.environ.pop(updatePrice.ALLOW_MISSING_PROVIDERS_ENV, None)
        output = contextlib.redirect_stdout(io.StringIO())
        output.__enter__()
        self.addCleanup(output.__exit__, None, None, None)

    def assert_presets_unchanged(self):
        self.assertEqual(self.output.read_text(encoding="utf-8"), self.original)

    @staticmethod
    def full_catalog(**providers):
        """构造含全部 provider 的 catalog(缺失即报错),未指定的 provider 为空模型表。"""
        catalog = {name: {"models": {}} for name in updatePrice.PROVIDERS}
        catalog.update(providers)
        return catalog

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

    def test_missing_provider_rejected_instead_of_partial_generation(self):
        catalog = {"openai": {"models": {"a": {"id": "audit-model", "cost": {"input": 1, "output": 2}}}}}
        response = io.BytesIO(json.dumps(catalog).encode())
        with mock.patch.object(updatePrice.urllib.request, "urlopen", return_value=response):
            with self.assertRaises(ValueError):
                updatePrice.main()
        self.assert_presets_unchanged()

    def test_schema_drift_with_all_prices_missing_rejected(self):
        # 上游把 cost 改名(如 pricing)后,每个模型都得到空 cost——必须报错而非生成全缺失表。
        catalog = {"openai": {"models": {
            "a": {"id": "audit-model", "pricing": {"input": 2, "output": 3}},
            "b": {"id": "audit-model-b", "pricing": {"input": 1, "output": 1}},
        }}}
        response = io.BytesIO(json.dumps(catalog).encode())
        with mock.patch.object(updatePrice.urllib.request, "urlopen", return_value=response):
            with self.assertRaises(ValueError):
                updatePrice.main()
        self.assert_presets_unchanged()

    def test_schema_drift_with_mostly_missing_prices_rejected(self):
        catalog = {"openai": {"models": {
            "priced": {"id": "audit-priced", "cost": {"input": 2, "output": 3}},
            "missing-a": {"id": "audit-missing-a"},
            "missing-b": {"id": "audit-missing-b"},
        }}}
        response = io.BytesIO(json.dumps(catalog).encode())
        with mock.patch.object(updatePrice.urllib.request, "urlopen", return_value=response):
            with self.assertRaises(ValueError):
                updatePrice.main()
        self.assert_presets_unchanged()

    def test_model_id_with_injection_characters_rejected(self):
        catalog = {"openai": {"models": {
            "evil": {"id": 'audit-x": zombie()} //', "cost": {"input": 1, "output": 2}},
        }}}
        response = io.BytesIO(json.dumps(catalog).encode())
        with mock.patch.object(updatePrice.urllib.request, "urlopen", return_value=response):
            with self.assertRaises(ValueError):
                updatePrice.main()
        self.assert_presets_unchanged()

    def test_colon_model_ids_dedup_independently(self):
        catalog = self.full_catalog(openai={"models": {
            "tagged-a": {"id": "audit-model:1", "cost": {"input": 1, "output": 1}},
            "tagged-b": {"id": "audit-model:2", "cost": {"input": 2, "output": 2}},
        }})
        response = io.BytesIO(json.dumps(catalog).encode())
        with mock.patch.object(updatePrice.urllib.request, "urlopen", return_value=response):
            updatePrice.main()
        content = self.output.read_text(encoding="utf-8")
        self.assertIn('"audit-model:1": {Input: 1, Output: 1,', content)
        self.assertIn('"audit-model:2": {Input: 2, Output: 2,', content)

    def test_allowed_missing_provider_generates_with_warning_note(self):
        catalog = self.full_catalog(
            openai={"models": {"a": {"id": "audit-model", "cost": {"input": 1, "output": 2}}}},
        )
        del catalog["anthropic"]
        response = io.BytesIO(json.dumps(catalog).encode())
        with mock.patch.object(updatePrice.urllib.request, "urlopen", return_value=response):
            with mock.patch.dict(os.environ, {updatePrice.ALLOW_MISSING_PROVIDERS_ENV: "anthropic"}):
                updatePrice.main()
        content = self.output.read_text(encoding="utf-8")
        self.assertIn('"audit-model": {Input: 1, Output: 2,', content)
        self.assertIn("providers skipped because they were missing from the upstream catalog", content)
        self.assertIn("- anthropic", content)

    def test_allowed_missing_provider_still_fails_for_others(self):
        catalog = self.full_catalog(
            openai={"models": {"a": {"id": "audit-model", "cost": {"input": 1, "output": 2}}}},
        )
        del catalog["zhipuai"]
        response = io.BytesIO(json.dumps(catalog).encode())
        with mock.patch.object(updatePrice.urllib.request, "urlopen", return_value=response):
            with mock.patch.dict(os.environ, {updatePrice.ALLOW_MISSING_PROVIDERS_ENV: "anthropic"}):
                with self.assertRaises(ValueError):
                    updatePrice.main()
        self.assert_presets_unchanged()

    def test_valid_catalog_preserves_prices_and_deduplicates_models(self):
        catalog = self.full_catalog(
            openai={"models": {
                "paid": {"id": "audit-model", "cost": {"input": 2, "output": 3}},
                "free": {"id": "audit-free", "cost": {"input": 0, "output": 0}},
            }},
            google={"models": {
                "duplicate": {"id": "audit-model", "cost": {"input": 99}},
            }},
        )
        response = io.BytesIO(json.dumps(catalog).encode())
        with mock.patch.object(updatePrice.urllib.request, "urlopen", return_value=response):
            updatePrice.main()
        content = self.output.read_text(encoding="utf-8")
        self.assertEqual(content.count('"audit-model":'), 1)
        self.assertIn('"audit-model": {Input: 2, Output: 3,', content)
        self.assertIn('"audit-free": {Input: 0, Output: 0,', content)


if __name__ == "__main__":
    unittest.main()
