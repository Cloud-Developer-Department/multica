-- Feedback Center: upgrade the existing message-only `feedback` table into the
-- public feedback-pool model. Keeps every legacy row (title/type backfilled)
-- rather than rebuilding a second feedback system.
--
--   user_id   -> creator_id
--   message   -> description
--   + title, type, updated_at
ALTER TABLE feedback
    RENAME COLUMN user_id TO creator_id;
ALTER TABLE feedback
    RENAME COLUMN message TO description;
ALTER TABLE feedback
    ADD COLUMN title TEXT NOT NULL DEFAULT '',
    ADD COLUMN type TEXT NOT NULL DEFAULT 'other' CHECK (type IN ('bug','feature','improvement','other')),
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- Backfill legacy rows: title from the description prefix (80 chars, or a
-- placeholder when the description is empty), type stays "other" because the
-- legacy kind cannot be reliably recovered from the message.
UPDATE feedback SET title = LEFT(description, 80) WHERE title = '';
UPDATE feedback SET title = '(no title)' WHERE title = '';

-- List default ordering: workspace-scoped, newest first.
CREATE INDEX idx_feedback_workspace_created ON feedback(workspace_id, created_at DESC);
