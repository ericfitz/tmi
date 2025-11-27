# WORKFLOW GENERATION PROMPT

You are an experienced software developer specializing in implementing back-end API servers, and you are passionate about reliability, which you have learned from experience requires thorough testing.

You have been assigned the task of analyzing the TMI OpenAPI 3.0 specification JSON file docs/reference/apis/tmi-openapi.json and generating a json file that will be used to drive automated testing.

The openapi spec defines hierarchical objects, such as "diagram" objects as children of "threat_model" objects. Most APIs require OAuth authentication/authorization (some are public and don't require authentication or authorization). We support the "authorization code with PKCE" OAuth flow.

The problem with automated testing of complex APIs is that many API calls depend on state established with earlier API calls. For example, you must authenticate before you can create objects. You must create an object before you can create child objects of the original object. You can't delete an object that has not yet been created, and so forth.

The TMI API has many endpoints and is moderately complex. In order to test it thoroughly, you are going to need to build and/or use API test automation. And your task is to generate the documentation needed to build that automation. The OpenAPI spec defines the endpoints and schemas, but doesn't explicitly describe the order of operations for API calls that have other API calls as prerequisites, and that's what you're going to create.

Please create a structured JSON "workflows" file that outlines the required API call sequences for testing every supported HTTP method (GET, POST, PUT, PATCH, DELETE, etc.) on every object and every property.

For each object (e.g., "threat_model", "diagram"):

- List dependencies: Prerequisite objects/actions (e.g., to operate on diagram, one must first create a threat model, since diagrams are child objects of threat model objects).

For each method:

- List the sequence, in order of prerequisite calls, starting from OAuth flow
- Example: To modify the name property of a diagram object, one must perform: OAuth authentication → PUT /threat_model → PUT /threat_model/{threat_model_id}/diagram → PATCH /threat_model/{threat_model_id}/diagram/{diagram_id}/name.

Cover all properties: For PATCH/PUT, include sequences per property if dependencies differ.

Handle hierarchies: Ensure child operations require parent creation.

Output format: JSON with top-level keys as object names, sub-keys as methods, values as arrays of ordered steps (each step: { "action": "METHOD /path", "description": "brief purpose", "prereqs": ["prev-step-ids"] }).

You can see an example output file at docs/reference/apis/api-workflows.json

Ensure completeness for all paths, parameters, and auth in the spec.

Write the json file to the project root directory.
