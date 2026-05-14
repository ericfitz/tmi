-- verify-oracle-char-semantics.sql
-- Reports VARCHAR2 columns that are still in BYTE semantics mode after the
-- #379 remediation. Expected result after all batches: empty result set.
-- Any rows returned indicate columns that need investigation.
--
-- Run as the TMI app schema owner on Oracle ADB.

SELECT
    TABLE_NAME,
    COLUMN_NAME,
    DATA_LENGTH,
    CHAR_LENGTH,
    CHAR_USED,
    NULLABLE
FROM
    USER_TAB_COLUMNS
WHERE
    DATA_TYPE = 'VARCHAR2'
    AND CHAR_USED = 'B'
ORDER BY
    TABLE_NAME,
    COLUMN_NAME;
