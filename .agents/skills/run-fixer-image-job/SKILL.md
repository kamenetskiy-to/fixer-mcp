---
name: run-fixer-image-job
description: "Use this skill when a Fixer needs to generate or edit raster images through the live Fixer MCP sidecar image-job tools, keeping the main thread alive and returning a durable output path."
---

# Run Fixer Image Job

Use this skill for Fixer-side raster image generation or editing through the durable image-job sidecar.

## Preconditions

1. Authenticate as `fixer`.
2. Confirm the current tool surface exposes:
   - `launch_image_generation_job`
   - `wait_for_image_generation_job`
   - `copy_image_generation_job_output`
3. Stop if you are not in a Fixer context with those tools available.

## Generation Flow

1. Call `launch_image_generation_job` with a concrete `prompt` and optional `model`.
2. Save the returned `job_id`.
3. Call `wait_for_image_generation_job`.
4. If the result must become a project asset, call `copy_image_generation_job_output` with a project-relative destination path.

## Editing Flow

1. Confirm each source image exists.
2. Call `launch_image_generation_job` with:
   - `prompt`
   - `input_image_paths`
   - optional `model`
3. Wait with `wait_for_image_generation_job`.
4. Copy into the workspace only when needed.

## Prompting Rules

- Describe the desired output directly.
- For edits, state what must remain unchanged and what must change.
- Launch one job per requested output when variants or separate assets are required.
- Add `no text` when text should not appear in the image.

## Failure Handling

If a job fails, inspect the returned `stderr_log_path`, `json_output_path`, and `output_last_message_path`. Do not guess an output path when the wait tool has already returned structured result data.
