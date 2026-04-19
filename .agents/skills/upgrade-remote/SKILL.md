---
name: upgrade-remote
description: "Upgrade a trusted remote Windows glaw/claw-life-saver instance by email using a two-stage workflow: first prepare and compile on the remote side, then finalize with a detached cutover and verification. Use when direct git pull or artifact download is unsuitable and the remote machine already supports mail execution through `glaw serve --exec-subject-keyword ...`."
---

# Upgrade Remote

## Overview

Use this skill to ship a local glaw change set to a trusted remote Windows machine through the existing mail execution chain. Default to Python scripts, build the next exe on the remote side, then do the process stop/replace/restart step in a detached second stage.

## Workflow

1. Build the source bundle locally with [build-mail-source-bundle.py](/C:/Users/MECHREV/glaw/scripts/build-mail-source-bundle.py).
2. Send a prepare mail with the target-specific one-off script in `tmp/scripts/`, such as [prepare-claw-life-saver-build.py](/C:/Users/MECHREV/glaw/tmp/scripts/prepare-claw-life-saver-build.py) or [prepare-shuyao-build.py](/C:/Users/MECHREV/glaw/tmp/scripts/prepare-shuyao-build.py), plus the source zip.
3. Verify `build-info.txt` reports `exit_code=0` and points at the prepared remote exe path.
4. Send a finalize mail with the target-specific detached cutover script, such as [finalize-claw-life-saver-upgrade.py](/C:/Users/MECHREV/glaw/tmp/scripts/finalize-claw-life-saver-upgrade.py).
5. Verify the remote process and attachment-arg protocol with [probe-mail-exec-multi-attachment.py](/C:/Users/MECHREV/glaw/tmp/scripts/probe-mail-exec-multi-attachment.py).

Always split remote upgrades into exactly two mail-exec stages:

1. `prepare`
   Build or stage everything needed for the switch.
2. `finalize`
   Do the actual process stop, binary replace, and restart in a detached script.

## Source Bundle Rule

Use the default Git short hash in the source bundle filename. Prefer target-specific names such as:

- `dist/mail-upgrade/claw-life-saver-source-<git-short-hash>.zip`
- `dist/mail-upgrade/shuyao-source-<git-short-hash>.zip`

Do not hard-code 4 characters. Use `git rev-parse --short HEAD`, which is currently 7 characters in this repo.

The source bundle should include only tracked `*.go`, `go.mod`, and `go.sum` files needed to rebuild `./cmd/glaw`.

## Mail Protocol

- Default to Python execution scripts.
- For `shuyao`-style cutovers, prefer [graceful-stop.py](/C:/Users/MECHREV/glaw/scripts/graceful-stop.py) to stop `glaw.exe` only after `pi` work has quiesced.
- If a script is only for one or two runs in the current upgrade, keep it in `tmp/scripts/`; move it into `scripts/` only after it proves to be a long-lived reusable tool.
- Use one serial shell command for send + reply polling:
  `python ...send_email.py ... ; go run ./cmd/glaw mail latest --sender <addr> --max-sleep-seconds 120 --poll-interval-seconds 2`
- For prepare mails, include the build result paths in the body so the reply zip carries them back.
- For finalize mails, include `upgrade-claw-life-saver.log`, `claw-life-saver-stdout.log`, `claw-life-saver-stderr.log`, `finalize-detached.stdout.txt`, and `finalize-detached.stderr.txt`.

## Remote Assumptions

- Target runtime is Windows and already has a working mail-exec chain.
- Prepare stage can compile with `go build` on the remote host.
- Finalize stage must be detached, sleep 5 seconds before acting, and write its own stdout/stderr to files for later inspection.
- Current remote mail-exec protocol supports multiple attachments and passes only resource attachments as argv; with one script plus one resource zip, `argv[1]` is the resource attachment path.
- Current remote resource attachment args are absolute paths.

## Verification

- Trust `stderr` first.
- If finalize returns too quickly, send a second probe mail to read the detached output files and current process command line.
- After protocol changes, re-run the multi-attachment probe and inspect `argv` to confirm the remote side really matches the new contract.
