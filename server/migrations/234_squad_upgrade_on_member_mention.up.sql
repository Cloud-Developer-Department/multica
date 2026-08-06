-- Squad-level mention-upgrade switch (LIU-9 subtask A / F3): when true (the
-- default), @-mentioning an ordinary squad member whose only membership is
-- this squad upgrades the mention to a squad-level trigger that wakes the
-- Leader (B01 serial semantics: the mentioned member's personal task defers).
-- When false the safety valve is closed and member mentions stay personal.
--
-- Column is NOT NULL DEFAULT TRUE so existing squads opt in without a
-- backfill; the handler exposes it on create (I1) and update (I3, subtask A
-- owns only this field).
ALTER TABLE squad ADD COLUMN upgrade_on_member_mention BOOLEAN NOT NULL DEFAULT TRUE;
