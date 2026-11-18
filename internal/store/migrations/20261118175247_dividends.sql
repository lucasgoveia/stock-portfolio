-- Create "dividends" table
CREATE TABLE `dividends` (
  `id` integer NULL PRIMARY KEY AUTOINCREMENT,
  `asset_id` integer NOT NULL,
  `kind` text NOT NULL,
  `record_date` text NOT NULL,
  `pay_date` text NULL,
  `quantity` text NOT NULL,
  `per_share` text NOT NULL,
  `gross` text NOT NULL,
  `withholding` text NOT NULL DEFAULT '0',
  `net` text NOT NULL,
  `currency` text NOT NULL,
  `fx_rate` text NOT NULL DEFAULT '1',
  `status` text NOT NULL DEFAULT 'confirmed',
  `source` text NOT NULL DEFAULT 'manual',
  `movement_id` integer NULL,
  `contribution_id` integer NULL,
  `created_at` text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
  CONSTRAINT `0` FOREIGN KEY (`contribution_id`) REFERENCES `contributions` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT `1` FOREIGN KEY (`movement_id`) REFERENCES `cash_movements` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT `2` FOREIGN KEY (`asset_id`) REFERENCES `assets` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE,
  CHECK (kind IN ('dividend', 'jcp')),
  CHECK (currency IN ('BRL', 'USD')),
  CHECK (status IN ('detected', 'confirmed', 'rejected'))
);
-- Create index "dividends_detected" to table: "dividends"
CREATE UNIQUE INDEX `dividends_detected` ON `dividends` (`asset_id`, `kind`, `record_date`, `source`) WHERE source <> 'manual';
-- Create index "dividends_asset" to table: "dividends"
CREATE INDEX `dividends_asset` ON `dividends` (`asset_id`, `record_date`);
