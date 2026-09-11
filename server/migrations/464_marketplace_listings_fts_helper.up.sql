-- F-523 (CLO-617): IMMUTABLE wrapper over to_tsvector so the marketplace FTS
-- index can include the tags text[] column. array_to_string(text[], text) is
-- STABLE in PostgreSQL (collation-sensitive), so an index expression calling
-- it directly is rejected with 42P17; wrapping it in an explicitly-IMMUTABLE
-- SQL function is the standard workaround and keeps the index safely
-- maintainable.
--
-- The function is deliberately single-purpose: it builds the search vector
-- over the same fields the keyword search filters on (title, summary, tags,
-- author display name) with the 'simple' config so identifiers and short tags
-- match predictably.
CREATE OR REPLACE FUNCTION marketplace_listings_tsvector(
    p_title text,
    p_summary text,
    p_tags text[],
    p_author text
) RETURNS tsvector
LANGUAGE sql IMMUTABLE
AS $$
    SELECT to_tsvector('simple'::regconfig,
        p_title || ' ' || p_summary || ' ' ||
        coalesce(array_to_string(p_tags, ' '), '') || ' ' ||
        p_author)
$$;
