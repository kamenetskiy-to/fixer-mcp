"""Fixer MCP macOS release packaging and verification package."""

from packaging.syntax import PythonSyntaxFloorError, verify_python_syntax_floor

__version__ = "0.1.0"

__all__ = [
    "PythonSyntaxFloorError",
    "verify_python_syntax_floor",
]
