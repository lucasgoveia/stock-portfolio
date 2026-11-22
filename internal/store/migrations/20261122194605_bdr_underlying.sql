-- Disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- Create "new_assets" table
CREATE TABLE `new_assets` (
  `id` integer NULL PRIMARY KEY AUTOINCREMENT,
  `ticker` text NOT NULL,
  `name` text NOT NULL DEFAULT '',
  `market` text NOT NULL,
  `currency` text NOT NULL,
  `kind` text NOT NULL,
  `sleeve` text NOT NULL,
  `status` text NOT NULL DEFAULT 'watchlist',
  `benchmark` text NOT NULL,
  `cnpj` text NOT NULL DEFAULT '',
  `cik` text NOT NULL DEFAULT '',
  `underlying_asset_id` integer NULL,
  `bdr_ratio` text NOT NULL DEFAULT '',
  `created_at` text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
  `updated_at` text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
  CONSTRAINT `0` FOREIGN KEY (`underlying_asset_id`) REFERENCES `assets` (`id`) ON UPDATE NO ACTION ON DELETE NO ACTION,
  CHECK (market IN ('B3', 'US')),
  CHECK (currency IN ('BRL', 'USD')),
  CHECK (kind IN ('stock', 'bdr', 'etf')),
  CHECK (sleeve IN ('core', 'tactical')),
  CHECK (status IN ('watchlist', 'holding', 'exited')),
  CHECK (benchmark IN ('IBOV', 'SP500'))
);
-- Copy rows from old table "assets" to new temporary table "new_assets"
INSERT INTO `new_assets` (`id`, `ticker`, `name`, `market`, `currency`, `kind`, `sleeve`, `status`, `benchmark`, `cnpj`, `cik`, `created_at`, `updated_at`) SELECT `id`, `ticker`, `name`, `market`, `currency`, `kind`, `sleeve`, `status`, `benchmark`, `cnpj`, `cik`, `created_at`, `updated_at` FROM `assets`;
-- Drop "assets" table after copying rows
DROP TABLE `assets`;
-- Rename temporary table "new_assets" to "assets"
ALTER TABLE `new_assets` RENAME TO `assets`;
-- Create index "assets_market_ticker" to table: "assets"
CREATE UNIQUE INDEX `assets_market_ticker` ON `assets` (`market`, `ticker`);
-- Enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
