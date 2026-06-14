# Copyright 2021 Contributors to the Parsec project.
# SPDX-License-Identifier: Apache-2.0

PROJECT_NAME := "parsec-client-go"
PKG := "github.com/parallaxsecond/$(PROJECT_NAME)"
PKG_LIST := $(shell go list ${PKG}/... | grep -v /vendor/)
GO_FILES := $(shell find . -name '*.go' | grep -v /vendor/ | grep -v _test.go)


PROTOC_PREPROCESSED_FILES := $(shell find ./interface/parsec-operations/protobuf -name '*.proto' -exec basename {} \; | awk '{print "interface/go-protobuf/"$$1}')
PROTOC_OUTPUT_FILES=$(shell find interface/parsec-operations/protobuf/ -name "*.proto" -exec basename {} .proto \; | awk '{print "interface/operations/"$$1".pb.go"}')

.PHONY: all dep lint vet test test-coverage build  protoc protobuf_preprocess clean-protobuf clean clean-all
 
protobuf_preprocess: ${PROTOC_PREPROCESSED_FILES}

protoc: protobuf_preprocess ${PROTOC_OUTPUT_FILES} ## Generate protocol buffer go code

interface/go-protobuf/%.proto: interface/parsec-operations/protobuf/%.proto
	@mkdir -p interface/go-protobuf
	@cp $< $@
	@$(eval PKG_NAME := $(shell basename $< .proto | sed s/_//g))
	@$(eval PKG_DEF := $(shell echo "option go_package = \\\"github.com/parallaxsecond/parsec-client-go/interface/operations/$(PKG_NAME)\\\";"))
	@#echo gopkg $(PKG_DEF)
	@grep  "$(PKG_DEF)" $@ || echo "\n$(PKG_DEF)" >> $@


# Can't work out how to get path and filename into the match
# need to have operations/option/option.pb.go maping to interface/go-protobuf/option.proto
# But works quickly and not needed often
interface/operations/%.pb.go: interface/go-protobuf/%.proto
	@mkdir -p tmp/protoout
	@protoc -I=interface/go-protobuf --go_out=tmp/protoout $< > /dev/null
	@$(eval PKG_NAME := $(shell basename $< .proto | sed s/_//g))
	@rm -Rf "./interface/operations/$(PKG_NAME)"
	@mv "tmp/protoout/github.com/parallaxsecond/parsec-client-go/interface/operations/$(PKG_NAME)" ./interface/operations
	@rm -Rf tmp/

clean-all: clean clean-protobuf
clean:
	@go clean ./...
	@rm -f $(PROJECT_NAME)/buildmk	

clean-protobuf:
	@find interface/operations/ -name "*.pb.go" -exec rm {} \;
	@rm -Rf interface/go-protobuf/*


all: protoc build ## Generate protocol buffer code and compile

dep: ## Get the dependencies
	@go mod download

lint: ## Lint Golang files and shellcheck scripts
	@golangci-lint run
	# Omit interface/parsec-operations as it is from a different repo and is loaded as submodule
	@find .  -path ./interface/parsec-operations -prune -false -o -name "*.sh" | xargs shellcheck 

test: ## Run unittests
	@go test -short ${PKG_LIST} | grep -v 'no test files'

test-coverage: ## Run tests with coverage
	@go test -short -coverprofile cover.out -covermode=atomic ${PKG_LIST} 
	@cat cover.out >> coverage.txt

ci-test-all: ## Run Continuous Integration tests for all providers
	@./e2etest/scripts/ci-all.sh

build: dep ## Build the binary file
	@go build -i -o ./... $(PKG)
 

	
 
help: ## Display this help screen
	@grep -h -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'