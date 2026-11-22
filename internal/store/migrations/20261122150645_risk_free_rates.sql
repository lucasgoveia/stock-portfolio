-- Create "risk_free_rates" table
CREATE TABLE `risk_free_rates` (
  `code` text NOT NULL,
  `ref_date` text NOT NULL,
  `rate` text NOT NULL,
  PRIMARY KEY (`code`, `ref_date`)
);
