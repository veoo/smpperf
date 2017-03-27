# smpperf
SMPP load testing and performance evaluation

# smppclient

Simple client which accepts a message as a config.toml file.

`go install cmd/smppclient.go`

`smppclient config.toml`

This will read the config from the current directory and send the defined message.
