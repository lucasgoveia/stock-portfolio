-- Create "contributions" table
CREATE TABLE `contributions` (
  `id` integer NULL PRIMARY KEY AUTOINCREMENT,
  `amount` text NOT NULL,
  `plan` text NOT NULL,
  `created_at` text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
  `discarded_at` text NULL
);
-- Create "contribution_orders" table
CREATE TABLE `contribution_orders` (
  `contribution_id` integer NOT NULL,
  `seq` integer NOT NULL,
  `status` text NOT NULL DEFAULT 'open',
  `trade_id` integer NULL,
  `updated_at` text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
  PRIMARY KEY (`contribution_id`, `seq`),
  CONSTRAINT `0` FOREIGN KEY (`trade_id`) REFERENCES `trades` (`id`) ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT `1` FOREIGN KEY (`contribution_id`) REFERENCES `contributions` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE,
  CHECK (status IN ('open', 'done', 'skipped'))
);
