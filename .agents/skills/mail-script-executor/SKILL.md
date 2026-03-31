---
name: mail-script-executor
description: Send a `.py` or `.ps1` script to a trusted remote machine by email, trigger execution via a subject keyword, then inspect the reply attachments and latest mailbox state. Use when Codex needs to drive a cautious remote workflow step by step through email instead of SSH or direct shell access, especially for probe scripts, environment checks, staged upgrade preparation, or final execution on the remote machine.
---

# Mail Script Executor

Use this skill when the remote machine already runs `glaw serve --exec-subject-keyword <keyword>` and accepts attached `.py` / `.ps1` files for execution.

Current project convention:

- For the `glaw` / `claw-life-saver` remote execution flow in this workspace, the execution subject keyword is typically `claw-life-saver`.
- Unless the user explicitly says otherwise or a fresher runtime check proves a different value, prefer subjects such as `claw-life-saver probe ...`, `claw-life-saver rerun ...`, or `claw-life-saver finalize ...`.

## Workflow

1. Prepare one narrow script per email.
2. Prefer `.py` over `.ps1` when the script prints structured text or Chinese output.
3. For every mailed `.py` script, set:

```python
if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8")
```

4. Trigger execution by subject keyword. Keep the body minimal, for example `请执行附件。`
5. Send exactly one executable attachment unless the remote executor is explicitly designed for more.
6. After sending, wait 30 seconds, then inspect the latest mail from that sender.
7. Read returned `stdout.txt` and `stderr.txt` before deciding the next step.
8. Run local helper scripts with `python`, not `uv`.
9. Use `mail-script-executor/scripts/send_and_check_reply.py` when you want one deterministic command for send -> sleep -> latest-reply.
10. If the remote has already upgraded to the zip-reply version, you may place one absolute file path per line in the mail body after the first short instruction line; existing files will be attached into the reply zip.

## Rules

- Default to attachment-driven collaboration. Do not pile commands into the body.
- For execution mails, confirm only the title and whether to send; do not block on正文逐字确认.
- Keep remote steps reversible until the very last cutover step.
- If a happy path probe passes, it is acceptable to include one extra low-risk next step in the same script.
- If the current step is high risk, keep the script narrow and single-purpose.
- Do not assume relative dates; inspect actual reply timestamps and outputs.
- On Windows, when a mailed script needs to launch a detached follow-up PowerShell process, prefer a script file plus `-ExecutionPolicy Bypass -File <script>` rather than inline `-Command`, so detached restarts are resilient to policy differences.
- When using the zip-reply flow, prefer absolute Windows paths in the body such as `C:\Users\cmwh\claw-life-saver\logs\upgrade-claw-life-saver.log`, one path per line, so the remote side can attach the intended files without ambiguity.

## Reply Inspection

- Treat `stderr` as authoritative for failures.
- If `stdout` says a file was written, but that file is not attached, assume the remote executor only returned `stdout/stderr`.
- After the remote upgrades successfully, expect one zip attachment instead of separate `stdout.txt` / `stderr.txt` attachments; inspect the zip contents first, then read `stdout`, `stderr`, and any requested files inside it.
- For detached start flows, verify success by a second probe that reads:
  - running process list
  - startup log
  - redirected `stdout/stderr` log files

## Common Patterns

- Probe repo cleanliness, then include one extra safe step if clean.
- Write config files from trusted source values, then print the written content.
- Build a temp exe first, validate it, and only later perform cutover.
- Split final cutover into:
  - prepare start script
  - inspect start script contents
  - execute final switch
  - probe post-switch state

## Sending

Use the existing `send-email` skill/script to send the mail with:

- a subject containing the execution keyword
- a very short body
- one attached script

Save the body file under `gateway/outbox/` if operating inside the `glaw` repo workflow.

## Script

Use `mail-script-executor/scripts/send_and_check_reply.py` for the common pattern:

```bash
python mail-script-executor/scripts/send_and_check_reply.py \
  --to cjwhshuyao@163.com \
  --subject "claw-life-saver probe git clean" \
  --body-file C:\path\to\gateway\outbox\mail.md \
  --attachment C:\path\to\script.py \
  --reply-sender cjwhshuyao@163.com \
  --repo C:\path\to\glaw
```
