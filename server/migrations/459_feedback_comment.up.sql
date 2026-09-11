-- Feedback comments. Kept deliberately simple (no nesting/@mention/reactions)
-- per the Feedback Center v1 scope.
CREATE TABLE feedback_comment (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    feedback_id UUID NOT NULL REFERENCES feedback(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    content     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_feedback_comment_feedback_id_created ON feedback_comment(feedback_id, created_at);
