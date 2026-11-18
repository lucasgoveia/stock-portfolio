-- Disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- Create "new_ratings" table
CREATE TABLE `new_ratings` (
  `id` integer NULL PRIMARY KEY AUTOINCREMENT,
  `asset_id` integer NOT NULL,
  `rated_on` text NOT NULL,
  `volatility` integer NOT NULL,
  `understanding` integer NOT NULL,
  `moat` integer NOT NULL,
  `predictability` integer NOT NULL,
  `horizon` integer NOT NULL,
  `reason` text NOT NULL,
  `volatility_auto` integer NOT NULL DEFAULT 0,
  `created_at` text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
  CONSTRAINT `0` FOREIGN KEY (`asset_id`) REFERENCES `assets` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE,
  CHECK (volatility BETWEEN 1 AND 5),
  CHECK (understanding BETWEEN 1 AND 5),
  CHECK (moat BETWEEN 1 AND 5),
  CHECK (predictability BETWEEN 1 AND 5),
  CHECK (horizon BETWEEN 1 AND 5),
  CHECK (volatility_auto IN (0, 1))
);
-- Copy rows from old table "ratings" to new temporary table "new_ratings"
INSERT INTO `new_ratings` (`id`, `asset_id`, `rated_on`, `volatility`, `understanding`, `moat`, `predictability`, `horizon`, `reason`, `created_at`) SELECT `id`, `asset_id`, `rated_on`, `volatility`, `understanding`, `moat`, `predictability`, `horizon`, `reason`, `created_at` FROM `ratings`;
-- Drop "ratings" table after copying rows
DROP TABLE `ratings`;
-- Rename temporary table "new_ratings" to "ratings"
ALTER TABLE `new_ratings` RENAME TO `ratings`;
-- Create index "ratings_asset_date" to table: "ratings"
CREATE INDEX `ratings_asset_date` ON `ratings` (`asset_id`, `rated_on`, `id`);
-- Create "asset_betas" table
CREATE TABLE `asset_betas` (
  `asset_id` integer NULL,
  `index_code` text NOT NULL,
  `beta` text NOT NULL,
  `weeks` integer NOT NULL,
  `computed_on` text NOT NULL,
  PRIMARY KEY (`asset_id`),
  CONSTRAINT `0` FOREIGN KEY (`asset_id`) REFERENCES `assets` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
