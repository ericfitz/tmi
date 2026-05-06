package api

// contentProviderMeta is the static metadata for a known content source name:
// its kind (delegated/service/direct), and default user-facing display name
// and icon when the operator has not supplied overrides.
type contentProviderMeta struct {
	Kind        string
	DefaultName string
	DefaultIcon string
}

// contentProviderMetaTable maps ContentSource.Name() -> static metadata.
// Unknown ids fall through to a "direct" default in lookupContentProviderMeta.
var contentProviderMetaTable = map[string]contentProviderMeta{
	"http":             {Kind: "direct", DefaultName: "HTTP", DefaultIcon: "fa-solid fa-globe"},
	"google_drive":     {Kind: "service", DefaultName: "Google Drive", DefaultIcon: "fa-brands fa-google-drive"},
	"google_workspace": {Kind: "delegated", DefaultName: "Google Workspace", DefaultIcon: "fa-brands fa-google"},
	"microsoft":        {Kind: "delegated", DefaultName: "Microsoft 365", DefaultIcon: "fa-brands fa-microsoft"},
	"confluence":       {Kind: "delegated", DefaultName: "Atlassian Confluence", DefaultIcon: "fa-brands fa-confluence"},
}

// lookupContentProviderMeta returns the metadata for the given source id.
// Unknown ids are treated as "direct" with the id itself as the default name
// and an empty icon — a safe fallback that future-registered sources can override
// by adding a row to contentProviderMetaTable.
func lookupContentProviderMeta(id string) contentProviderMeta {
	if m, ok := contentProviderMetaTable[id]; ok {
		return m
	}
	return contentProviderMeta{Kind: "direct", DefaultName: id, DefaultIcon: ""}
}
