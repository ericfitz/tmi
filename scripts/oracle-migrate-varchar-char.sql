-- oracle-migrate-varchar-char.sql
-- One-time migration that converts every VARCHAR2 column in the current
-- Oracle schema from BYTE semantics to CHAR semantics, by re-issuing the
-- ALTER ... MODIFY that GORM AutoMigrate elides.
--
-- Run on Oracle ADB after each batch deploy of #379 (Batches 1-5) on any
-- existing schema. New installs that auto-create tables from the new
-- DBVarchar / NullableDBVarchar types are born CHAR-mode and do not need
-- this script.
--
-- Idempotent: the WHERE filter targets only columns currently in BYTE mode,
-- so re-running produces no further changes.
--
-- Safe: ALTER ... MODIFY ... VARCHAR2(N CHAR) preserves existing row data
-- when the existing data fits in the new char-counted budget. Every TMI
-- column was sized in characters at design time and validated in characters
-- at the OpenAPI layer, so existing rows already satisfy the new constraint.
--
-- Verification after running:
--   @scripts/verify-oracle-char-semantics.sql
--   Expected result: empty.
--
-- Run as the TMI app schema owner. For ADMIN-owned schemas on Oracle ADB,
-- connect as ADMIN.

SET SERVEROUTPUT ON SIZE UNLIMITED;
DECLARE
    v_converted PLS_INTEGER := 0;
    v_failed    PLS_INTEGER := 0;
BEGIN
    FOR rec IN (
        SELECT TABLE_NAME, COLUMN_NAME, CHAR_LENGTH
        FROM USER_TAB_COLUMNS
        WHERE DATA_TYPE = 'VARCHAR2'
          AND CHAR_USED = 'B'
        ORDER BY TABLE_NAME, COLUMN_NAME
    ) LOOP
        BEGIN
            EXECUTE IMMEDIATE
                'ALTER TABLE "' || rec.TABLE_NAME ||
                '" MODIFY ("' || rec.COLUMN_NAME ||
                '" VARCHAR2(' || rec.CHAR_LENGTH || ' CHAR))';
            v_converted := v_converted + 1;
            DBMS_OUTPUT.PUT_LINE(
                'Converted ' || rec.TABLE_NAME || '.' || rec.COLUMN_NAME ||
                ' to VARCHAR2(' || rec.CHAR_LENGTH || ' CHAR)'
            );
        EXCEPTION
            WHEN OTHERS THEN
                v_failed := v_failed + 1;
                DBMS_OUTPUT.PUT_LINE(
                    'FAILED ' || rec.TABLE_NAME || '.' || rec.COLUMN_NAME ||
                    ': ' || SQLERRM
                );
        END;
    END LOOP;

    DBMS_OUTPUT.PUT_LINE('---');
    DBMS_OUTPUT.PUT_LINE('Summary: converted=' || v_converted || ' failed=' || v_failed);
END;
/
