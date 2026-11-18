-- Create "alerts" table
CREATE TABLE `alerts` (
  `id` integer NULL PRIMARY KEY AUTOINCREMENT,
  `type` text NOT NULL,
  `asset_id` integer NULL,
  `dedupe_key` text NOT NULL,
  `payload` text NOT NULL DEFAULT '{}',
  `state` text NOT NULL DEFAULT 'open',
  `snoozed_until` text NULL,
  `created_at` text NOT NULL,
  `updated_at` text NOT NULL,
  `resolved_at` text NULL,
  CONSTRAINT `0` FOREIGN KEY (`asset_id`) REFERENCES `assets` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE,
  CHECK (state IN ('open', 'snoozed', 'acknowledged', 'resolved'))
);
-- Create index "alerts_dedupe_active" to table: "alerts"
CREATE UNIQUE INDEX `alerts_dedupe_active` ON `alerts` (`dedupe_key`) WHERE state <> 'resolved';
-- Create index "alerts_state" to table: "alerts"
CREATE INDEX `alerts_state` ON `alerts` (`state`);
-- Create "assets" table
CREATE TABLE `assets` (
  `id` integer NULL PRIMARY KEY AUTOINCREMENT,
  `ticker` text NOT NULL,
  `name` text NOT NULL DEFAULT '',
  `market` text NOT NULL,
  `currency` text NOT NULL,
  `kind` text NOT NULL,
  `sleeve` text NOT NULL,
  `status` text NOT NULL DEFAULT 'watchlist',
  `benchmark` text NOT NULL,
  `created_at` text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
  `updated_at` text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
  CHECK (market IN ('B3', 'US')),
  CHECK (currency IN ('BRL', 'USD')),
  CHECK (kind IN ('stock', 'bdr', 'etf')),
  CHECK (sleeve IN ('core', 'tactical')),
  CHECK (status IN ('watchlist', 'holding', 'exited')),
  CHECK (benchmark IN ('IBOV', 'SP500'))
);
-- Create index "assets_market_ticker" to table: "assets"
CREATE UNIQUE INDEX `assets_market_ticker` ON `assets` (`market`, `ticker`);
-- Create "cash_instruments" table
CREATE TABLE `cash_instruments` (
  `id` integer NULL PRIMARY KEY AUTOINCREMENT,
  `kind` text NOT NULL,
  `name` text NOT NULL,
  `currency` text NOT NULL,
  `pct_cdi` text NOT NULL DEFAULT '100',
  `created_at` text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
  CHECK (kind IN ('tesouro_selic', 'cdi', 'usd_cash')),
  CHECK (currency IN ('BRL', 'USD'))
);
-- Create "cash_movements" table
CREATE TABLE `cash_movements` (
  `id` integer NULL PRIMARY KEY AUTOINCREMENT,
  `instrument_id` integer NOT NULL,
  `move_date` text NOT NULL,
  `type` text NOT NULL,
  `amount` text NOT NULL,
  `quantity` text NOT NULL DEFAULT '0',
  `created_at` text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
  CONSTRAINT `0` FOREIGN KEY (`instrument_id`) REFERENCES `cash_instruments` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE,
  CHECK (type IN ('deposit', 'withdraw', 'yield_adjust', 'balance'))
);
-- Create index "cash_movements_instrument" to table: "cash_movements"
CREATE INDEX `cash_movements_instrument` ON `cash_movements` (`instrument_id`, `move_date`, `id`);
-- Create "cdi_rates" table
CREATE TABLE `cdi_rates` (
  `ref_date` text NULL,
  `rate_pct` text NOT NULL,
  PRIMARY KEY (`ref_date`)
);
-- Create "tesouro_selic_prices" table
CREATE TABLE `tesouro_selic_prices` (
  `ref_date` text NULL,
  `price` text NOT NULL,
  PRIMARY KEY (`ref_date`)
);
-- Create "fx_rates" table
CREATE TABLE `fx_rates` (
  `pair` text NOT NULL,
  `ref_date` text NOT NULL,
  `rate` text NOT NULL,
  PRIMARY KEY (`pair`, `ref_date`)
);
-- Create "fair_values" table
CREATE TABLE `fair_values` (
  `id` integer NULL PRIMARY KEY AUTOINCREMENT,
  `asset_id` integer NOT NULL,
  `method` text NOT NULL,
  `value_per_share` text NOT NULL,
  `currency` text NOT NULL,
  `as_of` text NOT NULL,
  `inputs` text NOT NULL DEFAULT '{}',
  `sources` text NOT NULL DEFAULT '[]',
  `note` text NOT NULL DEFAULT '',
  `created_at` text NOT NULL,
  CONSTRAINT `0` FOREIGN KEY (`asset_id`) REFERENCES `assets` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE,
  CHECK (method IN ('scenario_dcf', 'bazin', 'graham', 'manual', 'target_price')),
  CHECK (currency IN ('BRL', 'USD'))
);
-- Create index "fair_values_asset" to table: "fair_values"
CREATE INDEX `fair_values_asset` ON `fair_values` (`asset_id`, `as_of`, `id`);
-- Create "ratings" table
CREATE TABLE `ratings` (
  `id` integer NULL PRIMARY KEY AUTOINCREMENT,
  `asset_id` integer NOT NULL,
  `rated_on` text NOT NULL,
  `volatility` integer NOT NULL,
  `understanding` integer NOT NULL,
  `moat` integer NOT NULL,
  `predictability` integer NOT NULL,
  `horizon` integer NOT NULL,
  `reason` text NOT NULL,
  `created_at` text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
  CONSTRAINT `0` FOREIGN KEY (`asset_id`) REFERENCES `assets` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE,
  CHECK (volatility BETWEEN 1 AND 5),
  CHECK (understanding BETWEEN 1 AND 5),
  CHECK (moat BETWEEN 1 AND 5),
  CHECK (predictability BETWEEN 1 AND 5),
  CHECK (horizon BETWEEN 1 AND 5)
);
-- Create index "ratings_asset_date" to table: "ratings"
CREATE INDEX `ratings_asset_date` ON `ratings` (`asset_id`, `rated_on`, `id`);
-- Create "research_notes" table
CREATE TABLE `research_notes` (
  `id` integer NULL PRIMARY KEY AUTOINCREMENT,
  `asset_id` integer NOT NULL,
  `body` text NOT NULL,
  `sources` text NOT NULL DEFAULT '[]',
  `author` text NOT NULL DEFAULT 'user',
  `opportunity` integer NOT NULL DEFAULT 0,
  `opportunity_expires_on` text NULL,
  `created_at` text NOT NULL,
  `updated_at` text NOT NULL,
  CONSTRAINT `0` FOREIGN KEY (`asset_id`) REFERENCES `assets` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE,
  CHECK (author IN ('user', 'ai_approved')),
  CHECK (opportunity IN (0, 1)),
  CHECK (opportunity = 0 OR opportunity_expires_on IS NOT NULL)
);
-- Create index "research_notes_asset" to table: "research_notes"
CREATE INDEX `research_notes_asset` ON `research_notes` (`asset_id`, `id`);
-- Create "settings" table
CREATE TABLE `settings` (
  `key` text NULL,
  `value` text NOT NULL,
  PRIMARY KEY (`key`)
);
-- Create "theses" table
CREATE TABLE `theses` (
  `id` integer NULL PRIMARY KEY AUTOINCREMENT,
  `asset_id` integer NOT NULL,
  `body` text NOT NULL,
  `status` text NOT NULL DEFAULT 'active',
  `next_review_on` text NULL,
  `created_at` text NOT NULL,
  `updated_at` text NOT NULL,
  CONSTRAINT `0` FOREIGN KEY (`asset_id`) REFERENCES `assets` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE,
  CHECK (status IN ('active', 'under_review', 'broken'))
);
-- Create index "theses_asset_id" to table: "theses"
CREATE UNIQUE INDEX `theses_asset_id` ON `theses` (`asset_id`);
-- Create "thesis_break_conditions" table
CREATE TABLE `thesis_break_conditions` (
  `id` integer NULL PRIMARY KEY AUTOINCREMENT,
  `thesis_id` integer NOT NULL,
  `body` text NOT NULL,
  `created_at` text NOT NULL,
  `updated_at` text NOT NULL,
  CONSTRAINT `0` FOREIGN KEY (`thesis_id`) REFERENCES `theses` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "thesis_break_conditions_thesis" to table: "thesis_break_conditions"
CREATE INDEX `thesis_break_conditions_thesis` ON `thesis_break_conditions` (`thesis_id`, `id`);
-- Create "thesis_reviews" table
CREATE TABLE `thesis_reviews` (
  `id` integer NULL PRIMARY KEY AUTOINCREMENT,
  `thesis_id` integer NOT NULL,
  `reviewed_on` text NOT NULL,
  `note` text NOT NULL,
  `status_after` text NOT NULL,
  `next_review_on` text NULL,
  `created_at` text NOT NULL,
  CONSTRAINT `0` FOREIGN KEY (`thesis_id`) REFERENCES `theses` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE,
  CHECK (status_after IN ('active', 'under_review', 'broken'))
);
-- Create index "thesis_reviews_thesis" to table: "thesis_reviews"
CREATE INDEX `thesis_reviews_thesis` ON `thesis_reviews` (`thesis_id`, `reviewed_on`, `id`);
-- Create "trades" table
CREATE TABLE `trades` (
  `id` integer NULL PRIMARY KEY AUTOINCREMENT,
  `asset_id` integer NOT NULL,
  `trade_date` text NOT NULL,
  `side` text NOT NULL,
  `quantity` text NOT NULL,
  `price` text NOT NULL,
  `fees` text NOT NULL DEFAULT '0',
  `currency` text NOT NULL,
  `fx_rate` text NOT NULL DEFAULT '1',
  `created_at` text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
  CONSTRAINT `0` FOREIGN KEY (`asset_id`) REFERENCES `assets` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE,
  CHECK (side IN ('buy', 'sell')),
  CHECK (currency IN ('BRL', 'USD'))
);
-- Create index "trades_asset_date" to table: "trades"
CREATE INDEX `trades_asset_date` ON `trades` (`asset_id`, `trade_date`, `id`);
