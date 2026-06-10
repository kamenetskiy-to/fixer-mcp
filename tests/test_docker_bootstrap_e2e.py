from __future__ import annotations

import pathlib
import subprocess
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]


class DockerBootstrapE2EContractTests(unittest.TestCase):
    def test_makefile_exposes_separate_expensive_target(self) -> None:
        makefile = (ROOT / "Makefile").read_text(encoding="utf-8")
        self.assertIn("docker-bootstrap-e2e:", makefile)
        self.assertIn("docker/fixer-bootstrap-e2e.sh", makefile)
        self.assertIn("docker-smoke:", makefile)

    def test_auth_is_runtime_readonly_mount_not_image_copy(self) -> None:
        wrapper = (ROOT / "docker" / "fixer-bootstrap-e2e.sh").read_text(encoding="utf-8")
        dockerfile = (ROOT / "docker" / "fixer-bootstrap-e2e.Dockerfile").read_text(encoding="utf-8")
        self.assertIn("target=/root/.codex/auth.json,readonly", wrapper)
        self.assertNotIn("auth.json", dockerfile)
        self.assertIn("npm install -g", dockerfile)

    def test_codex_compat_is_vendored_not_runtime_mount(self) -> None:
        wrapper = (ROOT / "docker" / "fixer-bootstrap-e2e.sh").read_text(encoding="utf-8")
        container = (ROOT / "docker" / "fixer-bootstrap-e2e-container.sh").read_text(encoding="utf-8")
        dockerfile = (ROOT / "docker" / "fixer-bootstrap-e2e.Dockerfile").read_text(encoding="utf-8")

        self.assertNotIn("DOCKER_BOOTSTRAP_E2E_CODEX_PRO_APP_PATH", wrapper)
        self.assertNotIn("mcp_servers/codex_pro_app", wrapper)
        self.assertNotIn("MCP_SERVERS_ROOT", wrapper)
        self.assertIn("import client_wires.codex_compat", container)
        self.assertIn("vendored codex_compat detected", container)
        self.assertNotIn("../mcp_servers", dockerfile)

    def test_runner_names_stage_pass_criteria_and_forced_fixer_wiring(self) -> None:
        runner = (ROOT / "docker" / "fixer_bootstrap_e2e.py").read_text(encoding="utf-8")
        self.assertIn("Stage 1 passes", runner)
        self.assertIn("Stage 2 passes", runner)
        self.assertIn("launch_and_wait_netrunner", runner)
        self.assertIn("list_mcp_servers", runner)
        self.assertIn("mcp_servers.fixer_mcp.command", runner)
        self.assertIn("FIXER_MCP_LOCKED_ROLE", runner)
        self.assertIn("import sys", runner)
        self.assertIn("copy_headless_netrunner_logs", runner)
        self.assertIn("headless_netrunner_logs", runner)
        self.assertIn("shutil.copytree", runner)

    def test_shell_and_python_syntax(self) -> None:
        subprocess.run(
            ["bash", "-n", str(ROOT / "docker" / "fixer-bootstrap-e2e.sh")],
            check=True,
        )
        subprocess.run(
            ["bash", "-n", str(ROOT / "docker" / "fixer-bootstrap-e2e-container.sh")],
            check=True,
        )
        subprocess.run(
            ["python3", "-m", "py_compile", str(ROOT / "docker" / "fixer_bootstrap_e2e.py")],
            check=True,
        )


if __name__ == "__main__":
    unittest.main()
