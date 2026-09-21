# Fixer MCP Installer & Runtime Environment

This directory provides the managed installation, command shim generation, health check (`doctor`), and self-update engine for Fixer MCP.

## Minimum Python Policy

- **Supported Python floor**: **Python 3.9** (`sys.version_info >= (3, 9)`).
- **Target platforms**: macOS 12.0+ (ARM64 and AMD64) and modern Linux distributions (x86_64/glibc 2.31+).
- **Rationale**: Many operator environments, fleet nodes, and developer laptops run system Python 3.9 (e.g. macOS default `python3`) or 3.11. Code shipped in release payloads must execute on Python 3.9+ without syntax errors or missing feature exceptions.

### Syntax Rules for Shipped Python Modules
All Python code shipped in release packages (`client_wires/`, `installer/`, `packaging/`) must parse cleanly under Python 3.9. Specifically:
1. **No PEP 695 type aliases**: `type X = ...` is forbidden (SyntaxError before 3.12).
2. **No PEP 701 f-string backslashes**: Backslashes inside f-string expressions (e.g. `f"{re.sub(r'[/\\:]', '-', s)}"`) are forbidden (SyntaxError before 3.12). Expressions containing backslashes must be computed in a local variable before string formatting.
3. **No PEP 654 exception groups syntax**: `except*` is forbidden (SyntaxError before 3.11).
4. **No PEP 634 match/case statements**: `match ...: case ...:` is forbidden (SyntaxError before 3.10).

### Enforcement Mechanisms
1. **Release Packaging Gate**: During `ReleaseAssembler.assemble()`, `packaging.syntax.verify_python_syntax_floor` validates every `.py` file using `ast.parse(source, feature_version=(3, 9))` and checks AST `FormattedValue` nodes for backslashes. If any module fails, release assembly terminates immediately with a non-zero exit code.
2. **Runtime Command Shim**: The installed command shim (`~/.local/bin/fixer`) and payload launcher inspect `sys.version_info` before executing any imports. If running on Python < 3.9, the shim exits immediately with code 1 and writes a single actionable line to `stderr` without a traceback:
   ```text
   Fixer MCP requires Python 3.9 or newer (running on Python X.Y.Z).
   ```
3. **Release Descriptor**: Format=1 release descriptors declare `"min_python": "3.9"` in their schema.

## Payload Bootstrap
Release payload archives contain `scripts/install/install.py`. A bare machine with only Python 3.9+, standard tools (`tar`), and the release descriptor + payload archive can bootstrap a complete managed installation:
```bash
tar -xzf fixer-mcp-<version>-<platform>.tar.gz
python3 payload/scripts/install/install.py --descriptor release.json
```
No git checkout or repository clone is required.
