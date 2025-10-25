#!/usr/bin/env python3
# /// script
# dependencies = []
# ///
"""Add BulkUpdate handlers to all metadata handler files."""

import re
import os

# Define the handlers that need BulkUpdate methods
handlers_config = [
    {
        'file': 'api/document_metadata_handlers.go',
        'handler_struct': 'DocumentMetadataHandler',
        'entity_type': 'document',
        'entity_param': 'document_id',
        'parent_param': 'threat_model_id',
    },
    {
        'file': 'api/repository_metadata_handlers.go',
        'handler_struct': 'RepositoryMetadataHandler',
        'entity_type': 'repository',
        'entity_param': 'repository_id',
        'parent_param': 'threat_model_id',
    },
    {
        'file': 'api/threat_metadata_handlers.go',
        'handler_struct': 'ThreatMetadataHandler',
        'entity_type': 'threat',
        'entity_param': 'threat_id',
        'parent_param': 'threat_model_id',
    },
    {
        'file': 'api/threat_model_metadata_handlers.go',
        'handler_struct': 'ThreatModelMetadataHandler',
        'entity_type': 'threat_model',
        'entity_param': 'threat_model_id',
        'parent_param': None,  # No parent for threat models
    },
]

def generate_bulk_update_handler(config):
    """Generate BulkUpdate handler code for a given configuration."""
    entity_type = config['entity_type']
    entity_param = config['entity_param']
    parent_param = config['parent_param']
    handler_struct = config['handler_struct']

    # Capitalize first letter for display names
    entity_name = entity_type.replace('_', ' ').title().replace(' ', '')

    # Build function name
    if parent_param:
        func_name = f'BulkUpdate{entity_name}Metadata'
        route_prefix = f'/threat_models/{{{parent_param}}}'
    else:
        func_name = f'BulkUpdate{entity_name}Metadata'
        route_prefix = ''

    route = f'{route_prefix}/{entity_type}s/{{{entity_param}}}/metadata/bulk'

    # Build parameter extraction code
    param_extraction = []
    param_validation = []

    if parent_param:
        param_extraction.append(f'\t{parent_param.replace("_", "")} := c.Param("{parent_param}")')
        param_validation.append(f'''
\tif {parent_param.replace("_", "")} == "" {{
\t\tHandleRequestError(c, InvalidIDError("Missing {parent_param.replace('_', ' ')} ID"))
\t\treturn
\t}}
''')
        param_validation.append(f'''
\t// Validate {parent_param.replace('_', ' ')} ID format
\tif _, err := ParseUUID({parent_param.replace("_", "")}); err != nil {{
\t\tHandleRequestError(c, InvalidIDError("Invalid {parent_param.replace('_', ' ')} ID format, must be a valid UUID"))
\t\treturn
\t}}
''')

    param_extraction.append(f'\t{entity_param.replace("_", "")} := c.Param("{entity_param}")')
    param_validation.append(f'''
\tif {entity_param.replace("_", "")} == "" {{
\t\tHandleRequestError(c, InvalidIDError("Missing {entity_param.replace('_', ' ')} ID"))
\t\treturn
\t}}
''')
    param_validation.append(f'''
\t// Validate {entity_param.replace('_', ' ')} ID format
\tif _, err := ParseUUID({entity_param.replace("_", "")}); err != nil {{
\t\tHandleRequestError(c, InvalidIDError("Invalid {entity_param.replace('_', ' ')} ID format, must be a valid UUID"))
\t\treturn
\t}}
''')

    # Build log message
    entity_var = entity_param.replace("_", "")
    if parent_param:
        parent_var = parent_param.replace("_", "")
        log_msg = f'Bulk updating %d metadata entries for {entity_type} %s in {parent_param.replace("_", " ")} %s (user: %s)'
        log_args = f'len(metadataList), {entity_var}, {parent_var}, userEmail'
    else:
        log_msg = f'Bulk updating %d metadata entries for {entity_type} %s (user: %s)'
        log_args = f'len(metadataList), {entity_var}, userEmail'

    code = f'''
// {func_name} updates multiple metadata entries in a single request
// PUT {route}
func (h *{handler_struct}) {func_name}(c *gin.Context) {{
\tlogger := slogging.GetContextLogger(c)
\tlogger.Debug("{func_name} - updating multiple metadata entries")

\t// Extract parameters from URL
{"".join(param_extraction)}

{"".join(param_validation)}
\t// Get authenticated user
\tuserEmail, _, err := ValidateAuthenticatedUser(c)
\tif err != nil {{
\t\tHandleRequestError(c, err)
\t\treturn
\t}}

\t// Parse and validate request body using OpenAPI validation
\tvar metadataList []Metadata
\tif err := c.ShouldBindJSON(&metadataList); err != nil {{
\t\tHandleRequestError(c, InvalidInputError("Invalid request body: "+err.Error()))
\t\treturn
\t}}

\t// Validate bulk metadata
\tif len(metadataList) == 0 {{
\t\tHandleRequestError(c, InvalidInputError("No metadata entries provided"))
\t\treturn
\t}}

\tif len(metadataList) > 20 {{
\t\tHandleRequestError(c, InvalidInputError("Maximum 20 metadata entries allowed per bulk operation"))
\t\treturn
\t}}

\t// Check for duplicate keys within the request
\tkeyMap := make(map[string]bool)
\tfor _, metadata := range metadataList {{
\t\tif keyMap[metadata.Key] {{
\t\t\tHandleRequestError(c, InvalidInputError("Duplicate metadata key found: "+metadata.Key))
\t\t\treturn
\t\t}}
\t\tkeyMap[metadata.Key] = true
\t}}

\tlogger.Debug("{log_msg}",
\t\t{log_args})

\t// Update metadata entries in store
\tif err := h.metadataStore.BulkUpdate(c.Request.Context(), "{entity_type}", {entity_var}, metadataList); err != nil {{
\t\tlogger.Error("Failed to bulk update {entity_type} metadata for %s: %v", {entity_var}, err)
\t\tHandleRequestError(c, ServerError("Failed to update metadata entries"))
\t\treturn
\t}}

\t// Retrieve the updated metadata to return with timestamps
\tupdatedMetadata, err := h.metadataStore.List(c.Request.Context(), "{entity_type}", {entity_var})
\tif err != nil {{
\t\t// Log error but still return success since update succeeded
\t\tlogger.Error("Failed to retrieve updated metadata: %v", err)
\t\tc.JSON(http.StatusOK, metadataList)
\t\treturn
\t}}

\tlogger.Debug("Successfully bulk updated %d metadata entries for {entity_type} %s", len(metadataList), {entity_var})
\tc.JSON(http.StatusOK, updatedMetadata)
}}
'''
    return code

def add_handler_to_file(filepath, handler_code):
    """Add handler code to the end of a file."""
    with open(filepath, 'a') as f:
        f.write(handler_code)
    print(f"Added BulkUpdate handler to {filepath}")

def main():
    """Main function to add BulkUpdate handlers to all metadata handler files."""
    os.chdir('/Users/efitz/Projects/tmi')

    for config in handlers_config:
        filepath = config['file']
        if not os.path.exists(filepath):
            print(f"Warning: File {filepath} not found, skipping...")
            continue

        # Check if BulkUpdate already exists in the file
        with open(filepath, 'r') as f:
            content = f.read()
            if 'BulkUpdate' in content and config['handler_struct'] in content:
                print(f"BulkUpdate handler already exists in {filepath}, skipping...")
                continue

        # Generate and add the handler code
        handler_code = generate_bulk_update_handler(config)
        add_handler_to_file(filepath, handler_code)

if __name__ == '__main__':
    main()
