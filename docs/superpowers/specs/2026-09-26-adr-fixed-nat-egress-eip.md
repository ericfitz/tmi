# ADR: Fixed NAT egress EIP and account-level AWS logging

Date: 2026-09-26. Status: accepted.
Full design: `tmi-tf-wh/docs/superpowers/specs/2026-09-26-fixed-egress-eip-and-aws-logging-design.md`.

## Human-made architectural decisions (Eric, 2026-09-26)

1. The NAT egress Elastic IP **`34.232.165.1`** (`eipalloc-07c325e51173c0bc9`, account
   967218005408, us-east-1) is **never released** unless Eric explicitly instructs it, naming
   the address. It survives a full undeploy/redeploy. Reason: an external system monitors
   traffic from this address.
2. The EIP lives in a long-lived Terraform stack, `terraform/environments/aws-persistent`
   (state key `tmi/aws-persistent/terraform.tfstate`), which no script destroys. `aws-public`
   looks it up by tag `Name=tmi-nat-eip` and passes it to the network module.
3. Guard layers: Terraform `prevent_destroy`; IAM policy `deny-release-tmi-nat-eip` (explicit
   Deny on releasing that allocation) attached to every admin user; tags `Retain=true`,
   `ReleasePolicy=explicit-owner-approval-only`; a Claude Code PreToolUse hook; memories and
   CLAUDE.md lines; source comments.
4. Logging with 30-day retention everywhere: CloudTrail (multi-region, management events,
   log-file validation, writes to the Terraform state bucket) to S3 and CloudWatch Logs;
   EventBridge alerts to SNS topic `tmi-security-alerts` (email security@tmi.dev); EKS `audit`
   and `authenticator` logs; IAM Access Analyzer; VPC flow logs; ALB access logs (tmi-server
   and tmi-webhooks); SQS DLQ alarm; RDS `postgresql` log export with connection logging.
   Not adopted: GuardDuty, Config, Security Hub, Resolver query logs.
5. Every step that mutates the live AWS deployment waits for Eric's explicit approval.

## Consequences

- Undeploying `aws-public` leaves the EIP allocated (about $3.60/month while idle) and leaves
  the log bucket, trail, and alerts running.
- The account has no AWS Organization, so there is no SCP; the root user is not restricted by
  the IAM deny. Root activity raises an alert instead.
- `aws-persistent` must be applied before `aws-public` in a fresh account.

## Release and recovery procedure

Release only on Eric's explicit instruction naming `34.232.165.1`: detach
`deny-release-tmi-nat-eip`, remove `prevent_destroy`, then destroy or release. Expect alerts.

If the address is ever released by accident, immediately try to reclaim it:

```bash
aws ec2 allocate-address --domain vpc --address 34.232.165.1 --profile tmi --region us-east-1
```

This works only if AWS has not already handed the address to another account. Then
re-import it into `aws-persistent` (update the `import` block's id) and re-apply.
