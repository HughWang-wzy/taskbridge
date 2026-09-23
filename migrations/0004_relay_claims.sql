ALTER TABLE notifications ADD COLUMN claim_token TEXT;
UPDATE clients SET scopes = json_insert(scopes, '$[#]', 'notifications:relay')
WHERE json_valid(scopes)
  AND NOT EXISTS (SELECT 1 FROM json_each(clients.scopes) WHERE value = 'notifications:relay');
