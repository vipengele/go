module go.vipengele.org/update

go 1.27.4

require (
	go.vipengele.org/cli/spec v0.0.0
	go.vipengele.org/cli/state v0.0.0
	go.vipengele.org/commons v0.0.0
	go.vipengele.org/filelock v0.0.0
	go.yaml.in/yaml/v3 v3.0.5
	golang.org/x/crypto v0.58.1-0.20261009181718-2f1f834330b2
	golang.org/x/mod v0.42.0
	golang.org/x/sys v0.50.0
)

require golang.org/x/term v0.47.0 // indirect

replace go.vipengele.org/cli/spec => ../cli/spec

replace go.vipengele.org/commons => ../commons

replace go.vipengele.org/cli/state => ../cli/state

replace go.vipengele.org/filelock => ../filelock
