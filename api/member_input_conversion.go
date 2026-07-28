package api

// Conversions from the client-writable member shapes to the resolved ones.
//
// TeamMember and ResponsibleParty carry a `user` object that the server
// populates on read and that a client must never send. It was marked
// `readOnly: true` on a schema shared by requests and responses, which is the
// correct OpenAPI keyword but is ignored by at least one client (CATS emits it,
// and TMI's validation middleware then rejects the whole request — #604). The
// schemas are now split: requests take TeamMemberInput / ResponsiblePartyInput,
// which simply do not have the field, and responses keep the resolved variant.
//
// These converters are the seam between the two. They exist rather than the
// handlers assigning across the types because the request type genuinely cannot
// supply `user` — leaving it nil is the whole point, and doing so explicitly is
// clearer than a struct literal that silently drops a field.

// SEM@b4c5d6e7f8091a2b3c4d5e6f708192930415263: convert client-supplied team members to the resolved shape, leaving user unset (pure)
func teamMembersFromInput(in *[]TeamMemberInput) *[]TeamMember {
	if in == nil {
		return nil
	}
	out := make([]TeamMember, 0, len(*in))
	for _, m := range *in {
		out = append(out, TeamMember{
			UserId:     m.UserId,
			Role:       m.Role,
			CustomRole: m.CustomRole,
			// User is deliberately unset: it is resolved on read.
		})
	}
	return &out
}

// SEM@b4c5d6e7f8091a2b3c4d5e6f708192930415263: convert client-supplied responsible parties to the resolved shape, leaving user unset (pure)
func responsiblePartiesFromInput(in *[]ResponsiblePartyInput) *[]ResponsibleParty {
	if in == nil {
		return nil
	}
	out := make([]ResponsibleParty, 0, len(*in))
	for _, p := range *in {
		out = append(out, ResponsibleParty{
			UserId:     p.UserId,
			Role:       p.Role,
			CustomRole: p.CustomRole,
			// User is deliberately unset: it is resolved on read.
		})
	}
	return &out
}
