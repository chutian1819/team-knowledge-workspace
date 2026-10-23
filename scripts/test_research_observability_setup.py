"""Isolated configuration safety checks; these never use live service credentials."""
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import stat
import tempfile
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location("observability_setup", Path(__file__).with_name("research-observability-setup.py"))
setup = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(setup)


class ObservabilitySetupTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        root = Path(self.directory.name)
        self.root_patch = mock.patch.object(setup, "ROOT", root)
        self.env_patch = mock.patch.object(setup, "ENV", root / ".research-observability.env")
        self.root_patch.start()
        self.env_patch.start()
        self.addCleanup(self.root_patch.stop)
        self.addCleanup(self.env_patch.stop)
        (root / ".env").write_text("PLATFORM_SECRET=unchanged-test-value\n")
        self.values = {"LF_DATABASE_PASSWORD": "a" * 48,
                       "LF_DATABASE_URL": "postgresql://test-only/database",
                       "LF_REDIS_URL": "redis://:old-test-password@context-workspace-redis:6379/1",
                       "LF_SALT": "same-salt", "LF_ENCRYPTION_KEY": "same-encryption-key",
                       "LANGFUSE_SECRET_KEY": "same-project-key", "LF_ADMIN_PASSWORD": "same-admin-password"}

    def write_config(self):
        setup.ENV.write_text("# preserve this comment\n" + "\n".join(k + "=" + v for k, v in self.values.items()) + "\n")

    def run_existing(self, *args):
        output = io.StringIO()
        with mock.patch.object(setup, "database_state", return_value=({"rolname": "research_langfuse"}, "research_langfuse")), \
                mock.patch.object(setup, "sql", side_effect=AssertionError("existing migration must not write the database")), \
                mock.patch("sys.argv", ["setup", *args]), contextlib.redirect_stdout(output):
            setup.main()
        return output.getvalue()

    def test_migration_is_explicit_and_preserves_existing_credentials(self):
        self.write_config()
        before = setup.ENV.read_text()
        with self.assertRaisesRegex(RuntimeError, "explicit --migrate-redis"):
            self.run_existing()
        self.assertEqual(before, setup.ENV.read_text())
        output = self.run_existing("--migrate-redis")
        result = setup.dotenv(setup.ENV)
        self.assertTrue(result["LF_REDIS_PASSWORD"])
        self.assertIn("@research-langfuse-redis:6379/0", result["LF_REDIS_URL"])
        for key, value in self.values.items():
            if key != "LF_REDIS_URL":
                self.assertEqual(value, result[key])
            self.assertNotIn(value, output)
        self.assertEqual(0o600, stat.S_IMODE(setup.ENV.stat().st_mode))
        self.assertIn("# preserve this comment", setup.ENV.read_text())
        self.assertEqual("PLATFORM_SECRET=unchanged-test-value\n", (setup.ROOT / ".env").read_text())

    def test_repeat_migration_reuses_all_keys(self):
        self.write_config()
        self.run_existing("--migrate-redis")
        before = setup.ENV.read_bytes()
        with mock.patch.object(setup.secrets, "token_hex", side_effect=AssertionError("must not rotate keys")):
            self.run_existing("--migrate-redis")
            self.run_existing()
        self.assertEqual(before, setup.ENV.read_bytes())

    def test_existing_database_with_missing_config_never_regenerates(self):
        with mock.patch.object(setup.secrets, "token_hex", side_effect=AssertionError("must not replace lost secrets")):
            with self.assertRaisesRegex(RuntimeError, "restore the original private environment"):
                self.run_existing("--migrate-redis")
        self.assertFalse(setup.ENV.exists())

    def test_role_privilege_and_database_owner_checks_fail_closed(self):
        safe = {"rolcanlogin": True}
        for flag in ("rolsuper", "rolcreatedb", "rolcreaterole", "rolreplication", "rolbypassrls", "member_of_other_roles"):
            with self.subTest(flag=flag), mock.patch.object(setup, "sql", side_effect=[json.dumps({**safe, flag: True}), "research_langfuse"]):
                with self.assertRaisesRegex(RuntimeError, "unexpected privileges"):
                    setup.database_state()
        with mock.patch.object(setup, "sql", side_effect=[json.dumps(safe), "another_owner"]):
            with self.assertRaisesRegex(RuntimeError, "another role"):
                setup.database_state()
        with mock.patch.object(setup, "sql", side_effect=[json.dumps(safe), "research_langfuse"]):
            self.assertEqual((safe, "research_langfuse"), setup.database_state())

    def test_clean_install_uses_independent_redis_and_private_env(self):
        with mock.patch.object(setup, "database_state", return_value=(None, "")), \
                mock.patch.object(setup, "sql", return_value="") as sql, \
                mock.patch("sys.argv", ["setup"]), contextlib.redirect_stdout(io.StringIO()):
            setup.main()
        values = setup.dotenv(setup.ENV)
        self.assertIn("@research-langfuse-redis:6379/0", values["LF_REDIS_URL"])
        self.assertNotEqual(values["LF_DATABASE_PASSWORD"], values["LF_REDIS_PASSWORD"])
        self.assertEqual(2, sql.call_count)
        self.assertEqual(0o600, stat.S_IMODE(setup.ENV.stat().st_mode))


if __name__ == "__main__":
    unittest.main()
