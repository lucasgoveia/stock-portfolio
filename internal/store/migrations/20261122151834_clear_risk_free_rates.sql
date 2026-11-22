-- Stored rates came from the 10-year Treasury and the Tesouro Prefixado; the
-- policy rates replace them and are refetched on startup and by the daily job.
DELETE FROM risk_free_rates;
