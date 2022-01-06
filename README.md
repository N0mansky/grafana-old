![Grafana](docs/logo-horizontal.png)

The open-source platform for monitoring and observability.

[![License](https://img.shields.io/github/license/grafana/grafana)](LICENSE)
[![Drone](https://drone.grafana.net/api/badges/grafana/grafana/status.svg)](https://drone.grafana.net/grafana/grafana)
[![Go Report Card](https://goreportcard.com/badge/github.com/grafana/grafana)](https://goreportcard.com/report/github.com/grafana/grafana)

Implementing lark notification channel and elasticsearch alert details based on grafana v8.3.3

## Get started

### Build Backend

#### The Prerequisite of Building go

1. `yum install gcc make gcc-c++`
2. Download and Install Go quickly with the document. https://go.dev/doc/install

#### Compile Grafana Binary

1. `git clone xxxx.git`
2. `cd grafana && make build-go`

#### Packing Grafana Tarball

1. Download grafana v8.3.3 release version
2. Replace compiled binary files to grafana-8.3.3/bin
