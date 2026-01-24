# Oracle Autonomous Database (ADB) Configuration

TMI supports both PostgreSQL and Oracle Autonomous Database (ADB) as backend databases. This guide covers configuring TMI to use Oracle ADB.

## Prerequisites

1. **Oracle Autonomous Database instance** - Oracle ADB 21c or later (required for native JSON datatype support)
2. **Oracle Instant Client** - Required for the godror driver
3. **Wallet credentials** - Downloaded from Oracle Cloud Console

## Installing Oracle Instant Client

### macOS (Homebrew)

```bash
brew tap InstantClientTap/instantclient
brew install instantclient-basic
brew install instantclient-sdk
```

### Linux (RPM-based)

```bash
# Download from Oracle website or use yum repository
sudo yum install oracle-instantclient-basic
sudo yum install oracle-instantclient-devel
```

### Linux (Debian-based)

```bash
# Download .deb packages from Oracle website
sudo dpkg -i oracle-instantclient-basic_*.deb
sudo dpkg -i oracle-instantclient-devel_*.deb
```

Set the library path:

```bash
export LD_LIBRARY_PATH=/usr/lib/oracle/21/client64/lib:$LD_LIBRARY_PATH
```

## Downloading the Wallet

1. Log in to Oracle Cloud Console
2. Navigate to your Autonomous Database instance
3. Click **DB Connection**
4. Download the **Instance Wallet** (contains connection credentials and TLS certificates)
5. Extract the wallet to a secure location (e.g., `/opt/oracle/wallet`)

## Environment Variables

Configure TMI with the following environment variables:

```bash
# Database type selection
DATABASE_TYPE=oracle

# Oracle connection credentials
ORACLE_USER=your_db_user
ORACLE_PASSWORD=your_db_password

# Connection string (from tnsnames.ora in wallet)
# Format: (description=(address=...))
# Or use the TNS alias if TNS_ADMIN is set
ORACLE_CONNECT_STRING=tmi_high

# Wallet location (directory containing cwallet.sso, tnsnames.ora, etc.)
ORACLE_WALLET_LOCATION=/opt/oracle/wallet

# Optional: Set TNS_ADMIN to wallet location for TNS alias resolution
TNS_ADMIN=/opt/oracle/wallet
```

## Connection String Formats

### Using TNS Alias (Recommended)

If `TNS_ADMIN` points to the wallet directory containing `tnsnames.ora`:

```bash
ORACLE_CONNECT_STRING=tmi_high
```

The TNS alias (e.g., `tmi_high`, `tmi_medium`, `tmi_low`) determines the connection priority and resource allocation.

### Using Full Connect Descriptor

```bash
ORACLE_CONNECT_STRING="(description=(retry_count=20)(retry_delay=3)(address=(protocol=tcps)(port=1522)(host=adb.us-ashburn-1.oraclecloud.com))(connect_data=(service_name=abc123_tmi_high.adb.oraclecloud.com))(security=(ssl_server_dn_match=yes)))"
```

## Configuration File

Alternatively, configure in `config.yml`:

```yaml
database:
  type: oracle
  oracle:
    user: ${ORACLE_USER}
    password: ${ORACLE_PASSWORD}
    connect_string: ${ORACLE_CONNECT_STRING}
    wallet_location: ${ORACLE_WALLET_LOCATION}
```

## Database Schema

TMI automatically creates the required schema on first startup using GORM's AutoMigrate feature. The following tables are created:

- `users` - User accounts
- `groups` - Group definitions
- `threat_models` - Threat model metadata
- `diagrams` - Diagram data with JSON cells
- `threats` - Threat entries
- `assets` - Asset definitions
- `documents` - Document attachments
- `threat_model_access` - Access control entries
- `administrators` - Admin role assignments
- `webhook_subscriptions` - Webhook configurations
- `webhook_deliveries` - Webhook delivery history
- `webhook_quotas` - Rate limiting quotas
- `notification_queue` - Polling-based notifications (Oracle only)

## Notification System

PostgreSQL uses `LISTEN/NOTIFY` for real-time notifications. Since Oracle doesn't support this feature, TMI uses a **polling-based notification system** for Oracle deployments:

- Notifications are written to the `notification_queue` table
- A background process polls for new notifications at configurable intervals (default: 1 second)
- Processed notifications are automatically cleaned up after 1 hour

## Verifying the Connection

Start TMI and check the logs for successful database connection:

```bash
./bin/tmiserver --config=config.yml
```

Look for:

```
level=INFO msg="Database connection established" type=oracle
level=INFO msg="Polling notification service initialized"
```

## Troubleshooting

### ORA-12154: TNS:could not resolve the connect identifier

- Verify `TNS_ADMIN` points to the wallet directory
- Check that `tnsnames.ora` exists in the wallet directory
- Verify the TNS alias matches an entry in `tnsnames.ora`

### ORA-28759: failure to open file

- Verify `ORACLE_WALLET_LOCATION` is set correctly
- Ensure the wallet files (`cwallet.sso`, `ewallet.p12`) are readable
- Check file permissions on the wallet directory

### ORA-01017: invalid username/password

- Verify `ORACLE_USER` and `ORACLE_PASSWORD` are correct
- Ensure the user has been created in the Oracle ADB instance
- Check that the user has appropriate privileges

### Connection timeout

- Verify network connectivity to Oracle Cloud
- Check firewall rules allow outbound connections on port 1522
- Try using a different TNS alias (e.g., `_low` instead of `_high`)

## Performance Considerations

1. **TNS Alias Selection**:
   - `_high` - Highest priority, parallel queries enabled
   - `_medium` - Standard priority
   - `_low` - Background/batch processing

2. **JSON Operations**: Oracle 21c+ supports native JSON datatype with efficient storage and indexing

## Switching Between PostgreSQL and Oracle

TMI supports runtime database switching via the `DATABASE_TYPE` environment variable:

```bash
# Use PostgreSQL (default)
DATABASE_TYPE=postgres

# Use Oracle ADB
DATABASE_TYPE=oracle
```

The same application binary works with both databases - GORM handles dialect differences automatically.

---

<!-- Migrated from: docs/operator/oracle-adb-configuration.md on 2025-01-24 -->
<!-- Migrated to wiki: Database-Setup.md (Oracle Autonomous Database Setup section) -->

## Verification Summary

### Verified Items

| Item | Status | Evidence |
|------|--------|----------|
| `DATABASE_TYPE` environment variable | Verified | `internal/config/config.go:54`, `auth/config.go:163` |
| `ORACLE_USER` environment variable | Verified | `internal/config/config.go:65`, `auth/config.go:173` |
| `ORACLE_PASSWORD` environment variable | Verified | `internal/config/config.go:66`, `auth/config.go:174` |
| `ORACLE_CONNECT_STRING` environment variable | Verified | `internal/config/config.go:67`, `auth/config.go:175` |
| `ORACLE_WALLET_LOCATION` environment variable | Verified | `internal/config/config.go:68`, `auth/config.go:176` |
| `notification_queue` table for Oracle | Verified | `api/notifications/polling.go:16-27` |
| Polling default interval (1 second) | Verified | `api/notifications/service.go:59` |
| Notification cleanup after 1 hour | Verified | `api/notifications/polling.go:177-186` |
| Oracle Instant Client Homebrew tap | Verified | [InstantClientTap/homebrew-instantclient](https://github.com/InstantClientTap/homebrew-instantclient) |
| Oracle 21c JSON datatype requirement | Verified | [Oracle Database 21c JSON Data Type](https://oracle-base.com/articles/21c/json-data-type-21c) |
| godror driver requires Instant Client | Verified | [godror GitHub](https://github.com/godror/godror) |
| Make targets (start-dev-oci, test-integration-oci) | Verified | `Makefile:519-529`, `Makefile:609-616` |
| Config file format | Verified | `config-development-oci.yml` |

### Corrections Made

1. **Homebrew tap name**: Changed from `instantclienttap/instantclient` to `InstantClientTap/instantclient` (correct casing per GitHub)
2. **Connection pooling env vars removed**: `DB_MAX_OPEN_CONNS`, `DB_MAX_IDLE_CONNS`, `DB_CONN_MAX_LIFETIME` are not implemented in source code
3. **Notification poll interval**: Removed `NOTIFICATION_POLL_INTERVAL` env var documentation as it's not implemented (interval is hardcoded at 1 second)
4. **Log message format**: Updated to match actual source output format
5. **GORM auto-migration note**: Removed claim about auto-migration logging (specific log message not confirmed in source)
