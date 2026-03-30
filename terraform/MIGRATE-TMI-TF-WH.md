# Migrating tmi-tf-wh Out of TMI Terraform Templates

The tmi-tf-wh webhook analyzer is no longer managed by the TMI Terraform templates.
It is now deployed separately. If you previously had `tmi_tf_wh_enabled = true`,
follow these steps to cleanly remove the resources before updating to the latest templates.

## Steps

1. **Set `tmi_tf_wh_enabled = false`** in your `terraform.tfvars`:

   ```hcl
   tmi_tf_wh_enabled = false
   ```

2. **Preview the changes:**

   ```bash
   terraform plan
   ```

   Verify only tmi-tf-wh resources will be destroyed:
   - `oci_artifacts_container_repository.tmi_tf_wh`
   - `oci_queue_queue.tmi_tf_wh`
   - IAM policy statement updates (queue and generative-ai-family permissions removed)
   - `kubernetes_service_account_v1.tmi_tf_wh`
   - `kubernetes_config_map_v1.tmi_tf_wh`
   - `kubernetes_deployment_v1.tmi_tf_wh`
   - `kubernetes_service_v1.tmi_tf_wh`

3. **Apply:**

   ```bash
   terraform apply
   ```

4. **Remove tmi-tf-wh variables** from your `terraform.tfvars`:

   Delete any lines starting with `tmi_tf_wh_`:
   ```
   tmi_tf_wh_enabled
   tmi_tf_wh_image_url
   tmi_tf_wh_extra_env_vars
   ```

5. **Update to the latest TMI Terraform templates** (which no longer contain tmi-tf-wh).

## If You Never Enabled tmi-tf-wh

No action needed. Simply update to the latest templates.
