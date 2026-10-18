{{define "intent" -}}
{{if .SpecID -}}
Spec this Checkpoint implements: {{.SpecID}}  (read it with: bd show {{.SpecID}})
{{- end}}
{{- if and .SpecID .Surgeries}}
{{end}}
{{- if .Surgeries -}}
Surgeries on this Checkpoint ({{if .SpecID}}context for work touching their commits{{else}}the human's intent, your context in place of a Spec{{end}}; read each with bd show <id>):
{{- range .Surgeries}}
- {{.ID}}: {{.Title}}
{{- end}}
{{- end}}
{{- if not (or .SpecID .Surgeries) -}}
This Checkpoint is headless: there is no Spec.
{{- end}}
{{- end}}

{{define "no-spec-finding" -}}
With no Spec, nothing here is a Spec flaw: file every finding beyond your slice,
including an intent you think is wrong, as a Critique; the human settles it at
Approval.
{{- end}}

{{define "external-work" -}}
Work in the external world that you find mid-run (outside the bead graph and
the code: a repo to create, a token to mint, a console to click through) is an
offshoot Signal, never a Task, so intake gates it like any other work. Read crew
doc signal-filing.md and file it in its offshoot form, discovered-from {{.}};
the description holds what is needed and why.
{{- end}}
