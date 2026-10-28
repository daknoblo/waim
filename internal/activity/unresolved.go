package activity

// UnresolvedTitle uses source-local identity, not just the title, so equal
// names from distinct sources are not collapsed into one skipped event.
func (r *Run) UnresolvedTitle(identity, sourceName, title string) {
	r.Report(Diagnostic{Key: "unresolved:" + identity, Reason: Unresolved, Severity: Skip, Phase: Identity, Subject: sourceName, Current: title})
}
