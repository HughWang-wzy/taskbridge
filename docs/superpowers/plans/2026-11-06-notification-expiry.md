# Five-minute notification delivery window

Goal: stop sending notifications five minutes after their original creation, including existing backlog.
Spec: user explicitly requested five minutes maximum and no sends after expiry.
Architecture: D1 claim/lease/deadline filtering without deleting audit rows; original created_at travels with locally queued notifications. Relay checks deadline before publish. Existing state/question persistence remains independent of delivery expiration.

- [x] Pin Worker claim expiry, boundary, targeted claim, failure scheduling and audit counters with failing tests.
- [x] Implement a five-minute age limit, bounded retries, expiry metadata and original creation timestamps.
- [x] Pin client expired batch items and offline replay timestamps with tests; preserve expired local notification files in an archive.
- [x] Verify tests, deploy Worker, replace local binary, resume relay and confirm old backlog is ineligible.

Verification: Go tests/vet, 41 Worker tests, TypeScript checking passed. Worker version d91d7c42-aac6-4718-854f-5459e2848c2b deployed; local binary replaced and relay resumed. Expired notification records are not deleted or ACKed as sent.
