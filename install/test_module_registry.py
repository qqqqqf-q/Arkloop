import os
import subprocess
import sys
import unittest

sys.path.insert(0, os.path.dirname(__file__))
import module_registry


ROOT = os.path.dirname(__file__)
MODULES = os.path.join(ROOT, "modules.yaml")


class ModuleRegistryTest(unittest.TestCase):
    def test_parse_modules_extracts_web_tool_metadata(self):
        modules = module_registry.parse_modules(MODULES)
        self.assertEqual(modules["searxng"]["compose_service"], "searxng")
        self.assertEqual(modules["firecrawl"]["compose_profile"], "firecrawl")
        # sandbox/browser 已移除,不应再出现在模块清单
        self.assertNotIn("sandbox-docker", modules)
        self.assertNotIn("browser", modules)

    def test_resolve_standard_defaults(self):
        modules = module_registry.parse_modules(MODULES)
        parser = module_registry.build_parser()
        args = parser.parse_args([
            "resolve",
            "--modules",
            MODULES,
            "--host-os",
            "macos",
        ])
        plan = module_registry.resolve_plan(modules, args)
        self.assertEqual(plan["profile"], "standard")
        self.assertEqual(plan["web_tools"], "builtin")
        self.assertEqual(plan["selected_modules"], [])

    def test_resolve_full_defaults_self_hosted_web_tools(self):
        modules = module_registry.parse_modules(MODULES)
        parser = module_registry.build_parser()
        args = parser.parse_args([
            "resolve",
            "--modules",
            MODULES,
            "--profile",
            "full",
            "--host-os",
            "linux",
        ])
        plan = module_registry.resolve_plan(modules, args)
        self.assertEqual(plan["web_tools"], "self-hosted")
        self.assertIn("searxng", plan["selected_modules"])
        self.assertIn("firecrawl", plan["selected_modules"])
        self.assertIn("searxng", plan["compose_services"])
        self.assertIn("firecrawl", plan["compose_profiles"])

    def test_resolve_explicit_web_tools_self_hosted(self):
        cmd = [
            "python3",
            os.path.join(ROOT, "module_registry.py"),
            "resolve",
            "--modules",
            MODULES,
            "--web-tools",
            "self-hosted",
            "--host-os",
            "macos",
        ]
        proc = subprocess.run(cmd, capture_output=True, text=True)
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertIn("RESOLVED_WEB_TOOLS=self-hosted", proc.stdout)
        self.assertIn("searxng", proc.stdout)

    def test_resolve_rejects_unknown_web_tools(self):
        cmd = [
            "python3",
            os.path.join(ROOT, "module_registry.py"),
            "resolve",
            "--modules",
            MODULES,
            "--web-tools",
            "bogus",
            "--host-os",
            "macos",
        ]
        proc = subprocess.run(cmd, capture_output=True, text=True)
        self.assertNotEqual(proc.returncode, 0)
        self.assertIn("web_tools", proc.stderr)


if __name__ == "__main__":
    unittest.main()
