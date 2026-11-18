-- Create "tactical_setups" table
CREATE TABLE `tactical_setups` (
  `asset_id` integer NULL,
  `note_id` integer NOT NULL,
  `entry_price` text NOT NULL,
  `target_price` text NOT NULL,
  `stop_price` text NOT NULL,
  `expires_on` text NOT NULL,
  `created_at` text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
  `updated_at` text NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
  PRIMARY KEY (`asset_id`),
  CONSTRAINT `0` FOREIGN KEY (`note_id`) REFERENCES `research_notes` (`id`) ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT `1` FOREIGN KEY (`asset_id`) REFERENCES `assets` (`id`) ON UPDATE NO ACTION ON DELETE CASCADE
);
