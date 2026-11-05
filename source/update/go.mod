module go.vipengele.org/update

go 1.27.3

require (
	go.vipengele.org/cli/spec v0.0.0
	go.vipengele.org/cli/state v0.0.0
	go.vipengele.org/commons v0.0.0
	golang.org/x/crypto v0.58.0
	golang.org/x/mod v0.42.0
	golang.org/x/sys v0.49.0
)

require (
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/term v0.47.0 // indirect
)

replace go.vipengele.org/cli/spec => ../cli/spec

replace go.vipengele.org/commons => ../commons

replace go.vipengele.org/cli/state => ../cli/state
