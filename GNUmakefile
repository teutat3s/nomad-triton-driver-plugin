PLUGIN_BINARY=triton-driver
export GO111MODULE=on

default: build

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf ${PLUGIN_BINARY}

build:
	go build -v -o ${PLUGIN_BINARY} .
