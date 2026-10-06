/**
 * System1 restricted evidence reader tool (Pi extension).
 *
 * This is the ONE tool the one-shot System1 transcript reader may hold. It is
 * loaded explicitly (`-e <this file>`) while `--no-extensions` disables every
 * discovered, configured, and built-in extension, and `--tools read_evidence`
 * drops every other tool (no bash, write, edit, codemode, tool_search, MCP).
 *
 * Threat model: the reader is a one-shot review component, never a nested
 * implementation agent. A malicious project (or a prompt-injected transcript)
 * must never be able to read the Fixer DB, credentials, or arbitrary project
 * files, and must never be able to write, execute, or authenticate anything.
 * The ONLY capability this file grants is bounded sequential reads of exactly
 * the evidenced transcript/artifact paths the runner registered before launch
 * in READER_EVIDENCE_ALLOWLIST (JSON array of absolute paths). Every read is
 * appended to READER_EVIDENCE_READ_LOG as a durable, secret-safe read ledger.
 *
 * This file must stay dependency-free: no package imports (TypeBox schemas are
 * plain JSON Schema objects, so the parameter schema is written literally),
 * only node: builtins and plain ESM syntax.
 */

import * as fs from "node:fs";
import * as path from "node:path";

const MAX_LINES_PER_CALL = 200;
const MAX_BYTES_PER_CALL = 32 * 1024;
const DEFAULT_LINES_PER_CALL = 120;

function allowlistPaths() {
	const raw = process.env.READER_EVIDENCE_ALLOWLIST || "[]";
	let parsed;
	try {
		parsed = JSON.parse(raw);
	} catch (err) {
		return [];
	}
	if (!Array.isArray(parsed)) return [];
	return parsed.filter((entry) => typeof entry === "string" && entry.trim() !== "");
}

function canonicalPath(candidate) {
	try {
		return fs.realpathSync(candidate);
	} catch (err) {
		// Fall back to lexical normalization for the not-yet-resolvable case;
		// it still fails closed because the comparison is exact.
		return path.resolve(candidate);
	}
}

function resolveAllowedPath(candidate, allowed) {
	const target = canonicalPath(candidate);
	for (const entry of allowed) {
		if (canonicalPath(entry) === target) {
			return entry;
		}
	}
	return null;
}

function appendReadLog(record) {
	const logPath = process.env.READER_EVIDENCE_READ_LOG;
	if (!logPath) return;
	try {
		fs.appendFileSync(logPath, JSON.stringify(record) + "\n");
	} catch (err) {
		// The read ledger is diagnostic; a logging failure never widens access.
	}
}

function denyResult(reason, params) {
	appendReadLog({
		tool: "read_evidence",
		path: typeof params.path === "string" ? params.path : "",
		status: "denied",
		reason: reason,
	});
	return {
		isError: true,
		content: [
			{
				type: "text",
				text:
					"DENIED: read_evidence only reads the registered evidence paths " +
					"(session transcripts and their artifacts). " +
					"Reason: " + reason,
			},
		],
		details: { status: "denied", reason: reason },
	};
}

function executeRead(params) {
	const allowed = allowlistPaths();
	if (allowed.length === 0) {
		return denyResult("no evidence paths were registered for this run", params);
	}
	const rawPath = typeof params.path === "string" ? params.path : "";
	if (rawPath.trim() === "") {
		return denyResult("missing path", params);
	}
	const matched = resolveAllowedPath(rawPath, allowed);
	if (matched === null) {
		return denyResult("path is outside the registered evidence set", params);
	}

	let startLine = Number.isInteger(params.start_line) ? params.start_line : 1;
	if (startLine < 1) startLine = 1;
	let limitLines = Number.isInteger(params.limit_lines) ? params.limit_lines : DEFAULT_LINES_PER_CALL;
	if (limitLines < 1) limitLines = DEFAULT_LINES_PER_CALL;
	if (limitLines > MAX_LINES_PER_CALL) limitLines = MAX_LINES_PER_CALL;

	let content;
	try {
		content = fs.readFileSync(matched, "utf8");
	} catch (err) {
		appendReadLog({ tool: "read_evidence", path: matched, status: "error", reason: "unreadable" });
		return {
			isError: true,
			content: [{ type: "text", text: "ERROR: evidenced file is unreadable: " + path.basename(matched) }],
			details: { status: "error" },
		};
	}

	const endsWithNewline = content === "" || content.endsWith("\n");
	const allLines = content.split("\n");
	if (endsWithNewline && allLines.length > 0 && allLines[allLines.length - 1] === "") {
		allLines.pop();
	}
	const totalLines = allLines.length;
	const slice = allLines.slice(startLine - 1, startLine - 1 + limitLines);
	const endLine = startLine - 1 + slice.length;
	const eof = endLine >= totalLines;

	const numbered = [];
	let bytes = 0;
	for (let index = 0; index < slice.length; index++) {
		const lineText = "L" + (startLine + index) + ": " + slice[index];
		if (bytes + lineText.length > MAX_BYTES_PER_CALL && numbered.length > 0) {
			break;
		}
		numbered.push(lineText);
		bytes += lineText.length + 1;
	}
	const returnedEnd = startLine - 1 + numbered.length;

	const header =
		"evidence file: " + path.basename(matched) +
		"\ntotal_lines: " + totalLines +
		"\nreturned_lines: " + startLine + ".." + returnedEnd +
		"\neof: " + (returnedEnd >= totalLines ? "true" : "false");

	appendReadLog({
		tool: "read_evidence",
		path: matched,
		status: "ok",
		start_line: startLine,
		end_line: returnedEnd,
		total_lines: totalLines,
		bytes: bytes,
		eof: returnedEnd >= totalLines,
	});

	return {
		content: [{ type: "text", text: header + "\n\n" + numbered.join("\n") }],
		details: {
			status: "ok",
			path: matched,
			start_line: startLine,
			end_line: returnedEnd,
			total_lines: totalLines,
			eof: returnedEnd >= totalLines,
		},
	};
}

const readEvidenceTool = {
	name: "read_evidence",
	label: "Read evidence",
	description:
		"Read one registered evidence file (session transcript or artifact) sequentially by line. " +
		"Only the exact registered evidence paths are readable; every other path is denied. " +
		"Use start_line/limit_lines (max " + MAX_LINES_PER_CALL + " lines per call) to read the file end to end.",
	parameters: {
		type: "object",
		properties: {
			path: { type: "string", description: "Absolute path of one registered evidence file." },
			start_line: { type: "integer", description: "1-based first line to return. Defaults to 1." },
			limit_lines: { type: "integer", description: "How many lines to return (max " + MAX_LINES_PER_CALL + ")." },
		},
		required: ["path"],
		additionalProperties: false,
	},
	async execute(_toolCallId, params, _signal, _onUpdate, _ctx) {
		return executeRead(params || {});
	},
};

export default function (pi) {
	pi.registerTool(readEvidenceTool);
}
