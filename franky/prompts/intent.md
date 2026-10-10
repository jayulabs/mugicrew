{{define "intent" -}}
{{if .SpecID -}}
Spec this Checkpoint implements: {{.SpecID}}  (read it with: bd show {{.SpecID}})
{{- else if .SurgeryID -}}
Surgery this Checkpoint carries out: {{.SurgeryID}}  (read it with: bd show {{.SurgeryID}}; the human's intent, your context in place of a Spec)
{{- else -}}
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
offshoot Signal, never a Task, so intake gates it like any other work:

    bd create --type task --labels kind:signal,drafted-by:{{.}} -s deferred --no-inherit-labels \
      --deps discovered-from:{{.}} \
      --title "<the external work>" --description "<what is needed and why>"
{{- end}}
