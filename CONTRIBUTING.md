# Contributing

Issues and pull requests are welcome. Please describe the behavior you expect, the behavior you observed, and how to reproduce it without including tokens, ntfy topics, or other private configuration.

Before opening a pull request:

```bash
go test ./...
npm ci
cp wrangler.example.jsonc wrangler.jsonc
npm test
npm run typecheck
npm run package
python3 test/installer_smoke.py
```

Keep local credentials in `wrangler.jsonc`, `.dev.vars`, and OS user config files. The public repository contains only an example Worker configuration. Changes to the notification path should preserve D1 idempotency, lease-based relay ACKs, and the rule that the Worker never publishes directly to ntfy.
