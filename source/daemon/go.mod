module go.vipengele.org/daemon

go 1.27.3

require (
	go.vipengele.org/cli/spec v0.0.0
	go.vipengele.org/filelock v0.0.0
)

require (
	go.vipengele.org/commons v0.0.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
)

replace go.vipengele.org/filelock => ../filelock

replace go.vipengele.org/cli/spec => ../cli/spec

replace go.vipengele.org/commons => ../commons
