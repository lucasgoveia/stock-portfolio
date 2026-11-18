-- Create "asset_prices" table
CREATE TABLE `asset_prices` (
  `asset_id` integer NOT NULL,
  `ref_date` text NOT NULL,
  `source` text NOT NULL,
  `close` text NOT NULL,
  `adj_close` text NULL,
  `fetched_at` text NOT NULL,
  PRIMARY KEY (`asset_id`, `ref_date`, `source`),
  CONSTRAINT `0` FOREIGN KEY (`asset_id`) REFERENCES `assets` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE,
  CHECK (source IN ('cotahist', 'brapi', 'tiingo', 'manual'))
);
-- Create "index_points" table
CREATE TABLE `index_points` (
  `index_code` text NOT NULL,
  `ref_date` text NOT NULL,
  `close` text NOT NULL,
  PRIMARY KEY (`index_code`, `ref_date`)
);
-- Create "job_runs" table
CREATE TABLE `job_runs` (
  `id` integer NULL PRIMARY KEY AUTOINCREMENT,
  `job` text NOT NULL,
  `run_date` text NOT NULL,
  `started_at` text NOT NULL,
  `finished_at` text NULL,
  `status` text NOT NULL,
  `steps` text NOT NULL DEFAULT '[]',
  CHECK (status IN ('running', 'ok', 'partial', 'failed'))
);
-- Create index "job_runs_job" to table: "job_runs"
CREATE INDEX `job_runs_job` ON `job_runs` (`job`, `id`);
