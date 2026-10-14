module go.vipengele.org/cli/core

go 1.27.2

require (
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.9
	go.vipengele.org/cli/spec v0.0.0
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	go.vipengele.org/commons v0.0.0 // indirect
)

replace go.vipengele.org/cli/spec => ../spec

replace go.vipengele.org/commons => ../../commons
