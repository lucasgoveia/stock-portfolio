-- Create "corporate_actions" table
CREATE TABLE `corporate_actions` (
  `id` integer NULL PRIMARY KEY AUTOINCREMENT,
  `asset_id` integer NOT NULL,
  `kind` text NOT NULL,
  `ex_date` text NOT NULL,
  `factor` text NOT NULL,
  `cost_per_share` text NOT NULL DEFAULT '0',
  `status` text NOT NULL DEFAULT 'confirmed',
  `source` text NOT NULL DEFAULT 'manual',
  `price_ratio` text NULL,
  `note` text NOT NULL DEFAULT '',
  `created_at` text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
  CONSTRAINT `0` FOREIGN KEY (`asset_id`) REFERENCES `assets` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE,
  CHECK (kind IN ('split', 'reverse_split', 'bonus')),
  CHECK (status IN ('detected', 'confirmed', 'rejected'))
);
-- Create index "corporate_actions_detected" to table: "corporate_actions"
CREATE UNIQUE INDEX `corporate_actions_detected` ON `corporate_actions` (`asset_id`, `kind`, `ex_date`, `source`) WHERE source <> 'manual';
-- Create index "corporate_actions_asset" to table: "corporate_actions"
CREATE INDEX `corporate_actions_asset` ON `corporate_actions` (`asset_id`, `ex_date`);
