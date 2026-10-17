module go.vipengele.org/cli/core

go 1.27.2

require (
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.10
	go.vipengele.org/cli/spec v0.0.0
	go.vipengele.org/config v0.0.0
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	go.vipengele.org/commons v0.0.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/term v0.47.0 // indirect
)

replace go.vipengele.org/cli/spec => ../spec

replace go.vipengele.org/commons => ../../commons

replace go.vipengele.org/config => ../../config
