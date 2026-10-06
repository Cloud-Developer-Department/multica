-- Roll back the feedback table to the legacy message-only shape.
DROP INDEX IF EXISTS idx_feedback_workspace_created;

ALTER TABLE feedback
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS type,
    DROP COLUMN IF EXISTS title;
ALTER TABLE feedback
    RENAME COLUMN creator_id TO user_id;
ALTER TABLE feedback
    RENAME COLUMN description TO message;
