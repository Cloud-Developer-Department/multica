-- Single statement: CREATE INDEX CONCURRENTLY cannot run inside a transaction
-- or share a multi-command migration file (repository hard rule).
--
-- Supports the child-squad lookups added for squad nesting (LIU-8): detail /
-- list responses load a parent's children by parent_squad_id, and the leader
-- briefing roster expands child-squad members. The partial predicate keeps the
-- index off top-level squads (the majority of rows).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_squad_parent_squad_id
    ON squad (parent_squad_id)
    WHERE parent_squad_id IS NOT NULL;
