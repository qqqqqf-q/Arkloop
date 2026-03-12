import os
import subprocess
import sys
import unittest

sys.path.insert(0, os.path.dirname(__file__))
import module_registry


ROOT = os.path.dirname(__file__)
MODULES = os.path.join(ROOT, "modules.yaml")


class ModuleRegistryTest(unittest.TestCase):
    def test_parse_modules_extracts_console_and_browser_metadata(self):
        modules = module_registry.parse_modules(MODULES)
        self.assertIn("console-lite", modules)
        self.assertIn("browser", modules)
        self.assertEqual(modules["console-lite"]["compose_service"], "console-lite")
        self.assertEqual(modules["browser"]["compose_service"], "sandbox-docker")

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
        self.assertEqual(plan["console"], "lite")
        self.assertEqual(plan["sandbox"], "none")
        self.assertIn("console-lite", plan["selected_modules"])
        self.assertIn("gateway", plan["selected_modules"])

    def test_resolve_full_defaults_prefers_firecracker_on_linux_kvm(self):
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
            "--has-kvm",
        ])
        plan = module_registry.resolve_plan(modules, args)
        self.assertEqual(plan["sandbox"], "firecracker")
        self.assertIn("sandbox-firecracker", plan["selected_modules"])
        self.assertEqual(plan["console"], "full")

    def test_resolve_saas_standard_defaults(self):
        """SaaS mode standard profile should auto-select pgbouncer, seaweedfs, and full console."""
        modules = module_registry.parse_modules(MODULES)
        parser = module_registry.build_parser()
        args = parser.parse_args([
            "resolve",
            "--modules", MODULES,
            "--mode", "saas",
            "--profile", "standard",
            "--host-os", "linux",
        ])
        plan = module_registry.resolve_plan(modules, args)
        selected = plan["selected_modules"]
        self.assertIn("pgbouncer", selected)
        self.assertIn("seaweedfs", selected)
        self.assertIn("console", selected)
        self.assertNotIn("console-lite", selected)
        profiles = plan["compose_profiles"]
        self.assertIn("pgbouncer", profiles)
        self.assertIn("s3", profiles)
        self.assertIn("console-full", profiles)

    def test_resolve_saas_full_defaults(self):
        """SaaS mode full profile should include pgbouncer, seaweedfs, and extra full-profile modules."""
        modules = module_registry.parse_modules(MODULES)
        parser = module_registry.build_parser()
        args = parser.parse_args([
            "resolve",
            "--modules", MODULES,
            "--mode", "saas",
            "--profile", "full",
            "--host-os", "linux",
            "--has-kvm",
        ])
        plan = module_registry.resolve_plan(modules, args)
        selected = plan["selected_modules"]
        self.assertIn("pgbouncer", selected)
        self.assertIn("seaweedfs", selected)
        self.assertIn("console", selected)
        self.assertIn("sandbox-firecracker", selected)

    def test_saas_does_not_raise(self):
        """SaaS mode should no longer raise ValueError (PR8 unblocked it)."""
        modules = module_registry.parse_modules(MODULES)
        parser = module_registry.build_parser()
        args = parser.parse_args([
            "resolve",
            "--modules", MODULES,
            "--mode", "saas",
            "--profile", "standard",
            "--host-os", "macos",
        ])
        plan = module_registry.resolve_plan(modules, args)
        self.assertIn("selected_modules", plan)

    def test_browser_requires_docker(self):
        cmd = [
            "python3",
            os.path.join(ROOT, "module_registry.py"),
            "resolve",
            "--modules",
            MODULES,
            "--browser",
            "on",
            "--sandbox",
            "none",
            "--host-os",
            "macos",
        ]
        proc = subprocess.run(cmd, capture_output=True, text=True)
        self.assertNotEqual(proc.returncode, 0)
        self.assertIn("browser=on", proc.stderr)


if __name__ == "__main__":
    unittest.main()
