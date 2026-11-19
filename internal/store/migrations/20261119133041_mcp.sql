-- Disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- Create "new_analyst_runs" table
CREATE TABLE `new_analyst_runs` (
  `id` integer NULL PRIMARY KEY AUTOINCREMENT,
  `asset_id` integer NOT NULL,
  `model` text NOT NULL,
  `status` text NOT NULL DEFAULT 'draft',
  `inputs` text NOT NULL DEFAULT '{}',
  `output` text NOT NULL DEFAULT '{}',
  `input_tokens` integer NOT NULL DEFAULT 0,
  `output_tokens` integer NOT NULL DEFAULT 0,
  `cost_usd` text NOT NULL DEFAULT '0',
  `approvals` text NOT NULL DEFAULT '{}',
  `error` text NOT NULL DEFAULT '',
  `created_at` text NOT NULL,
  `source` text NOT NULL DEFAULT 'api',
  CONSTRAINT `0` FOREIGN KEY (`asset_id`) REFERENCES `assets` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE,
  CHECK (status IN ('draft', 'approved', 'discarded', 'failed')),
  CHECK (source IN ('api', 'mcp'))
);
-- Copy rows from old table "analyst_runs" to new temporary table "new_analyst_runs"
INSERT INTO `new_analyst_runs` (`id`, `asset_id`, `model`, `status`, `inputs`, `output`, `input_tokens`, `output_tokens`, `cost_usd`, `approvals`, `error`, `created_at`) SELECT `id`, `asset_id`, `model`, `status`, `inputs`, `output`, `input_tokens`, `output_tokens`, `cost_usd`, `approvals`, `error`, `created_at` FROM `analyst_runs`;
-- Drop "analyst_runs" table after copying rows
DROP TABLE `analyst_runs`;
-- Rename temporary table "new_analyst_runs" to "analyst_runs"
ALTER TABLE `new_analyst_runs` RENAME TO `analyst_runs`;
-- Create index "analyst_runs_asset" to table: "analyst_runs"
CREATE INDEX `analyst_runs_asset` ON `analyst_runs` (`asset_id`, `id`);
-- Create index "analyst_runs_created" to table: "analyst_runs"
CREATE INDEX `analyst_runs_created` ON `analyst_runs` (`created_at`);
-- Create "candidates" table
CREATE TABLE `candidates` (
  `id` integer NULL PRIMARY KEY AUTOINCREMENT,
  `ticker` text NOT NULL,
  `market` text NOT NULL,
  `name` text NOT NULL DEFAULT '',
  `cnpj` text NOT NULL DEFAULT '',
  `cik` text NOT NULL DEFAULT '',
  `note` text NOT NULL DEFAULT '',
  `output` text NOT NULL DEFAULT '{}',
  `provenance` text NOT NULL DEFAULT '{}',
  `status` text NOT NULL DEFAULT 'pending',
  `asset_id` integer NULL,
  `run_id` integer NULL,
  `created_at` text NOT NULL,
  `decided_at` text NOT NULL DEFAULT '',
  CONSTRAINT `0` FOREIGN KEY (`run_id`) REFERENCES `analyst_runs` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT `1` FOREIGN KEY (`asset_id`) REFERENCES `assets` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL,
  CHECK (market IN ('B3', 'US')),
  CHECK (status IN ('pending', 'accepted', 'rejected'))
);
-- Create index "candidates_status" to table: "candidates"
CREATE INDEX `candidates_status` ON `candidates` (`status`, `id`);
-- Create "lookup_cache" table
CREATE TABLE `lookup_cache` (
  `key` text NULL,
  `payload` text NOT NULL,
  `fetched_at` text NOT NULL,
  PRIMARY KEY (`key`)
);
-- Create "lookup_symbols" table
CREATE TABLE `lookup_symbols` (
  `day` text NOT NULL,
  `market` text NOT NULL,
  `symbol` text NOT NULL,
  PRIMARY KEY (`day`, `market`, `symbol`)
);
-- Enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
