-- Existing Worker-side ntfy retries can have long backoff. Make them available
-- immediately to a newly installed relay after the architecture cutover.
UPDATE notifications
SET next_attempt_at = 0,
    lease_until = 0,
    claim_token = NULL
WHERE sent_at IS NULL;
