# One-off transform for issue #956: flatten the diagram cell and diagram
# schemas so generators stop producing shadowed fields and self-referential
# discriminators. Task 4 of the plan deletes this script.
#
# Usage: jq -f scripts/flatten-diagram-schemas.jq api-schema/tmi-openapi.json

def uniq_ordered: reduce .[] as $x ([]; if index([$x]) then . else . + [$x] end);

def flat_cell($name):
  .components.schemas[$name] as $w
  | $w.allOf[1] as $child
  | .components.schemas.Cell as $cell
  | ({
      type: "object",
      description: ($w.description // $child.description),
      required: (($cell.required + ($child.required // [])) | uniq_ordered),
      properties: (
        {id: {"$ref": "#/components/schemas/CellId"},
         data: {"$ref": "#/components/schemas/CellData"}}
        + $child.properties
        + {shape: ($child.properties.shape
                   + {pattern: $cell.properties.shape.pattern, maxLength: 64})}
      )
    }
    + (if $w.example then {example: $w.example} else {} end)) as $flat
  | .components.schemas[$name] = $flat;

def flat_diagram($name; $base):
  .components.schemas[$name] as $w
  | .components.schemas[$base] as $b
  | $w.allOf[1] as $child
  | ({
      type: "object",
      description: $w.description,
      required: (($b.required + ($child.required // [])) | uniq_ordered),
      properties: (
        $b.properties + $child.properties
        + (if $w.properties.version then {version: $w.properties.version} else {} end)
        + {type: ($b.properties.type
                  + {description: $child.properties.type.description, maxLength: 64})}
      )
    }
    + (if $w.example then {example: $w.example} else {} end)) as $flat
  | .components.schemas[$name] = $flat;

.components.schemas.CellId = .components.schemas.Cell.properties.id
| .components.schemas.CellData = .components.schemas.Cell.properties.data
| flat_cell("Node")
| flat_cell("Edge")
| flat_diagram("DfdDiagram"; "BaseDiagram")
| flat_diagram("DfdDiagramInput"; "BaseDiagramInput")
| .components.schemas.ThreatModel.allOf[1].properties.diagrams.items = {"$ref": "#/components/schemas/DfdDiagram"}
| .components.schemas.DiagramListItem.properties.type.maxLength = 64
| del(.components.schemas.Cell, .components.schemas.BaseDiagram,
      .components.schemas.BaseDiagramInput, .components.schemas.Diagram)
| walk(if . == "new-cell-id" then "8a3f0c2e-5b7d-4e1a-9c6f-2d4b8e0a1f37"
       elif type == "string" then gsub("BaseDiagram\\.update_vector"; "the diagram update_vector")
       else . end)
| (([.. | objects | .["$ref"]? // empty
     | select(type == "string" and test("/(Cell|BaseDiagram|BaseDiagramInput|Diagram)$"))])
   as $dangling
   | if ($dangling | length) > 0 then error("dangling refs: \($dangling)") else . end)
