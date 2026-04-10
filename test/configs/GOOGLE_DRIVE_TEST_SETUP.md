# Google Drive Integration Test Setup

## Overview

Integration tests for document access tracking and content provider infrastructure (#232) use real Google Drive files. This guide walks you through creating the GCP service account and test documents.

## Step 1: Create a GCP Service Account

1. Go to [GCP Console > IAM > Service Accounts](https://console.cloud.google.com/iam-admin/serviceaccounts)
2. Select your project (or create one, e.g. `tmi-testing`)
3. Click **Create Service Account**
   - Name: `tmi-test-gdrive`
   - Description: `TMI integration test — Google Drive read access`
4. Skip the optional "Grant access" and "Grant users access" steps (no roles needed — access is via file sharing)
5. Click on the created service account, go to **Keys** tab
6. Click **Add Key > Create new key > JSON**
7. Save the downloaded JSON file to:

```
test/configs/google-drive-credentials.json
```

This path is already gitignored (falls under the allowlist pattern). **Do not commit this file.**

8. Note the service account email (looks like `tmi-test-gdrive@your-project.iam.gserviceaccount.com`) — you'll need it for file sharing and config.

## Step 2: Create Test Documents in Google Drive

Create 3 items in your personal Google Drive. Use any Google account.

### Document 1: Accessible Google Doc (Workspace export path)

1. Create a new **Google Doc**
2. Title: `TMI Test - Accessible Doc`
3. Add some body text, e.g.: `This is a test document for TMI integration tests. It contains sample content for the content provider pipeline.`
4. Click **Share**, add the service account email with **Viewer** access
5. Copy the doc URL (looks like `https://docs.google.com/document/d/FILE_ID/edit`)
6. Note the **FILE_ID** from the URL

### Document 2: Inaccessible Google Doc (auth_required path)

1. Create a new **Google Doc**
2. Title: `TMI Test - Inaccessible Doc`
3. Add some body text (content doesn't matter — the service account can't read it)
4. **Do NOT share** this document with the service account
5. Copy the doc URL and note the **FILE_ID**

### Document 3: Accessible PDF (binary download path)

1. Upload a small **PDF file** to Google Drive (any PDF, keep it under 1 MB)
2. Title: `TMI Test - Accessible PDF`
3. Click **Share**, add the service account email with **Viewer** access
4. Copy the file URL (looks like `https://drive.google.com/file/d/FILE_ID/view`)
5. Note the **FILE_ID**

## Step 3: Create the Test Fixture File

Create `test/configs/google-drive-test-docs.json` with the file IDs and URLs from Step 2:

```json
{
  "service_account_email": "tmi-test-gdrive@your-project.iam.gserviceaccount.com",
  "accessible_doc": {
    "file_id": "PASTE_FILE_ID_HERE",
    "url": "https://docs.google.com/document/d/PASTE_FILE_ID_HERE/edit",
    "description": "Google Doc shared with service account (Workspace export path)"
  },
  "inaccessible_doc": {
    "file_id": "PASTE_FILE_ID_HERE",
    "url": "https://docs.google.com/document/d/PASTE_FILE_ID_HERE/edit",
    "description": "Google Doc NOT shared with service account (auth_required path)"
  },
  "accessible_pdf": {
    "file_id": "PASTE_FILE_ID_HERE",
    "url": "https://drive.google.com/file/d/PASTE_FILE_ID_HERE/view",
    "description": "PDF file shared with service account (binary download path)"
  }
}
```

**Do not commit this file.**

## Step 4: Verify Setup

After placing both files, verify the directory looks like:

```
test/configs/
  google-drive-credentials.json    (service account key - gitignored)
  google-drive-test-docs.json      (file IDs and URLs - gitignored)
  GOOGLE_DRIVE_TEST_SETUP.md       (this file - committed)
```

## How Tests Use These Files

- Tests check for `test/configs/google-drive-credentials.json` at startup
- If the file is missing, Google Drive integration tests are **skipped** (not failed)
- The credentials file is used to create a `GoogleDriveSource` instance
- The test docs file provides URLs for creating documents in the test threat model
- Tests exercise: document creation with access detection, access validation, content fetch, access poller status transitions, and Timmy session document skipping

## Gitignore

Both JSON files are explicitly ignored in `.gitignore`. The `.md` setup guide is committed.
