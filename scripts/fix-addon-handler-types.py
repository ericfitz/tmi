#!/usr/bin/env python3
"""
Fix type mismatches in addon handler files to work with OpenAPI-generated types.

This script updates field names and adds type conversions where needed.
"""
# /// script
# dependencies = []
# ///

import re
import sys
from pathlib import Path

def fix_addon_handlers(file_path):
    """Fix addon_handlers.go type issues."""
    with open(file_path, 'r') as f:
        content = f.read()

    # Fix field name mismatches (ID -> Id, WebhookID -> WebhookId, etc.)
    content = re.sub(r'req\.WebhookID', 'req.WebhookId', content)
    content = re.sub(r'req\.ThreatModelID', 'req.ThreatModelId', content)
    content = re.sub(r'addon\.ID', 'addon.ID', content)  # Keep internal type as-is

    # Fix validation calls to handle pointers
    content = re.sub(
        r'if err := ValidateAddonDescription\(req\.Description\);',
        'if err := ValidateAddonDescription(fromStringPtr(req.Description));',
        content
    )
    content = re.sub(
        r'if err := ValidateIcon\(req\.Icon\);',
        'if err := ValidateIcon(fromStringPtr(req.Icon));',
        content
    )
    content = re.sub(
        r'if err := ValidateObjects\(req\.Objects\);',
        'if err := ValidateObjects(fromObjectsSlicePtr(req.Objects));',
        content
    )

    # Fix Addon struct initialization
    content = re.sub(
        r'addon := &Addon\{[\s\S]*?\}',
        '''addon := &Addon{
\t\tCreatedAt:     time.Now(),
\t\tName:          req.Name,
\t\tWebhookID:     req.WebhookId,
\t\tDescription:   fromStringPtr(req.Description),
\t\tIcon:          fromStringPtr(req.Icon),
\t\tObjects:       fromObjectsSlicePtr(req.Objects),
\t\tThreatModelID: req.ThreatModelId,
\t}''',
        content,
        count=1
    )

    # Fix AddonResponse struct initialization - use converter
    content = re.sub(
        r'response := AddonResponse\{[\s\S]*?\}',
        'response := addonToResponse(addon)',
        content
    )

    # Fix List response conversion
    content = re.sub(
        r'responses := make\(\[\]AddonResponse, len\(addons\)\)\s+for i, addon := range addons \{\s+responses\[i\] = AddonResponse\(addon\)\s+\}',
        '''responses := make([]AddonResponse, len(addons))
\tfor i, addon := range addons {
\t\tresponses[i] = addonToResponse(&addon)
\t}''',
        content
    )

    with open(file_path, 'w') as f:
        f.write(content)

    print(f"✓ Fixed {file_path}")

def fix_invocation_handlers(file_path):
    """Fix addon_invocation_handlers.go type issues."""
    with open(file_path, 'r') as f:
        content = f.read()

    # Remove unused import
    content = content.replace('\t"encoding/json"\n', '')

    # Fix field name mismatches
    content = re.sub(r'req\.ThreatModelID', 'req.ThreatModelId', content)
    content = re.sub(r'req\.ObjectID', 'req.ObjectId', content)

    # Fix payload length check
    content = re.sub(
        r'if len\(req\.Payload\) > maxPayloadSize',
        'if req.Payload != nil && len(payloadToString(req.Payload)) > maxPayloadSize',
        content
    )
    content = re.sub(
        r'len\(req\.Payload\)\)',
        'len(payloadToString(req.Payload)))',
        content
    )

    # Fix ObjectType validation
    content = re.sub(
        r'if req\.ObjectType != "" \{',
        'if req.ObjectType != nil && *req.ObjectType != "" {',
        content
    )
    content = re.sub(
        r'obj == req\.ObjectType',
        'obj == string(*req.ObjectType)',
        content
    )

    # Fix AddonInvocation struct creation
    content = re.sub(
        r'invocation := &AddonInvocation\{[\s\S]*?Payload:[\s\S]*?\}',
        '''invocation := &AddonInvocation{
\t\tID:            uuid.New(),
\t\tAddonID:       addonID,
\t\tThreatModelID: req.ThreatModelId,
\t\tObjectType:    toObjectTypeString(req.ObjectType),
\t\tObjectID:      req.ObjectId,
\t\tInvokedBy:     userID,
\t\tPayload:       payloadToString(req.Payload),
\t\tStatus:        "pending",
\t\tStatusPercent: 0,
\t\tCreatedAt:     time.Now(),
\t\tStatusUpdatedAt: time.Now(),
\t}''',
        content,
        count=1
    )

    # Fix InvokeAddonResponse
    content = re.sub(
        r'InvocationID: invocation\.ID,',
        'InvocationId: invocation.ID,',
        content
    )
    content = re.sub(
        r'Status:       invocation\.Status,',
        'Status:       statusToInvokeAddonResponseStatus(invocation.Status),',
        content
    )

    # Fix InvocationResponse - use converter
    content = re.sub(
        r'response := InvocationResponse\{[\s\S]*?StatusUpdatedAt:[\s\S]*?\}',
        'response := invocationToResponse(invocation)',
        content
    )

    # Fix list response conversion
    content = re.sub(
        r'responses := make\(\[\]InvocationResponse, len\(invocations\)\)\s+for i, inv := range invocations \{\s+responses\[i\] = InvocationResponse\(inv\)\s+\}',
        '''responses := make([]InvocationResponse, len(invocations))
\tfor i, inv := range invocations {
\t\tinv := inv // Create a copy for pointer
\t\tresponses[i] = invocationToResponse(&inv)
\t}''',
        content
    )

    # Fix status transition map
    content = re.sub(
        r'validTransitions\[req\.Status\]',
        'validTransitions[statusFromUpdateRequestStatus(req.Status)]',
        content
    )

    # Fix status percent validation
    content = re.sub(
        r'if req\.StatusPercent < 0 \|\| req\.StatusPercent > 100',
        'if req.StatusPercent != nil && (*req.StatusPercent < 0 || *req.StatusPercent > 100)',
        content
    )

    # Fix invocation updates
    content = re.sub(
        r'invocation\.Status = req\.Status',
        'invocation.Status = statusFromUpdateRequestStatus(req.Status)',
        content
    )
    content = re.sub(
        r'invocation\.StatusPercent = req\.StatusPercent',
        'invocation.StatusPercent = fromIntPtr(req.StatusPercent)',
        content
    )
    content = re.sub(
        r'invocation\.StatusMessage = req\.StatusMessage',
        'invocation.StatusMessage = fromStringPtr(req.StatusMessage)',
        content
    )

    # Fix UpdateInvocationStatusResponse
    content = re.sub(
        r'ID:              invocation\.ID,',
        'Id:              invocation.ID,',
        content
    )
    content = re.sub(
        r'Status:          invocation\.Status,',
        'Status:          statusToUpdateResponseStatus(invocation.Status),',
        content
    )

    with open(file_path, 'w') as f:
        f.write(content)

    print(f"✓ Fixed {file_path}")

def main():
    """Main entry point."""
    api_dir = Path(__file__).parent.parent / "api"

    # Fix addon_handlers.go
    addon_handlers = api_dir / "addon_handlers.go"
    if addon_handlers.exists():
        fix_addon_handlers(addon_handlers)
    else:
        print(f"Error: {addon_handlers} not found", file=sys.stderr)
        return 1

    # Fix addon_invocation_handlers.go
    invocation_handlers = api_dir / "addon_invocation_handlers.go"
    if invocation_handlers.exists():
        fix_invocation_handlers(invocation_handlers)
    else:
        print(f"Error: {invocation_handlers} not found", file=sys.stderr)
        return 1

    print("\n✓ Successfully fixed addon handler type mismatches")
    print("⚠ Manual review recommended - some fixes may need adjustment")
    return 0

if __name__ == "__main__":
    sys.exit(main())
