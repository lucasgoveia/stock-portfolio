-- Create "performance_snapshots" table
CREATE TABLE `performance_snapshots` (
  `ref_date` text NOT NULL,
  `segment` text NOT NULL,
  `value` text NOT NULL,
  `flow` text NOT NULL DEFAULT '0',
  PRIMARY KEY (`ref_date`, `segment`)
);
