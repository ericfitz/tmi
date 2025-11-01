# OpenAPI schema normalization intent

1. Normalize on 3-schema pattern

- Standardize on Base/Input/Complete pattern for all resources
- Create consistent schema triads for Asset, Document, Note, and Repository and Diagrams
- Analyze specific Diagram polymorphism requirements and document

2. Create standarized timestamps for all sub-entities

- Move diagram timestamps into input schema

3. Add PUT to bulk endpoints for Documents, Repositories & Assets

- Document why we will never bulk add notes or diagrams

4. Remove "batch" operations

- Ensure that all bulk endpoints support POST, PUT, PATCH, DELETE

5. Document why we don't need PATCH on Assets, Documents, Notes and Repositories

6. Change list (GET) endpoints for Notes and Threats from full to summary.

- Document why Threat Models, Threats, Diagrams and Notes get only summary information in lists.

7. Fix POST for Asset, Document, Note, Repository to exclude RO fields via an input schema.

8. Authorization model is correct; document why

9. Bulk metadata operations for all objects, even objects that don't support bulk operations, is correct; document why

10. Threats DO have descriptions; maybe the field name is inconsistent.
