# Terminal Workflow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Make TaskBridge usable through a short command or an interactive launcher, with editable alert presets and reusable training tasks.

**Architecture:** Keep task execution and notification delivery in the existing client. A small terminal UI prepares an action and restores the terminal before invoking it. Store favorites beside local client configuration, preserving argv for CLI-created tasks and explicit shell text for pasted commands.

**Tech Stack:** Go 1.22+, Bubble Tea v0.27.1 and golang.org/x/term v0.25.0 (compatible with Go 1.22), existing Worker/D1/ntfy unchanged.

**Spec:** User-approved in-chat menu/settings/automatic-name design; user explicitly requested comprehensive implementation including the proposed workflow.

## Global Constraints

- Support existing Linux, macOS and Windows binaries and SSH terminals.
- Explicit CLI commands remain scriptable. No-argument `tb` opens a menu only with terminal stdin/stdout; redirected use prints help.
- Leave raw/alternate-screen mode before child execution. Preserve child stdin, stdout, stderr and exit status.
- Preview exact command, working directory and notification levels; require confirmation before running or deleting favorites.
- Save preferences locally, preserve credentials/templates, and retain existing queue/idempotency behavior.
- Reminder presets select ntfy priorities; they do not guarantee phone sounds.

## Review Focus

- Esc/Ctrl-C/EOF must cancel without changing settings, running commands, or leaving terminal raw.
- Paths with spaces, quotes, Unicode, shell operators and continued lines must retain their intended meaning.
- Saved cwd and argv must survive reload and a failing task; corrupted favorite files must remain intact.
- Presets must update both client configurations, with explicit flags overriding a preset in either argument order.
- Existing piped/automated CLI use must never wait for interactive input.

### Task 1: Alert presets and shared configuration helpers

Files: `cmd/tb/alerts.go`, `cmd/tb/alerts_test.go`.

- [x] Write and observe failing tests for presets, override order and invalid-input preservation.
- [x] Add `alertPreset`, `readAlertConfigs`, `saveAlertLevels`; keep existing CLI semantics.
- [x] Verify preset maps: low-noise 2/4/4/2/5/2; standard 3/4/4/4/5/3; important 4/5/4/4/5/4.

### Task 2: Command names and favorites

Files: `cmd/tb/task_commands.go`, `cmd/tb/task_commands_test.go`; root integrates `main.go`.

- [x] Test interpreter/script/module names, saved argv/cwd, invalid/corrupt records, and run exit/output behavior.
- [x] Implement `inferTaskName`, `SavedTask`, local atomic storage, shell execution helper, and `tb tasks list|save|run|remove`.
- [x] Use inferred names when `tb run` has no explicit `-n`.

### Task 3: Terminal UI and action dispatch

Files: `cmd/tb/terminal_ui.go`, `cmd/tb/terminal_models.go`, `cmd/tb/terminal_ui_test.go`, `cmd/tb/main.go`, `go.mod`, `go.sum`.

- [x] Test pure navigation/settings state, save versus discard, command preview cancellation, escaped shell text and non-TTY behavior.
- [x] Implement arrow-key menu, editable preset/settings screen, multiline command input, favorites edit/delete/run, explicit test-notification confirmation and doctor view.
- [x] Route `tb`, `tb menu`, `tb settings` and `tb tasks`; keep UI and child execution sequential.
- [x] Verify actual Linux PTY navigation, settings cancellation/save, task stdin/output handoff and terminal restoration.

### Task 4: Documentation and delivery

Files: READMEs, deployment guides, release version/tag wrappers, CI.

- [x] Explain existing short commands, new interactive entrypoints, local favorites and sound limitations.
- [x] Run all Go/Worker/type/setup/install checks and cross-platform builds; review the whole change.
- [ ] Publish v0.5.0, verify latest assets, atomically update the local client and check menu availability.

## Execution Record

- Started from clean main at `122ddc6`; independent implementation tasks use disjoint files in the shared checkout.
- User-approved scope includes reusable task favorites; the menu hands execution to the existing foreground runner.

- Alert preset, task persistence, and UI model tests were observed failing before implementation and pass after implementation.
- Local validation passed: Go tests/vet, 38 Worker tests, TypeScript checking, setup wizard test, installer/update/Node repair smoke tests, real Unix PTY smoke, and four release targets with SHA-256 checks.
- PTY checks cover Esc cancellation, draft settings save/discard, raw multiline command editing, favorite save/edit/delete, child stdin/output, and restored terminal mode. Windows-specific child argv/exit tests run in Windows CI; Linux/macOS CI also run the PTY smoke.
- Read-only review found no blocking code issues; documentation now matches Enter-to-save settings.
