#!/usr/bin/env python3

# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "pyyaml>=6.0",
# ]
# ///

"""
Enhance Arazzo with TMI api-workflows.json data.

This script:
1. Loads base scaffold from Redocly CLI
2. Reads TMI workflow patterns from api-workflows.json
3. Adds OAuth PKCE sequences with proper RFC 7636 parameters
4. Maps TMI prerequisites to Arazzo dependsOn
5. Adds complete workflow sequences
6. Enriches with TMI-specific success criteria
7. Outputs both YAML and JSON formats
"""

import json
import yaml  # pyright: ignore[reportMissingModuleSource]  # ty:ignore[unresolved-import]
from pathlib import Path
from typing import Dict, List, Any


class ArazzoEnhancer:
    """Enhance Arazzo specifications with TMI workflow knowledge."""

    def __init__(self, api_workflows_path: str):
        """Load TMI workflow patterns."""
        print(f"📖 Loading TMI workflow patterns from {api_workflows_path}")
        with open(api_workflows_path) as f:
            self.workflows = json.load(f)

        # Prerequisite mapping from TMI to Arazzo step IDs
        self.prereq_map = {
            "oauth_complete": "oauth_token_exchange",
            "threat_model_create": "create_threat_model",
            "threat_create": "create_threat",
            "diagram_create": "create_diagram",
            "document_create": "create_document",
            "asset_create": "create_asset",
            "note_create": "create_note",
            "repository_create": "create_repository",
            "threat_metadata_set_key": "set_threat_metadata_key",
            "document_metadata_set_key": "set_document_metadata_key",
            "repository_metadata_set_key": "set_repository_metadata_key",
            "asset_metadata_set_key": "set_asset_metadata_key",
            "note_metadata_set_key": "set_note_metadata_key",
            "diagram_metadata_set_key": "set_diagram_metadata_key",
            "threat_model_metadata_set_key": "set_threat_model_metadata_key",
            "diagram_collaboration_start_session": "start_collaboration_session",
        }

    def enhance_scaffold(self, scaffold_path: str, output_yaml: str, output_json: str):
        """Main enhancement pipeline."""
        print(f"\n🔧 Loading base scaffold: {scaffold_path}")

        # Check if scaffold exists
        if not Path(scaffold_path).exists():
            print(f"⚠️  Scaffold not found at {scaffold_path}")
            print("   Creating minimal Arazzo structure...")
            arazzo = self._create_minimal_arazzo()
        else:
            with open(scaffold_path) as f:
                arazzo = yaml.safe_load(f)

        # Enhancement pipeline
        print("\n🔄 Enhancement Pipeline:")
        print("   1️⃣  Sanitizing scaffold IDs and adding descriptions...")
        self._sanitize_scaffold(arazzo)

        print("   2️⃣  Adding OAuth PKCE workflow...")
        oauth_workflow = self._create_oauth_pkce_workflow()
        if "workflows" not in arazzo:
            arazzo["workflows"] = []
        arazzo["workflows"].insert(0, oauth_workflow)

        print("   3️⃣  Adding complete workflow sequences...")
        complete_workflows = self._add_complete_sequences()
        arazzo["workflows"].extend(complete_workflows)

        print("   4️⃣  Enriching success criteria...")
        self._add_success_criteria(arazzo)

        print("   5️⃣  Adding workflow outputs...")
        self._add_workflow_outputs(arazzo)

        # Write outputs
        print("\n💾 Writing enhanced specifications:")
        print(f"   YAML: {output_yaml}")
        with open(output_yaml, "w") as f:
            yaml.dump(
                arazzo, f, sort_keys=False, default_flow_style=False, allow_unicode=True
            )

        print(f"   JSON: {output_json}")
        with open(output_json, "w") as f:
            json.dump(arazzo, f, indent=2)

        # Summary
        workflow_count = len(arazzo.get("workflows", []))
        total_steps = sum(len(w.get("steps", [])) for w in arazzo.get("workflows", []))
        print("\n✅ Enhancement complete!")
        print(f"   Workflows: {workflow_count}")
        print(f"   Total steps: {total_steps}")

    def _create_minimal_arazzo(self) -> Dict:
        """Create minimal Arazzo structure when no scaffold exists."""
        return {
            "arazzo": "1.0.0",
            "info": {
                "title": "TMI API Workflows",
                "version": "1.0.0",
                "description": "Executable API workflows for Threat Modeling Interface (TMI)",
            },
            "sourceDescriptions": [
                {
                    "name": "tmi-api",
                    "type": "openapi",
                    "url": "./tmi-openapi.json",
                }
            ],
            "workflows": [],
        }

    def _sanitize_scaffold(self, arazzo: Dict):
        """Sanitize scaffold IDs and add missing descriptions."""
        # Add info description if missing
        if "info" in arazzo:
            if "description" not in arazzo["info"] or not arazzo["info"]["description"]:
                arazzo["info"]["description"] = (
                    "Executable API workflows for Threat Modeling Interface (TMI)"
                )
            if "summary" not in arazzo["info"]:
                arazzo["info"]["summary"] = "TMI API Workflow Specifications"

        # Sanitize workflows
        for workflow in arazzo.get("workflows", []):
            # Sanitize workflowId
            if "workflowId" in workflow:
                workflow["workflowId"] = self._sanitize_id(workflow["workflowId"])

            # Add missing description
            if "description" not in workflow or not workflow["description"]:
                workflow_id = workflow.get("workflowId", "workflow")
                workflow["description"] = (
                    f"Workflow for {workflow_id.replace('_', ' ').replace('-', ' ')}"
                )

            # Add missing summary if not present
            if "summary" not in workflow or not workflow["summary"]:
                workflow_id = workflow.get("workflowId", "workflow")
                workflow["summary"] = (
                    workflow_id.replace("_", " ").replace("-", " ").title()
                )

            # Sanitize steps
            for step in workflow.get("steps", []):
                # Sanitize stepId
                if "stepId" in step:
                    step["stepId"] = self._sanitize_id(step["stepId"])

                # Add missing description
                if "description" not in step or not step["description"]:
                    step_id = step.get("stepId", "step")
                    operation_id = step.get("operationId", "")
                    if operation_id:
                        step["description"] = f"Execute {operation_id} operation"
                    else:
                        step["description"] = (
                            f"Execute {step_id.replace('_', ' ').replace('-', ' ')}"
                        )

    def _create_oauth_pkce_workflow(self) -> Dict:
        """
        Create OAuth 2.0 PKCE workflow from base_oauth_sequence.

        Implements RFC 7636 PKCE flow with:
        - code_verifier generation
        - S256 code_challenge computation
        - Proper parameter flow through steps
        """
        oauth = self.workflows["base_oauth_sequence"]
        notes = self.workflows["notes"]

        return {
            "workflowId": "oauth_pkce_authentication",
            "summary": "OAuth 2.0 Authorization Code Flow with PKCE",
            "description": notes["oauth_flow"],
            "inputs": {
                "type": "object",
                "properties": {
                    "idp": {
                        "type": "string",
                        "default": "tmi",
                        "description": "OAuth provider (tmi, google, github)",
                    },
                    "login_hint": {
                        "type": "string",
                        "default": "alice",
                        "description": "User identity hint for TMI provider (3-20 alphanumeric + hyphens)",
                    },
                    "client_callback": {
                        "type": "string",
                        "default": "http://localhost:8079/",
                        "description": "Client callback URL for OAuth redirect",
                    },
                    "scope": {
                        "type": "string",
                        "default": "openid profile email",
                        "description": "OAuth scopes to request",
                    },
                },
            },
            "steps": [
                {
                    "stepId": "oauth_authorize",
                    "operationId": "authorizeOAuthProvider",
                    "description": oauth["oauth_init"]["description"],
                    "parameters": [
                        {"name": "idp", "in": "query", "value": "$inputs.idp"},
                        {
                            "name": "login_hint",
                            "in": "query",
                            "value": "$inputs.login_hint",
                        },
                        {
                            "name": "client_callback",
                            "in": "query",
                            "value": "$inputs.client_callback",
                        },
                        {"name": "state", "in": "query", "value": "{$randomString}"},
                        {
                            "name": "code_challenge",
                            "in": "query",
                            "value": "{$base64url(sha256($components.code_verifier))}",
                        },
                        {
                            "name": "code_challenge_method",
                            "in": "query",
                            "value": "S256",
                        },
                        {"name": "scope", "in": "query", "value": "$inputs.scope"},
                    ],
                    "successCriteria": [
                        {"condition": "$statusCode == 302"},
                        {"condition": '$response.headers.Location contains "code="'},
                    ],
                    "outputs": {
                        "authorization_url": "$response.headers.Location",
                    },
                },
                {
                    "stepId": "oauth_callback",
                    "operationId": "handleOAuthCallback",
                    "description": oauth["oauth_callback"]["description"],
                    "successCriteria": [
                        {"condition": "$statusCode == 200"},
                    ],
                    "outputs": {
                        "auth_code": "$response.query.code",
                        "returned_state": "$response.query.state",
                    },
                },
                {
                    "stepId": "oauth_token_exchange",
                    "operationId": "exchangeOAuthCode",
                    "description": oauth["oauth_token_exchange"]["description"],
                    "requestBody": {
                        "contentType": "application/x-www-form-urlencoded",
                        "payload": {
                            "grant_type": "authorization_code",
                            "code": "$steps.oauth_callback.outputs.auth_code",
                            "code_verifier": "{$components.code_verifier}",
                        },
                    },
                    "successCriteria": [
                        {"condition": "$statusCode == 200"},
                        {"condition": "$response.body.access_token != null"},
                        {"condition": '$response.body.token_type == "Bearer"'},
                    ],
                    "outputs": {
                        "access_token": "$response.body.access_token",
                        "refresh_token": "$response.body.refresh_token",
                        "token_type": "$response.body.token_type",
                        "expires_in": "$response.body.expires_in",
                    },
                },
            ],
            "outputs": {
                "access_token": "$steps.oauth_token_exchange.outputs.access_token",
                "refresh_token": "$steps.oauth_token_exchange.outputs.refresh_token",
            },
        }

    def _add_complete_sequences(self) -> List[Dict]:
        """
        Add complete workflow sequences from api-workflows.json.

        Includes all 7 sequences:
        - threat_model_full_crud
        - asset_full_crud
        - diagram_collaboration_workflow
        - metadata_operations
        - bulk_operations
        - webhook_workflow
        - addon_workflow
        """
        workflows: List[Dict[str, Any]] = []
        sequences = self.workflows.get("complete_workflow_sequences", {})

        for seq_name, steps_data in sequences.items():
            workflow: Dict[str, Any] = {
                "workflowId": self._sanitize_id(seq_name),
                "summary": f"{seq_name.replace('_', ' ').title()}",
                "description": f"Complete end-to-end workflow for {seq_name.replace('_', ' ')}",
                "steps": [],
            }

            # Create shortened workflow prefix for unique stepIds
            workflow_prefix = seq_name[:20]  # Keep reasonable length

            # Track stepIds in this workflow to avoid duplicates
            workflow_step_ids = set()

            for step_data in steps_data:
                step_num = step_data["step"]
                base_step_id = self._generate_step_id(step_data["action"], step_num)
                # Make stepId unique by prefixing with workflow name
                step_id = self._sanitize_id(f"{workflow_prefix}_{base_step_id}")

                # If duplicate within workflow, add step number
                if step_id in workflow_step_ids:
                    step_id = self._sanitize_id(
                        f"{workflow_prefix}_{base_step_id}_{step_num}"
                    )

                workflow_step_ids.add(step_id)

                # Parse action into method and path
                parts = step_data["action"].split(" ", 1)
                method = parts[0] if len(parts) > 1 else "GET"
                path = parts[1] if len(parts) > 1 else parts[0]

                arazzo_step = {
                    "stepId": step_id,
                    "description": step_data["description"],
                    "operationPath": step_data["action"],
                }

                # Add dependencies (referencing steps within the same workflow)
                dependencies = []
                if step_data.get("auth_required") and step_num > 3:
                    # Steps 1-3 are OAuth, step 4+ need auth token
                    # Reference the OAuth step in THIS workflow
                    oauth_step_id = self._sanitize_id(
                        f"{workflow_prefix}_tmi_create_token"
                    )
                    dependencies.append(oauth_step_id)

                # Add path parameter dependencies (with workflow prefix)
                if "{threat_model_id}" in path and step_num > 4:
                    dependencies.append(
                        self._sanitize_id(f"{workflow_prefix}_tmi_create_threat_model")
                    )
                if "{threat_id}" in path:
                    dependencies.append(
                        self._sanitize_id(f"{workflow_prefix}_tmi_create_threat")
                    )
                if "{diagram_id}" in path:
                    dependencies.append(
                        self._sanitize_id(f"{workflow_prefix}_tmi_create_diagram")
                    )
                if "{document_id}" in path:
                    dependencies.append(
                        self._sanitize_id(f"{workflow_prefix}_tmi_create_document")
                    )
                if "{asset_id}" in path:
                    dependencies.append(
                        self._sanitize_id(f"{workflow_prefix}_tmi_create_asset")
                    )
                if "{note_id}" in path:
                    dependencies.append(
                        self._sanitize_id(f"{workflow_prefix}_tmi_create_note")
                    )
                if "{repository_id}" in path:
                    dependencies.append(
                        self._sanitize_id(f"{workflow_prefix}_tmi_create_repository")
                    )

                if dependencies:
                    arazzo_step["dependsOn"] = list(set(dependencies))

                # When using operationPath, don't add inline parameters or requestBody
                # The Arazzo runtime should get those from the OpenAPI spec
                # Only add success criteria and outputs

                # Add success criteria
                arazzo_step["successCriteria"] = self._get_success_criteria(method)

                # Add outputs for resource creation
                if method == "POST" and "{" not in path:
                    resource_type = self._extract_resource_type(path)
                    arazzo_step["outputs"] = {
                        f"{resource_type}_id": "$response.body.id",
                    }

                workflow["steps"].append(arazzo_step)

            workflows.append(workflow)

        return workflows

    def _sanitize_id(self, id_string: str) -> str:
        """Sanitize ID to match Arazzo pattern [A-Za-z0-9_-]."""
        import re

        # Replace invalid characters with underscores
        sanitized = re.sub(r"[^A-Za-z0-9_-]", "_", id_string)
        # Remove leading/trailing underscores
        sanitized = sanitized.strip("_")
        # Collapse multiple underscores
        sanitized = re.sub(r"_+", "_", sanitized)
        return sanitized

    def _generate_step_id(self, action: str, step_num: int) -> str:
        """Generate a step ID from an action string with tmi_ prefix to avoid conflicts."""
        # Parse "GET /path" into method + resource
        parts = action.split(" ")
        method = parts[0].lower() if len(parts) > 1 else "get"
        path = parts[1] if len(parts) > 1 else parts[0]

        # Extract resource from path
        path_parts = [p for p in path.split("/") if p and not p.startswith("{")]

        # Handle bulk operations
        if "bulk" in path:
            resource = path_parts[-2] if len(path_parts) > 1 else "resource"
            step_id = f"tmi_{method}_{resource}_bulk"
            return self._sanitize_id(step_id)

        # Handle metadata operations
        if "metadata" in path:
            if len(path_parts) >= 2:
                resource = path_parts[-3] if len(path_parts) > 2 else path_parts[-2]
                if "{key}" in path:
                    step_id = f"tmi_{method}_{resource}_metadata_key"
                else:
                    step_id = f"tmi_{method}_{resource}_metadata"
                return self._sanitize_id(step_id)

        # Handle collaboration
        if "collaborate" in path:
            step_id = f"tmi_{method}_collaboration_session"
            return self._sanitize_id(step_id)

        # Standard resource operations
        if path_parts:
            resource = path_parts[-1].rstrip("s")  # Remove plural 's'

            # Map HTTP methods to CRUD operations
            operation_map = {
                "post": "create",
                "get": "get" if "{" in path else "list",
                "put": "update",
                "patch": "patch",
                "delete": "delete",
            }

            operation = operation_map.get(method, method)
            step_id = f"tmi_{operation}_{resource}"
            return self._sanitize_id(step_id)

        return self._sanitize_id(f"tmi_step_{step_num}")

    def _extract_resource_type(self, path: str) -> str:
        """Extract resource type from path for output naming."""
        path_parts = [p for p in path.split("/") if p and not p.startswith("{")]
        if path_parts:
            resource = path_parts[-1]
            # Return singular form
            if resource.endswith("ies"):
                return resource[:-3] + "y"  # repositories -> repository
            elif resource.endswith("s"):
                return resource[:-1]  # threats -> threat
        return "resource"

    def _get_success_criteria(self, method: str) -> List[Dict]:
        """Generate HTTP-appropriate success criteria based on method."""
        criteria_map = {
            "GET": [{"condition": "$statusCode == 200"}],
            "POST": [
                {"condition": "$statusCode == 201 || $statusCode == 200"},
            ],
            "PUT": [{"condition": "$statusCode == 200"}],
            "PATCH": [{"condition": "$statusCode == 200"}],
            "DELETE": [{"condition": "$statusCode == 204 || $statusCode == 200"}],
        }
        return criteria_map.get(
            method.upper(), [{"condition": "$statusCode >= 200 && $statusCode < 300"}]
        )

    def _generate_sample_payload(self, path: str, method: str) -> Dict | List:
        """Generate sample request payload based on endpoint."""
        # Threat models
        if "threat_models" in path and method == "POST":
            return {
                "name": "Sample Threat Model",
                "description": "Generated for Arazzo workflow testing",
                "authorization": {
                    "owners": ["$user"],
                    "writers": [],
                    "readers": [],
                },
            }

        # Threats
        if "/threats" in path and method == "POST":
            if "bulk" in path:
                return [
                    {
                        "title": "Sample Threat 1",
                        "description": "Test threat for bulk operation",
                        "severity": "high",
                    }
                ]
            return {
                "title": "Sample Threat",
                "description": "Test threat",
                "severity": "high",
                "stride_category": "tampering",
            }

        # Diagrams
        if "/diagrams" in path and method == "POST":
            return {
                "name": "Sample Diagram",
                "description": "Test diagram",
                "diagram_type": "data_flow",
            }

        # Assets
        if "/assets" in path and method == "POST":
            if "bulk" in path:
                return [
                    {
                        "name": "Sample Asset 1",
                        "asset_type": "data_store",
                    }
                ]
            return {
                "name": "Sample Asset",
                "asset_type": "web_application",
            }

        # Documents
        if "/documents" in path and method == "POST":
            if "bulk" in path:
                return [
                    {
                        "name": "Sample Document 1",
                        "uri": "https://example.com/doc1",
                    }
                ]
            return {
                "name": "Sample Document",
                "uri": "https://example.com/document",
                "description": "Test document",
            }

        # Notes
        if "/notes" in path and method == "POST":
            return {
                "name": "Sample Note",
                "content": "Test note content",
            }

        # Repositories
        if "/repositories" in path and method == "POST":
            if "bulk" in path:
                return [
                    {
                        "name": "Sample Repository 1",
                        "uri": "https://github.com/example/repo1",
                    }
                ]
            return {
                "name": "Sample Repository",
                "uri": "https://github.com/example/repo",
                "description": "Test repository",
            }

        # Metadata
        if "/metadata" in path:
            if "{key}" in path and method == "PUT":
                return {"value": "sample_value"}
            elif method == "POST":
                return {"key": "sample_key", "value": "sample_value"}
            elif "bulk" in path:
                return [
                    {"key": "key1", "value": "value1"},
                    {"key": "key2", "value": "value2"},
                ]

        # Webhooks
        if "/webhooks/subscriptions" in path and method == "POST":
            return {
                "url": "https://example.com/webhook",
                "events": ["threat_model.created", "threat.created"],
                "active": True,
            }

        # Addons
        if "/addons" in path and method == "POST":
            if "/invoke" in path:
                return {
                    "parameters": {"key": "value"},
                }
            return {
                "name": "Sample Addon",
                "callback_url": "https://example.com/addon",
                "description": "Test addon",
            }

        # Collaboration
        if "/collaborate" in path and method == "POST":
            return {
                "participants": ["bob@tmi.local", "charlie@tmi.local"],
                "permissions": {
                    "can_edit": True,
                    "can_comment": True,
                },
            }

        # Generic fallback
        return {}

    def _add_success_criteria(self, arazzo: Dict):
        """Add HTTP-appropriate success criteria to steps that lack them."""
        for workflow in arazzo.get("workflows", []):
            for step in workflow.get("steps", []):
                if "successCriteria" not in step:
                    # Infer from operationPath if present
                    op_path = step.get("operationPath", "")
                    method = op_path.split(" ")[0] if " " in op_path else "GET"
                    step["successCriteria"] = self._get_success_criteria(method)

    def _add_workflow_outputs(self, arazzo: Dict):
        """Add workflow-level outputs for key workflows."""
        for workflow in arazzo.get("workflows", []):
            workflow_id = workflow.get("workflowId", "")

            # OAuth PKCE workflow outputs (exact match only)
            if workflow_id == "oauth_pkce_authentication":
                if "outputs" not in workflow:
                    workflow["outputs"] = {}
                workflow["outputs"]["access_token"] = (
                    "$steps.oauth_token_exchange.outputs.access_token"
                )
                workflow["outputs"]["refresh_token"] = (
                    "$steps.oauth_token_exchange.outputs.refresh_token"
                )

            # Resource creation workflows
            elif "crud" in workflow_id.lower() or "full" in workflow_id.lower():
                if "outputs" not in workflow:
                    workflow["outputs"] = {}

                # Find creation step
                for step in workflow.get("steps", []):
                    step_id = step.get("stepId", "")
                    if "create" in step_id and "outputs" in step:
                        for key in step["outputs"]:
                            workflow["outputs"][key] = (
                                "$steps." + step_id + ".outputs." + key
                            )


if __name__ == "__main__":
    import sys

    # Default paths
    api_workflows = "docs/reference/apis/api-workflows.json"
    scaffold = "docs/reference/apis/arazzo/scaffolds/base-scaffold.arazzo.yaml"
    output_yaml = "docs/reference/apis/tmi.arazzo.yaml"
    output_json = "docs/reference/apis/tmi.arazzo.json"

    # Allow override from command line
    if len(sys.argv) > 1:
        api_workflows = sys.argv[1]
    if len(sys.argv) > 2:
        scaffold = sys.argv[2]
    if len(sys.argv) > 3:
        output_yaml = sys.argv[3]
    if len(sys.argv) > 4:
        output_json = sys.argv[4]

    print("=" * 80)
    print("TMI Arazzo Enhancement Tool")
    print("=" * 80)

    enhancer = ArazzoEnhancer(api_workflows)
    enhancer.enhance_scaffold(scaffold, output_yaml, output_json)

    print("\n" + "=" * 80)
