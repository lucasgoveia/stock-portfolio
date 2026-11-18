-- Add column "cnpj" to table: "assets"
ALTER TABLE `assets` ADD COLUMN `cnpj` text NOT NULL DEFAULT '';
-- Add column "cik" to table: "assets"
ALTER TABLE `assets` ADD COLUMN `cik` text NOT NULL DEFAULT '';
-- Create "fundamental_facts" table
CREATE TABLE `fundamental_facts` (
  `asset_id` integer NOT NULL,
  `period_end` text NOT NULL,
  `period_type` text NOT NULL,
  `concept` text NOT NULL,
  `value` text NOT NULL,
  `source` text NOT NULL,
  `filed` text NOT NULL DEFAULT '',
  PRIMARY KEY (`asset_id`, `period_end`, `period_type`, `concept`),
  CONSTRAINT `0` FOREIGN KEY (`asset_id`) REFERENCES `assets` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE,
  CHECK (period_type IN ('annual', 'quarterly', 'ttm'))
);
-- Create "fundamental_syncs" table
CREATE TABLE `fundamental_syncs` (
  `asset_id` integer NULL,
  `source` text NOT NULL DEFAULT '',
  `fetched_at` text NOT NULL,
  `as_of` text NOT NULL DEFAULT '',
  `status` text NOT NULL,
  `message` text NOT NULL DEFAULT '',
  `notes` text NOT NULL DEFAULT '[]',
  PRIMARY KEY (`asset_id`),
  CONSTRAINT `0` FOREIGN KEY (`asset_id`) REFERENCES `assets` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE,
  CHECK (status IN ('ok', 'error'))
);
