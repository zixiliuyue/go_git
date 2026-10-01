.PHONY: all build test bench verify clean

BIN_DIR := bin
BIN_NAME := gogit
TARGET := $(BIN_DIR)/$(BIN_NAME)

all: build

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(TARGET) ./cmd/gogit
	@echo "Build complete: $(TARGET)"

test:
	go test -v ./...

bench:
	./bench/perf.sh

verify:
	./test/verify_all.sh

clean:
	rm -rf $(BIN_DIR) /tmp/gogit
