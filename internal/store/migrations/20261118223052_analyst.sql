-- Create "analyst_runs" table
CREATE TABLE `analyst_runs` (
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
  CONSTRAINT `0` FOREIGN KEY (`asset_id`) REFERENCES `assets` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE,
  CHECK (status IN ('draft', 'approved', 'discarded', 'failed'))
);
-- Create index "analyst_runs_asset" to table: "analyst_runs"
CREATE INDEX `analyst_runs_asset` ON `analyst_runs` (`asset_id`, `id`);
-- Create index "analyst_runs_created" to table: "analyst_runs"
CREATE INDEX `analyst_runs_created` ON `analyst_runs` (`created_at`);
