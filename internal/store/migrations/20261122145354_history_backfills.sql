-- Create "history_backfills" table
CREATE TABLE `history_backfills` (
  `subject` text NOT NULL,
  `source` text NOT NULL,
  `from_date` text NOT NULL,
  `done_at` text NOT NULL,
  PRIMARY KEY (`subject`, `source`)
);
