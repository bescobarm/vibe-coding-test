-- The anomaly signals query (D-059) finds a cardholder's cards on every
-- authorization. Without this index it scans the whole cards table, a cost
-- that grows with every card issued (docs/SQL_REVIEW.md, I-029).
CREATE INDEX cards_cardholder_idx ON cards (cardholder_id);
