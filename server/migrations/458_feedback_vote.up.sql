-- Feedback vote (like). Hard uniqueness on (feedback_id, user_id) guarantees a
-- user can support a feedback at most once; the API layer makes it idempotent
-- with ON CONFLICT DO NOTHING.
CREATE TABLE feedback_vote (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    feedback_id UUID NOT NULL REFERENCES feedback(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (feedback_id, user_id)
);

CREATE INDEX idx_feedback_vote_feedback_id ON feedback_vote(feedback_id);
