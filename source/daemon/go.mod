module go.vipengele.org/daemon

go 1.27.4

require (
	go.vipengele.org/cli/spec v0.0.0
	go.vipengele.org/cli/state v0.0.0
	go.vipengele.org/filelock v0.0.0
	go.vipengele.org/update v0.0.0
	golang.org/x/sys v0.50.0
)

require (
	go.vipengele.org/commons v0.1.0 // indirect
	go.yaml.in/yaml/v3 v3.0.6 // indirect
	golang.org/x/crypto v0.58.1-0.20261009181718-2f1f834330b2 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/term v0.47.0 // indirect
)

replace go.vipengele.org/filelock => ../filelock

replace go.vipengele.org/cli/spec => ../cli/spec

replace go.vipengele.org/commons => ../commons

replace go.vipengele.org/cli/state => ../cli/state

replace go.vipengele.org/update => ../update
