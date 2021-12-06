apiVersion: v1
kind: Secret
metadata:
  name: {{.Name}}
{{if not (eq (Mod .Index 20) 10 19) }} # .Index % 20 in {10,19} - only 10% deployments will have non-immutable Secret.
immutable: true
{{end}}
type: Opaque
data:
  password: c2NhbGFiaWxpdHkK
