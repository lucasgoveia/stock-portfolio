-- Create "fundamental_quarters" table
CREATE TABLE `fundamental_quarters` (
  `asset_id` integer NOT NULL,
  `period_end` text NOT NULL,
  `fiscal_quarter` integer NOT NULL,
  `revenue` text NOT NULL DEFAULT '',
  `net_income` text NOT NULL DEFAULT '',
  `eps` text NOT NULL DEFAULT '',
  `shares` text NOT NULL DEFAULT '',
  `derived` integer NOT NULL DEFAULT 0,
  `source` text NOT NULL,
  `filed` text NOT NULL DEFAULT '',
  PRIMARY KEY (`asset_id`, `period_end`),
  CONSTRAINT `0` FOREIGN KEY (`asset_id`) REFERENCES `assets` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE,
  CHECK (fiscal_quarter BETWEEN 1 AND 4),
  CHECK (derived IN (0, 1))
);
